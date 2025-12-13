package arbitrage

import (
	"crypto-arbitrage-monitor/pkg/common"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Calculator 价差计算器
type Calculator struct {
	mu              sync.RWMutex
	prices          map[string]*common.Price // key: exchange_markettype_symbol
	opportunities   []*common.ArbitrageOpportunity
	minSpread       float64
	opportunityChan chan *common.ArbitrageOpportunity
}

// NewCalculator 创建价差计算器
func NewCalculator(minSpreadPercent float64) *Calculator {
	return &Calculator{
		prices:          make(map[string]*common.Price),
		opportunities:   make([]*common.ArbitrageOpportunity, 0),
		minSpread:       minSpreadPercent,
		opportunityChan: make(chan *common.ArbitrageOpportunity, 100),
	}
}

// UpdatePrice 更新价格
func (c *Calculator) UpdatePrice(price *common.Price) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := c.makePriceKey(price.Exchange, price.MarketType, price.Symbol)
	c.prices[key] = price
}

// CalculateArbitrage 计算所有可能的套利机会
func (c *Calculator) CalculateArbitrage() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 清空旧的机会
	c.opportunities = make([]*common.ArbitrageOpportunity, 0)

	// 按symbol分组
	symbolPrices := c.groupPricesBySymbol()

	// Debug: 记录有效价格数量
	totalValidPrices := 0
	for _, prices := range symbolPrices {
		totalValidPrices += len(prices)
	}

	// 计算每个symbol的套利机会
	for symbol, prices := range symbolPrices {
		opps := c.calculateSymbolArbitrage(symbol, prices)
		c.opportunities = append(c.opportunities, opps...)
	}
}

// GetOpportunities 获取所有套利机会
func (c *Calculator) GetOpportunities() []*common.ArbitrageOpportunity {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 复制一份避免并发问题
	result := make([]*common.ArbitrageOpportunity, len(c.opportunities))
	copy(result, c.opportunities)

	return result
}

// GetOpportunityChan 获取套利机会通道
func (c *Calculator) GetOpportunityChan() <-chan *common.ArbitrageOpportunity {
	return c.opportunityChan
}

// GetPriceCount 获取价格数量
func (c *Calculator) GetPriceCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.prices)
}

// makePriceKey 生成价格键
func (c *Calculator) makePriceKey(exchange common.Exchange, marketType common.MarketType, symbol string) string {
	return fmt.Sprintf("%s_%s_%s", exchange, marketType, symbol)
}

// groupPricesBySymbol 按symbol分组价格
func (c *Calculator) groupPricesBySymbol() map[string][]*common.Price {
	result := make(map[string][]*common.Price)

	for _, price := range c.prices {
		// 检查价格是否过期（超过60秒）
		if time.Since(price.LastUpdated) > 60*time.Second {
			continue
		}

		if _, exists := result[price.Symbol]; !exists {
			result[price.Symbol] = make([]*common.Price, 0)
		}
		result[price.Symbol] = append(result[price.Symbol], price)
	}

	return result
}

// calculateSymbolArbitrage 计算单个symbol的套利机会
func (c *Calculator) calculateSymbolArbitrage(symbol string, prices []*common.Price) []*common.ArbitrageOpportunity {
	opportunities := make([]*common.ArbitrageOpportunity, 0)

	// 两两比较
	for i := 0; i < len(prices); i++ {
		for j := i + 1; j < len(prices); j++ {
			price1 := prices[i]
			price2 := prices[j]

			// 跳过相同交易所和市场类型的组合
			if price1.Exchange == price2.Exchange && price1.MarketType == price2.MarketType {
				continue
			}

			// 计算价差（使用买卖价）
			// 买入price1（ask），卖出price2（bid）
			spread1 := c.calculateSpread(price1.AskPrice, price2.BidPrice)
			// 买入price2（ask），卖出price1（bid）
			spread2 := c.calculateSpread(price2.AskPrice, price1.BidPrice)

			// 选择价差绝对值较大的一个
			var opp *common.ArbitrageOpportunity
			if math.Abs(spread1) > math.Abs(spread2) {
				opp = c.createOpportunity(symbol, price1, price2, spread1)
			} else {
				opp = c.createOpportunity(symbol, price2, price1, spread2)
			}

			if opp != nil {
				opportunities = append(opportunities, opp)

				// 只有高价差才发送到通道用于通知
				if opp.SpreadPercent >= c.minSpread {
					// 发送到通道（非阻塞）
					select {
					case c.opportunityChan <- opp:
					default:
					}
				}
			}
		}
	}

	return opportunities
}

