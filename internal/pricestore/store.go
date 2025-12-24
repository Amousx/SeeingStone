package pricestore

import (
	"crypto-arbitrage-monitor/pkg/common"
	"fmt"
	"strings"
	"sync"
	"time"
)

// PriceStore 价格数据存储器 - 使用双索引结构
type PriceStore struct {
	mu sync.RWMutex

	// 索引1: 按交易所维度存储
	// key: exchange, value: map[marketType_symbol]*Price
	byExchange map[common.Exchange]map[string]*common.Price

	// 索引2: 按标准化symbol维度存储
	// key: standardSymbol, value: map[exchange_marketType]*Price
	bySymbol map[string]map[string]*common.Price

	// Symbol标准化映射表
	// 用于解决不同交易所symbol名称不一致的问题
	symbolNormalizer *SymbolNormalizer
}

// NewPriceStore 创建价格存储器
func NewPriceStore() *PriceStore {
	return &PriceStore{
		byExchange:       make(map[common.Exchange]map[string]*common.Price),
		bySymbol:         make(map[string]map[string]*common.Price),
		symbolNormalizer: NewSymbolNormalizer(),
	}
}

// UpdatePrice 更新价格数据（线程安全）
// 自动判断是否应该更新（防止旧数据覆盖新数据）
// 返回值：是否实际更新了数据
func (ps *PriceStore) UpdatePrice(price *common.Price) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// 标准化symbol
	standardSymbol := ps.symbolNormalizer.Normalize(price.Symbol)

	// 生成各种key
	exchangeKey := ps.makeExchangeKey(price.MarketType, price.Symbol)

	// 检查是否应该更新（新鲜度判断）
	if ps.byExchange[price.Exchange] != nil {
		if existingPrice := ps.byExchange[price.Exchange][exchangeKey]; existingPrice != nil {
			if !ps.shouldUpdate(existingPrice, price) {
				return false // 不更新旧数据
			}
		}
	}

	symbolKey := ps.makeSymbolKey(price.Exchange, price.MarketType)

	// 更新exchange索引
	if ps.byExchange[price.Exchange] == nil {
		ps.byExchange[price.Exchange] = make(map[string]*common.Price)
	}
	ps.byExchange[price.Exchange][exchangeKey] = price

	// 更新symbol索引
	if ps.bySymbol[standardSymbol] == nil {
		ps.bySymbol[standardSymbol] = make(map[string]*common.Price)
	}
	ps.bySymbol[standardSymbol][symbolKey] = price

	return true
}

// shouldUpdate 判断是否应该更新价格
// 策略：
// 1. 如果现有数据超过10秒未更新，接受新数据（REST兜底）
// 2. 如果新数据的时间戳更新，接受新数据
// 3. 否则拒绝更新（防止旧数据覆盖新数据）
func (ps *PriceStore) shouldUpdate(existing, new *common.Price) bool {
	now := time.Now()

	// 现有数据超过10秒没更新，接受任何新数据（WS可能断了，REST兜底）
	if now.Sub(existing.LastUpdated) > 10*time.Second {
		return true
	}

	// 新数据的时间戳更新，接受
	if new.LastUpdated.After(existing.LastUpdated) {
		return true
	}

	// 新数据的价格时间戳更新，接受
	if new.Timestamp.After(existing.Timestamp) {
		return true
	}

	// 否则拒绝（防止旧数据覆盖）
	return false
}

// GetPricesByExchange 按交易所获取所有价格
func (ps *PriceStore) GetPricesByExchange(exchange common.Exchange) []*common.Price {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	prices := make([]*common.Price, 0)
	if exchangeMap, exists := ps.byExchange[exchange]; exists {
		for _, price := range exchangeMap {
			prices = append(prices, price)
		}
	}
	return prices
}

// GetPricesBySymbol 按标准化symbol获取跨交易所的所有价格
func (ps *PriceStore) GetPricesBySymbol(symbol string) []*common.Price {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	standardSymbol := ps.symbolNormalizer.Normalize(symbol)
	prices := make([]*common.Price, 0)

	if symbolMap, exists := ps.bySymbol[standardSymbol]; exists {
		for _, price := range symbolMap {
			prices = append(prices, price)
		}
	}
	return prices
}

