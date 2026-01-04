package okx

import (
	"crypto-arbitrage-monitor/pkg/common"
	"fmt"
	"math"
	"time"
)

// ValidateBidAskSpread 验证bid-ask价差是否合理
// bidPrice: bid价格（卖出价）
// askPrice: ask价格（买入价）
// maxSpreadPercent: 最大允许的价差百分比（如5.0表示5%）
//
// 返回error如果：
// - ask <= bid（不合理的价格关系）
// - 价差超过阈值
func ValidateBidAskSpread(bidPrice, askPrice float64, maxSpreadPercent float64) error {
	// 检查bid和ask的基本关系
	if askPrice <= bidPrice {
		return fmt.Errorf("invalid bid-ask relationship: ask(%.4f) <= bid(%.4f)", askPrice, bidPrice)
	}

	// 检查价格是否为正数
	if bidPrice <= 0 || askPrice <= 0 {
		return fmt.Errorf("invalid prices: bid=%.4f, ask=%.4f", bidPrice, askPrice)
	}

	// 计算价差百分比
	// spread = (ask - bid) / bid * 100
	spread := (askPrice - bidPrice) / bidPrice * 100

	// 检查是否超过阈值
	if spread > maxSpreadPercent {
		return fmt.Errorf("spread too large: %.2f%% > %.2f%% (bid=%.4f, ask=%.4f)",
			spread, maxSpreadPercent, bidPrice, askPrice)
	}

	return nil
}

// ValidatePriceChange 验证价格变化是否合理（与历史价格对比）
// symbol: 代币符号
// oldPrice: 历史价格（来自PriceStore）
// newPrice: 新价格
// maxChangePercent: 最大允许的价格变化百分比（如30.0表示30%）
//
// 返回error如果价格变化超过阈值
func ValidatePriceChange(symbol string, oldPrice, newPrice float64, maxChangePercent float64) error {
	// 如果没有历史价格，跳过验证
	if oldPrice == 0 {
		return nil
	}

	// 检查新价格是否为正数
	if newPrice <= 0 {
		return fmt.Errorf("invalid new price: %.4f", newPrice)
	}

	// 计算价格变化百分比
	// change = |new - old| / old * 100
	change := math.Abs(newPrice-oldPrice) / oldPrice * 100

	// 检查是否超过阈值
	if change > maxChangePercent {
		return fmt.Errorf("price change too large for %s: %.2f%% (old=%.4f, new=%.4f)",
			symbol, change, oldPrice, newPrice)
	}

	return nil
}

// ValidateTimestamp 验证时间戳是否合理
// timestamp: 要验证的时间戳
//
// 返回error如果：
// - 时间戳是未来时间（超过1分钟容忍）
// - 时间戳太旧（超过5分钟）
func ValidateTimestamp(timestamp time.Time) error {
	now := time.Now()

	// 检查是否为未来时间（容忍1分钟的时钟偏差）
	if timestamp.After(now.Add(1 * time.Minute)) {
		return fmt.Errorf("timestamp in future: %v (now=%v, diff=%.0fs)",
			timestamp, now, timestamp.Sub(now).Seconds())
	}

	// 检查是否太旧（超过5分钟）
	age := now.Sub(timestamp)
	if age > 5*time.Minute {
		return fmt.Errorf("timestamp too old: %v (age=%.0fs)",
			timestamp, age.Seconds())
	}

	return nil
}

// ValidatePrice 综合验证价格对象
// price: 要验证的价格对象
// oldPrice: 历史价格（用于变化检测，可为nil）
// maxSpreadPercent: 最大价差百分比
// maxChangePercent: 最大价格变化百分比
//
// 返回警告信息字符串（如果有多个问题，用分号分隔）
func ValidatePrice(
	price *common.Price,
	oldPrice *common.Price,
	maxSpreadPercent float64,
	maxChangePercent float64,
) string {
	if price == nil {
		return "price is nil"
	}

	warnings := []string{}

	// 验证1: Bid-Ask价差
	if price.BidPrice > 0 && price.AskPrice > 0 {
		if err := ValidateBidAskSpread(price.BidPrice, price.AskPrice, maxSpreadPercent); err != nil {
			warnings = append(warnings, err.Error())
		}
	}

	// 验证2: 价格变化（如果有历史价格）
	if oldPrice != nil && oldPrice.Price > 0 {
		if err := ValidatePriceChange(price.Symbol, oldPrice.Price, price.Price, maxChangePercent); err != nil {
			warnings = append(warnings, err.Error())
		}
	}

	// 验证3: 时间戳
	if err := ValidateTimestamp(price.Timestamp); err != nil {
		warnings = append(warnings, err.Error())
	}

	// 验证4: 基本价格检查
	if price.Price <= 0 {
		warnings = append(warnings, fmt.Sprintf("invalid price: %.4f", price.Price))
	}

	// 合并所有警告
	if len(warnings) > 0 {
		result := ""
		for i, w := range warnings {
			if i > 0 {
				result += "; "
			}
			result += w
		}
		return result
	}

	return ""
}

// ValidatePriceWithHistory 使用PriceStore中的历史价格进行验证
// 这是一个便捷方法，自动从PriceStore获取历史价格
func (c *BidirectionalTaskCoordinator) ValidatePriceWithHistory(price *common.Price) string {
	if price == nil {
		return "price is nil"
	}

	// 从PriceStore获取历史价格
	var oldPrice *common.Price
	if c.priceStore != nil {
		oldPrice = c.priceStore.GetPrice(price.Exchange, price.MarketType, price.Symbol)
	}

	// 使用配置的阈值进行验证
	return ValidatePrice(price, oldPrice, c.maxSpreadPercent, c.maxPriceChangePercent)
}