// calculateSpread 计算价差百分比
func (c *Calculator) calculateSpread(buyPrice, sellPrice float64) float64 {
	if buyPrice == 0 {
		return 0
	}
	return ((sellPrice - buyPrice) / buyPrice) * 100
}

// createOpportunity 创建套利机会
func (c *Calculator) createOpportunity(symbol string, buyPrice, sellPrice *common.Price, spreadPercent float64) *common.ArbitrageOpportunity {
	// 确定套利类型
	arbType := c.getArbitrageType(buyPrice.MarketType, sellPrice.MarketType)

	// 计算绝对价差
	spreadAbsolute := sellPrice.BidPrice - buyPrice.AskPrice

	// 估算利润潜力（使用较小的24h交易量）
	volume := math.Min(buyPrice.Volume24h, sellPrice.Volume24h)

	return &common.ArbitrageOpportunity{
		ID:               uuid.New().String(),
		Symbol:           symbol,
		Type:             arbType,
		Exchange1:        buyPrice.Exchange,
		Exchange2:        sellPrice.Exchange,
		Market1Type:      buyPrice.MarketType,
		Market2Type:      sellPrice.MarketType,
		Price1:           buyPrice.AskPrice, // 买入价
		Price2:           sellPrice.BidPrice, // 卖出价
		SpreadPercent:    spreadPercent,
		SpreadAbsolute:   spreadAbsolute,
		Volume24h:        volume,
		ProfitPotential:  spreadAbsolute * volume * 0.001, // 简单估算
		Timestamp:        time.Now(),
		NotificationSent: false,
	}
}

// getArbitrageType 获取套利类型（market1是买入市场，market2是卖出市场）
func (c *Calculator) getArbitrageType(market1, market2 common.MarketType) string {
	if market1 == common.MarketTypeSpot && market2 == common.MarketTypeSpot {
		return "spot-spot" // 现货买入 → 现货卖出
	} else if market1 == common.MarketTypeFuture && market2 == common.MarketTypeFuture {
		return "future-future" // 合约买入 → 合约卖出
	} else if market1 == common.MarketTypeSpot && market2 == common.MarketTypeFuture {
		return "spot-future" // 现货买入 → 合约卖出
	} else {
		return "future-spot" // 合约买入 → 现货卖出
	}
}

// SortOpportunities 排序套利机会
func SortOpportunities(opportunities []*common.ArbitrageOpportunity, sortBy string, desc bool) []*common.ArbitrageOpportunity {
	// 复制数组
	sorted := make([]*common.ArbitrageOpportunity, len(opportunities))
	copy(sorted, opportunities)

	// 简单的冒泡排序（小数据量）
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			swap := false

			switch sortBy {
			case "spread":
				// 按绝对价差的绝对值排序
				if desc {
					swap = math.Abs(sorted[j].SpreadPercent) < math.Abs(sorted[j+1].SpreadPercent)
				} else {
					swap = math.Abs(sorted[j].SpreadPercent) > math.Abs(sorted[j+1].SpreadPercent)
				}
			case "profit":
				if desc {
					swap = sorted[j].ProfitPotential < sorted[j+1].ProfitPotential
				} else {
					swap = sorted[j].ProfitPotential > sorted[j+1].ProfitPotential
				}
			case "volume":
				if desc {
					swap = sorted[j].Volume24h < sorted[j+1].Volume24h
				} else {
					swap = sorted[j].Volume24h > sorted[j+1].Volume24h
				}
			case "time":
				if desc {
					swap = sorted[j].Timestamp.Before(sorted[j+1].Timestamp)
				} else {
					swap = sorted[j].Timestamp.After(sorted[j+1].Timestamp)
				}
			}

			if swap {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	return sorted
}

// FilterOpportunities 过滤套利机会
func FilterOpportunities(opportunities []*common.ArbitrageOpportunity, filterType string) []*common.ArbitrageOpportunity {
	if filterType == "" || filterType == "all" {
		return opportunities
	}

	filtered := make([]*common.ArbitrageOpportunity, 0)
	for _, opp := range opportunities {
		if opp.Type == filterType {
			filtered = append(filtered, opp)
		}
	}

	return filtered
}