// GetPrice 获取特定交易所、市场类型、symbol的价格
func (ps *PriceStore) GetPrice(exchange common.Exchange, marketType common.MarketType, symbol string) *common.Price {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	exchangeKey := ps.makeExchangeKey(marketType, symbol)
	if exchangeMap, exists := ps.byExchange[exchange]; exists {
		return exchangeMap[exchangeKey]
	}
	return nil
}

// GetAllPrices 获取所有价格数据
func (ps *PriceStore) GetAllPrices() []*common.Price {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	prices := make([]*common.Price, 0)
	for _, exchangeMap := range ps.byExchange {
		for _, price := range exchangeMap {
			prices = append(prices, price)
		}
	}
	return prices
}

// GetAllSymbols 获取所有标准化symbol列表
func (ps *PriceStore) GetAllSymbols() []string {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	symbols := make([]string, 0, len(ps.bySymbol))
	for symbol := range ps.bySymbol {
		symbols = append(symbols, symbol)
	}
	return symbols
}

// GetAllExchanges 获取所有交易所列表
func (ps *PriceStore) GetAllExchanges() []common.Exchange {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	exchanges := make([]common.Exchange, 0, len(ps.byExchange))
	for exchange := range ps.byExchange {
		exchanges = append(exchanges, exchange)
	}
	return exchanges
}

// GetStats 获取统计信息
func (ps *PriceStore) GetStats() StoreStats {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	stats := StoreStats{
		TotalPrices:    0,
		TotalSymbols:   len(ps.bySymbol),
		TotalExchanges: len(ps.byExchange),
		ByExchange:     make(map[common.Exchange]int),
	}

	for exchange, priceMap := range ps.byExchange {
		count := len(priceMap)
		stats.TotalPrices += count
		stats.ByExchange[exchange] = count
	}

	return stats
}

// GetActivePrices 获取活跃价格（在指定时间内更新过的）
func (ps *PriceStore) GetActivePrices(within time.Duration) []*common.Price {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	now := time.Now()
	prices := make([]*common.Price, 0)

	for _, exchangeMap := range ps.byExchange {
		for _, price := range exchangeMap {
			if now.Sub(price.LastUpdated) <= within {
				prices = append(prices, price)
			}
		}
	}
	return prices
}

// Spread 价差信息
type Spread struct {
	Symbol         string            `json:"symbol"`
	BuyExchange    common.Exchange   `json:"buy_exchange"`
	BuyMarketType  common.MarketType `json:"buy_market_type"`
	BuyPrice       float64           `json:"buy_price"`
	SellExchange   common.Exchange   `json:"sell_exchange"`
	SellMarketType common.MarketType `json:"sell_market_type"`
	SellPrice      float64           `json:"sell_price"`
	SpreadPercent  float64           `json:"spread_percent"`
	SpreadAbsolute float64           `json:"spread_absolute"`
	Volume24h      float64           `json:"volume_24h"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// CalculateSpreads 计算所有symbol的价差
// 返回按价差百分比降序排列的价差列表
func (ps *PriceStore) CalculateSpreads() []*Spread {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	spreads := make([]*Spread, 0)

	// 遍历所有symbol
	for _, priceMap := range ps.bySymbol {
		// 将map转为slice方便比较
		prices := make([]*common.Price, 0, len(priceMap))
		for _, price := range priceMap {
			// 只考虑60秒内的活跃数据
			if time.Since(price.LastUpdated) <= 60*time.Second {
				prices = append(prices, price)
			}
		}

		// 至少需要2个交易所的数据才能计算价差
		if len(prices) < 2 {
			continue
		}

		// 两两比较计算价差
		for i := 0; i < len(prices); i++ {
			for j := i + 1; j < len(prices); j++ {
				p1 := prices[i]
				p2 := prices[j]

				// 跳过相同交易所和市场类型的组合
				if p1.Exchange == p2.Exchange && p1.MarketType == p2.MarketType {
					continue
				}

				// 计算两个方向的价差
				// 方向1: 买p1卖p2
				spread1 := ps.calculateSpread(p1, p2)
				if spread1 != nil {
					spreads = append(spreads, spread1)
				}

				// 方向2: 买p2卖p1
				spread2 := ps.calculateSpread(p2, p1)
				if spread2 != nil {
					spreads = append(spreads, spread2)
				}
			}
		}
	}

	// 按价差百分比降序排序
	ps.sortSpreadsByPercent(spreads)

	return spreads
}

// calculateSpread 计算单向价差（买buyPrice卖sellPrice）
func (ps *PriceStore) calculateSpread(buyPrice, sellPrice *common.Price) *Spread {
	// 使用ask价格买入，bid价格卖出
	askPrice := buyPrice.AskPrice
	if askPrice == 0 {
		askPrice = buyPrice.Price
	}

	bidPrice := sellPrice.BidPrice
	if bidPrice == 0 {
		bidPrice = sellPrice.Price
	}

	if askPrice == 0 || bidPrice == 0 {
		return nil
	}

	// 计算价差百分比
	spreadPercent := ((bidPrice - askPrice) / askPrice) * 100
	spreadAbsolute := bidPrice - askPrice

	// 选择较小的volume
	volume := buyPrice.Volume24h
	if sellPrice.Volume24h < volume {
		volume = sellPrice.Volume24h
	}

	// 使用较新的更新时间
	updatedAt := buyPrice.LastUpdated
	if sellPrice.LastUpdated.After(updatedAt) {
		updatedAt = sellPrice.LastUpdated
	}

	return &Spread{
		Symbol:         buyPrice.Symbol,
		BuyExchange:    buyPrice.Exchange,
		BuyMarketType:  buyPrice.MarketType,
		BuyPrice:       askPrice,
		SellExchange:   sellPrice.Exchange,
		SellMarketType: sellPrice.MarketType,
		SellPrice:      bidPrice,
		SpreadPercent:  spreadPercent,
		SpreadAbsolute: spreadAbsolute,
		Volume24h:      volume,
		UpdatedAt:      updatedAt,
	}
}

// sortSpreadsByPercent 按价差百分比降序排序
func (ps *PriceStore) sortSpreadsByPercent(spreads []*Spread) {
	// 简单冒泡排序（数据量不大）
	for i := 0; i < len(spreads)-1; i++ {
		for j := 0; j < len(spreads)-i-1; j++ {
			if spreads[j].SpreadPercent < spreads[j+1].SpreadPercent {
				spreads[j], spreads[j+1] = spreads[j+1], spreads[j]
			}
		}
	}
}

// CleanStaleData 清理过期数据
func (ps *PriceStore) CleanStaleData(threshold time.Duration) int {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	now := time.Now()
	removedCount := 0

	// 清理byExchange索引中的过期数据
	for exchange, exchangeMap := range ps.byExchange {
		for key, price := range exchangeMap {
			if now.Sub(price.LastUpdated) > threshold {
				delete(exchangeMap, key)
				removedCount++
			}
		}
		// 如果exchange map为空，删除整个exchange entry
		if len(exchangeMap) == 0 {
			delete(ps.byExchange, exchange)
		}
	}

	// 重建bySymbol索引
	ps.rebuildSymbolIndex()

	return removedCount
}

// rebuildSymbolIndex 重建symbol索引（必须在持有锁的情况下调用）
func (ps *PriceStore) rebuildSymbolIndex() {
	ps.bySymbol = make(map[string]map[string]*common.Price)

	for exchange, exchangeMap := range ps.byExchange {
		for _, price := range exchangeMap {
			standardSymbol := ps.symbolNormalizer.Normalize(price.Symbol)
			symbolKey := ps.makeSymbolKey(exchange, price.MarketType)

			if ps.bySymbol[standardSymbol] == nil {
				ps.bySymbol[standardSymbol] = make(map[string]*common.Price)
			}
			ps.bySymbol[standardSymbol][symbolKey] = price
		}
	}
}

// makeExchangeKey 生成exchange索引的key: marketType_symbol
func (ps *PriceStore) makeExchangeKey(marketType common.MarketType, symbol string) string {
	return fmt.Sprintf("%s_%s", marketType, symbol)
}

// makeSymbolKey 生成symbol索引的key: exchange_marketType
func (ps *PriceStore) makeSymbolKey(exchange common.Exchange, marketType common.MarketType) string {
	return fmt.Sprintf("%s_%s", exchange, marketType)
}

// StoreStats 存储统计信息
type StoreStats struct {
	TotalPrices    int
	TotalSymbols   int
	TotalExchanges int
	ByExchange     map[common.Exchange]int
}

// SymbolNormalizer 处理不同交易所symbol名称不一致的问题
type SymbolNormalizer struct {
	mu sync.RWMutex
	// 自定义映射规则
	customMappings map[string]string
}

// NewSymbolNormalizer 创建symbol标准化器
func NewSymbolNormalizer() *SymbolNormalizer {
	sn := &SymbolNormalizer{
		customMappings: make(map[string]string),
	}

	// 添加一些常见的映射规则
	// 例如: BTC-USDT -> BTCUSDT, BTC/USDT -> BTCUSDT
	sn.initDefaultMappings()

	return sn
}

// initDefaultMappings 初始化默认映射规则
func (sn *SymbolNormalizer) initDefaultMappings() {
	// 这里可以添加一些已知的symbol映射
	// 例如不同交易所对同一币种的不同叫法
}

// Normalize 标准化symbol名称
func (sn *SymbolNormalizer) Normalize(symbol string) string {
	sn.mu.RLock()
	defer sn.mu.RUnlock()

	// 检查是否有自定义映射
	if mapped, exists := sn.customMappings[symbol]; exists {
		return mapped
	}

	// 默认标准化规则：
	// 1. 转大写
	// 2. 移除常见分隔符 (-, /, _)
	normalized := strings.ToUpper(symbol)
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "/", "")
	normalized = strings.ReplaceAll(normalized, "_", "")

	return normalized
}

// AddMapping 添加自定义symbol映射
func (sn *SymbolNormalizer) AddMapping(original, standard string) {
	sn.mu.Lock()
	defer sn.mu.Unlock()
	sn.customMappings[original] = standard
}

// GetMapping 获取symbol的标准化映射
func (sn *SymbolNormalizer) GetMapping(symbol string) (string, bool) {
	sn.mu.RLock()
	defer sn.mu.RUnlock()
	mapped, exists := sn.customMappings[symbol]
	return mapped, exists
}

// CustomStrategy 自定义策略套利机会
type CustomStrategy struct {
	Name         string                `json:"name"`
	Description  string                `json:"description"`
	Formula      string                `json:"formula"`
	StrategyType string                `json:"strategy_type"` // "+A-B" or "-A+B"
	Value        float64               `json:"value"`
	ValuePercent float64               `json:"value_percent"`
	Components   []CustomStrategyToken `json:"components"`
	LastUpdated  time.Time             `json:"last_updated"`
	Status       string                `json:"status"` // "ready", "partial", "unavailable"
}

// CustomStrategyToken 策略中的代币信息
type CustomStrategyToken struct {
	Symbol      string            `json:"symbol"`
	Coefficient float64           `json:"coefficient"`
	Exchange    common.Exchange   `json:"exchange"`
	MarketType  common.MarketType `json:"market_type"`
	Price       float64           `json:"price"`
	Available   bool              `json:"available"`
}

// CalculateCustomStrategies 计算所有自定义策略
func (ps *PriceStore) CalculateCustomStrategies() []*CustomStrategy {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	strategies := make([]*CustomStrategy, 0)

	// 策略1: STG - 0.08634 * ZRO
	stgZroStrategy := ps.calculateSTGZROStrategy()
	if stgZroStrategy != nil {
		strategies = append(strategies, stgZroStrategy)
	}

	// 策略2: BTC/SOL/ETH 价差监控 (Aster, Binance, Lighter)
	multiExchangeStrategies := ps.calculateMultiExchangeSpreadStrategies()
	strategies = append(strategies, multiExchangeStrategies...)

	return strategies
}

// calculateSTGZROStrategy 计算 STG - 0.08634 * ZRO 策略
// 策略类型：+A-B (买入A，卖出B)
// A = STG, B = ZRO * 0.08634
// 使用 STG Ask（买入价格）和 ZRO Bid（卖出价格）
// 绝对价差 = ZRO Bid * 0.08634 - STG Ask
// 百分比 = (ZRO Bid * 0.08634 - STG Ask) * 2 / (ZRO Bid * 0.08634 + STG Ask) * 100
func (ps *PriceStore) calculateSTGZROStrategy() *CustomStrategy {
	const coefficient = 0.08634

	strategy := &CustomStrategy{
		Name:         "STG-ZRO 价差套利",
		Description:  "买入STG卖出ZRO的价差套利",
		Formula:      "(ZRO Bid * 0.08634 - STG Ask) * 2 / (ZRO Bid * 0.08634 + STG Ask) * 100",
		StrategyType: "+A-B",
		Components:   make([]CustomStrategyToken, 0),
		Status:       "unavailable",
	}

	// 获取 STG 价格（优先选择 Binance SPOT）
	stgPrice := ps.getBestPrice("STGUSDT", common.ExchangeBinance, common.MarketTypeSpot)
	if stgPrice == nil {
		stgPrice = ps.getBestPrice("STGUSDT", common.ExchangeAster, common.MarketTypeSpot)
	}

	// 获取 ZRO 价格（优先选择 Binance SPOT）
	zroPrice := ps.getBestPrice("ZROUSDT", common.ExchangeBinance, common.MarketTypeSpot)
	if zroPrice == nil {
		zroPrice = ps.getBestPrice("ZROUSDT", common.ExchangeAster, common.MarketTypeSpot)
	}

	// 获取实际使用的价格
	var stgAsk, zroBid float64

	// 添加 STG 组件（使用 Ask 价格 - 买入价格）
	if stgPrice != nil {
		stgAsk = stgPrice.AskPrice
		if stgAsk == 0 {
			stgAsk = stgPrice.Price
		}

		strategy.Components = append(strategy.Components, CustomStrategyToken{
			Symbol:      "STG",
			Coefficient: 1.0,
			Exchange:    stgPrice.Exchange,
			MarketType:  stgPrice.MarketType,
			Price:       stgAsk,
			Available:   true,
		})
	} else {
		strategy.Components = append(strategy.Components, CustomStrategyToken{
			Symbol:      "STG",
			Coefficient: 1.0,
			Available:   false,
		})
	}

	// 添加 ZRO 组件（使用 Bid 价格 - 卖出价格）
	if zroPrice != nil {
		zroBid = zroPrice.BidPrice
		if zroBid == 0 {
			zroBid = zroPrice.Price
		}

		strategy.Components = append(strategy.Components, CustomStrategyToken{
			Symbol:      "ZRO",
			Coefficient: -coefficient,
			Exchange:    zroPrice.Exchange,
			MarketType:  zroPrice.MarketType,
			Price:       zroBid,
			Available:   true,
		})
	} else {
		strategy.Components = append(strategy.Components, CustomStrategyToken{
			Symbol:      "ZRO",
			Coefficient: -coefficient,
			Available:   false,
		})
	}

	// 计算策略值和百分比
	if stgPrice != nil && zroPrice != nil && stgAsk > 0 && zroBid > 0 {
		// B Bid = ZRO Bid * coefficient
		bBid := zroBid * coefficient
		// A Ask = STG Ask
		aAsk := stgAsk

		// 绝对价差: B Bid - A Ask = ZRO Bid * 0.08634 - STG Ask
		strategy.Value = bBid - aAsk

		// 百分比: (B Bid - A Ask) * 2 / (B Bid + A Ask) * 100
		if (bBid + aAsk) > 0 {
			strategy.ValuePercent = (bBid - aAsk) * 2 / (bBid + aAsk) * 100
		}

		strategy.Status = "ready"

		// 使用较新的更新时间
		strategy.LastUpdated = stgPrice.LastUpdated
		if zroPrice.LastUpdated.After(strategy.LastUpdated) {
			strategy.LastUpdated = zroPrice.LastUpdated
		}
	} else if stgPrice != nil || zroPrice != nil {
		strategy.Status = "partial"
		if stgPrice != nil {
			strategy.LastUpdated = stgPrice.LastUpdated
		} else {
			strategy.LastUpdated = zroPrice.LastUpdated
		}
	}

	return strategy
}

// ArbitrageOpportunity 套利机会
type ArbitrageOpportunity struct {
	Type         string  `json:"type"`          // "major_coin_spread", "stg_zro_spread", "large_cap_spread"
	Symbol       string  `json:"symbol"`        // 币种符号
	Description  string  `json:"description"`   // 描述
	SpreadPercent float64 `json:"spread_percent"` // 价差百分比
	BuyFrom      string  `json:"buy_from"`      // 买入位置
	SellTo       string  `json:"sell_to"`       // 卖出位置
	Strategy     *CustomStrategy `json:"strategy,omitempty"` // 关联的策略详情
}

// GetArbitrageOpportunities 获取当前可套利策略
// 规则：
// 1. BTC/ETH/SOL 价差 >= 0.1%（千1）
// 2. STG-ZRO 价差 >= 0.4%（千4）
// 3. 大市值币种（市值>2B）价差 >= 0.2%（千2）
func (ps *PriceStore) GetArbitrageOpportunities() []*ArbitrageOpportunity {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	opportunities := make([]*ArbitrageOpportunity, 0)

	// 定义主流币种（BTC, ETH, SOL）
	majorCoins := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}

	// 定义大市值币种（市值>2B，根据2024-2025年数据）
	largeCapCoins := map[string]bool{
		"BTCUSDT":  true, // Bitcoin
		"ETHUSDT":  true, // Ethereum
		"SOLUSDT":  true, // Solana
		"BNBUSDT":  true, // BNB
		"XRPUSDT":  true, // XRP
		"ADAUSDT":  true, // Cardano
		"DOGEUSDT": true, // Dogecoin
		"TRXUSDT":  true, // TRON
		"LINKUSDT": true, // Chainlink
		"AVAXUSDT": true, // Avalanche
		"DOTUSDT":  true, // Polkadot
		"MATICUSDT": true, // Polygon
		"UNIUSDT":  true, // Uniswap
		"LTCUSDT":  true, // Litecoin
		"ATOMUSDT": true, // Cosmos
	}

	// 1. 检查 BTC/ETH/SOL 价差（千1 = 0.1%）
	for _, coin := range majorCoins {
		opps := ps.findSpreadOpportunities(coin, 0.1, "major_coin_spread")
		opportunities = append(opportunities, opps...)
	}

	// 2. 检查 STG-ZRO 策略价差（千4 = 0.4%）
	stgZroOpp := ps.checkSTGZROOpportunity(0.4)
	if stgZroOpp != nil {
		opportunities = append(opportunities, stgZroOpp)
	}

	// 3. 检查大市值币种价差（千2 = 0.2%）
	for coin := range largeCapCoins {
		// 跳过已经在主流币种中检查过的
		if coin == "BTCUSDT" || coin == "ETHUSDT" || coin == "SOLUSDT" {
			continue
		}
		opps := ps.findSpreadOpportunities(coin, 0.2, "large_cap_spread")
		opportunities = append(opportunities, opps...)
	}

	return opportunities
}

// findSpreadOpportunities 查找指定币种的价差套利机会
func (ps *PriceStore) findSpreadOpportunities(symbol string, minSpreadPercent float64, oppType string) []*ArbitrageOpportunity {
	opportunities := make([]*ArbitrageOpportunity, 0)

	// 获取该币种的所有价格
	standardSymbol := ps.symbolNormalizer.Normalize(symbol)
	symbolMap, exists := ps.bySymbol[standardSymbol]
	if !exists {
		return opportunities
	}

	// 转换为价格列表
	prices := make([]*common.Price, 0)
	for _, price := range symbolMap {
		if time.Since(price.LastUpdated) <= 60*time.Second {
			prices = append(prices, price)
		}
	}

	if len(prices) < 2 {
		return opportunities
	}

	// 提取币种名称
	coinName := symbol
	if len(coinName) > 4 && coinName[len(coinName)-4:] == "USDT" {
		coinName = coinName[:len(coinName)-4]
	}

	// 计算所有可能的价差组合
	for i := 0; i < len(prices); i++ {
		for j := i + 1; j < len(prices); j++ {
			buyPrice := prices[i]
			sellPrice := prices[j]

			// 跳过相同交易所相同市场类型
			if buyPrice.Exchange == sellPrice.Exchange && buyPrice.MarketType == sellPrice.MarketType {
				continue
			}

			// 获取买入和卖出价格
			askPrice := buyPrice.AskPrice
			if askPrice == 0 {
				askPrice = buyPrice.Price
			}

			bidPrice := sellPrice.BidPrice
			if bidPrice == 0 {
				bidPrice = sellPrice.Price
			}

			if askPrice == 0 || bidPrice == 0 {
				continue
			}

			// 计算价差百分比
			spreadPercent := ((bidPrice - askPrice) / askPrice) * 100

			// 检查是否满足最小价差要求
			if spreadPercent >= minSpreadPercent {
				buyFrom := fmt.Sprintf("%s %s", buyPrice.Exchange, buyPrice.MarketType)
				sellTo := fmt.Sprintf("%s %s", sellPrice.Exchange, sellPrice.MarketType)

				opportunities = append(opportunities, &ArbitrageOpportunity{
					Type:          oppType,
					Symbol:        coinName,
					Description:   fmt.Sprintf("买入 %s，卖出 %s", buyFrom, sellTo),
					SpreadPercent: spreadPercent,
					BuyFrom:       buyFrom,
					SellTo:        sellTo,
				})
			}

			// 反向检查
			spreadPercentReverse := ((askPrice - bidPrice) / bidPrice) * 100
			if spreadPercentReverse >= minSpreadPercent {
				buyFrom := fmt.Sprintf("%s %s", sellPrice.Exchange, sellPrice.MarketType)
				sellTo := fmt.Sprintf("%s %s", buyPrice.Exchange, buyPrice.MarketType)

				opportunities = append(opportunities, &ArbitrageOpportunity{
					Type:          oppType,
					Symbol:        coinName,
					Description:   fmt.Sprintf("买入 %s，卖出 %s", buyFrom, sellTo),
					SpreadPercent: spreadPercentReverse,
					BuyFrom:       buyFrom,
					SellTo:        sellTo,
				})
			}
		}
	}

	return opportunities
}

// checkSTGZROOpportunity 检查 STG-ZRO 策略套利机会
func (ps *PriceStore) checkSTGZROOpportunity(minSpreadPercent float64) *ArbitrageOpportunity {
	strategy := ps.calculateSTGZROStrategy()
	if strategy == nil || strategy.Status != "ready" {
		return nil
	}

	// 检查价差百分比是否满足条件
	if strategy.ValuePercent >= minSpreadPercent {
		return &ArbitrageOpportunity{
			Type:          "stg_zro_spread",
			Symbol:        "STG-ZRO",
			Description:   "STG-ZRO 套利策略",
			SpreadPercent: strategy.ValuePercent,
			BuyFrom:       "买入STG",
			SellTo:        "卖出ZRO",
			Strategy:      strategy,
		}
	}

	return nil
}

// getBestPrice 获取指定symbol的最佳价格（最近更新的活跃价格）
func (ps *PriceStore) getBestPrice(symbol string, preferredExchange common.Exchange, preferredMarketType common.MarketType) *common.Price {
	// 首先尝试获取指定交易所和市场类型的价格
	price := ps.GetPrice(preferredExchange, preferredMarketType, symbol)
	if price != nil && time.Since(price.LastUpdated) <= 60*time.Second {
		return price
	}

	// 如果没有找到，遍历所有该symbol的价格，找到最新的活跃价格
	standardSymbol := ps.symbolNormalizer.Normalize(symbol)
	if symbolMap, exists := ps.bySymbol[standardSymbol]; exists {
		var bestPrice *common.Price
		for _, p := range symbolMap {
			if time.Since(p.LastUpdated) > 60*time.Second {
				continue
			}
			if bestPrice == nil || p.LastUpdated.After(bestPrice.LastUpdated) {
				bestPrice = p
			}
		}
		return bestPrice
	}

	return nil
}

// calculateMultiExchangeSpreadStrategies 计算多交易所价差策略
// 监控 BTC, SOL, ETH 在 Aster, Binance, Lighter 之间的价差
func (ps *PriceStore) calculateMultiExchangeSpreadStrategies() []*CustomStrategy {
	strategies := make([]*CustomStrategy, 0)

	// 定义要监控的币种
	symbols := []string{"BTCUSDT", "SOLUSDT", "ETHUSDT"}

	// 定义要监控的交易所（优先顺序）
	exchanges := []struct {
		exchange   common.Exchange
		marketType common.MarketType
	}{
		{common.ExchangeAster, common.MarketTypeFuture},    // Aster合约
		{common.ExchangeBinance, common.MarketTypeFuture},  // Binance合约
		{common.ExchangeLighter, common.MarketTypeFuture},  // Lighter合约
		{common.ExchangeAster, common.MarketTypeSpot},      // Aster现货
		{common.ExchangeBinance, common.MarketTypeSpot},    // Binance现货
	}

	// 对每个币种计算价差
	for _, symbol := range symbols {
		// 获取所有交易所的价格
		prices := make([]*common.Price, 0)
		for _, ex := range exchanges {
			price := ps.GetPrice(ex.exchange, ex.marketType, symbol)
			if price != nil && time.Since(price.LastUpdated) <= 60*time.Second {
				prices = append(prices, price)
			}
		}

		// 调试日志：显示找到的价格数量
		if len(prices) > 0 {
			fmt.Printf("[MultiExchange] %s: found %d prices\n", symbol, len(prices))
		}

		// 需要至少2个交易所的价格才能计算价差
		if len(prices) < 2 {
			continue
		}

		// 计算所有可能的价差组合
		for i := 0; i < len(prices); i++ {
			for j := i + 1; j < len(prices); j++ {
				buyPrice := prices[i]
				sellPrice := prices[j]

				// 跳过相同交易所相同市场类型的组合
				if buyPrice.Exchange == sellPrice.Exchange && buyPrice.MarketType == sellPrice.MarketType {
					continue
				}

				// 计算两个方向的价差
				strategy1 := ps.calculateSpreadStrategy(buyPrice, sellPrice)
				if strategy1 != nil {
					strategies = append(strategies, strategy1)
				}

				strategy2 := ps.calculateSpreadStrategy(sellPrice, buyPrice)
				if strategy2 != nil {
					strategies = append(strategies, strategy2)
				}
			}
		}
	}

	// 调试日志：显示生成的策略数量
	if len(strategies) > 0 {
		fmt.Printf("[MultiExchange] Generated %d spread strategies\n", len(strategies))
	} else {
		fmt.Println("[MultiExchange] No spread strategies generated (waiting for price data...)")
	}

	return strategies
}

// calculateSpreadStrategy 计算单向价差策略
// buyPrice: 买入价格数据，sellPrice: 卖出价格数据
func (ps *PriceStore) calculateSpreadStrategy(buyPrice, sellPrice *common.Price) *CustomStrategy {
	// 获取实际使用的价格
	askPrice := buyPrice.AskPrice
	if askPrice == 0 {
		askPrice = buyPrice.Price
	}

	bidPrice := sellPrice.BidPrice
	if bidPrice == 0 {
		bidPrice = sellPrice.Price
	}

	if askPrice == 0 || bidPrice == 0 {
		return nil
	}

	// 计算价差
	spreadAbsolute := bidPrice - askPrice
	spreadPercent := (spreadAbsolute / askPrice) * 100

	// 币种名称（去掉USDT后缀）
	coinName := buyPrice.Symbol
	if len(coinName) > 4 && coinName[len(coinName)-4:] == "USDT" {
		coinName = coinName[:len(coinName)-4]
	}

	// 构建策略名称和描述
	name := fmt.Sprintf("%s 价差套利: %s(%s) -> %s(%s)",
		coinName,
		buyPrice.Exchange,
		buyPrice.MarketType,
		sellPrice.Exchange,
		sellPrice.MarketType)

	description := fmt.Sprintf("买入 %s %s %s，卖出 %s %s %s",
		buyPrice.Exchange,
		buyPrice.MarketType,
		coinName,
		sellPrice.Exchange,
		sellPrice.MarketType,
		coinName)

	formula := fmt.Sprintf("(Sell Bid - Buy Ask) / Buy Ask * 100 = (%.4f - %.4f) / %.4f * 100",
		bidPrice, askPrice, askPrice)

	// 使用较新的更新时间
	updatedAt := buyPrice.LastUpdated
	if sellPrice.LastUpdated.After(updatedAt) {
		updatedAt = sellPrice.LastUpdated
	}

	return &CustomStrategy{
		Name:         name,
		Description:  description,
		Formula:      formula,
		StrategyType: "spread",
		Value:        spreadAbsolute,
		ValuePercent: spreadPercent,
		Components: []CustomStrategyToken{
			{
				Symbol:      coinName,
				Coefficient: 1.0,
				Exchange:    buyPrice.Exchange,
				MarketType:  buyPrice.MarketType,
				Price:       askPrice,
				Available:   true,
			},
			{
				Symbol:      coinName,
				Coefficient: -1.0,
				Exchange:    sellPrice.Exchange,
				MarketType:  sellPrice.MarketType,
				Price:       bidPrice,
				Available:   true,
			},
		},
		LastUpdated: updatedAt,
		Status:      "ready",
	}
}
