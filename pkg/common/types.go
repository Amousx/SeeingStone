package common

import "time"

// MarketType 市场类型
type MarketType string

const (
	MarketTypeSpot   MarketType = "SPOT"
	MarketTypeFuture MarketType = "FUTURE"
)

// Exchange 交易所名称
type Exchange string

const (
	ExchangeAster       Exchange = "ASTER"
	ExchangeBinance     Exchange = "BINANCE"
	ExchangeBitget      Exchange = "BITGET"
	ExchangeBybit       Exchange = "BYBIT"
	ExchangeGate        Exchange = "GATE"
	ExchangeHyperliquid Exchange = "HYPERLIQUID"
	ExchangeLighter     Exchange = "LIGHTER"
	ExchangeOKX         Exchange = "OKX" // OKX DEX (链上价格)
)

// PriceSource 价格数据来源
type PriceSource string

const (
	PriceSourceWebSocket PriceSource = "WEBSOCKET" // WebSocket实时数据
	PriceSourceREST      PriceSource = "REST"      // REST API数据
)

// Price 价格信息
type Price struct {
	Symbol      string      `json:"symbol"`
	Exchange    Exchange    `json:"exchange"`
	MarketType  MarketType  `json:"market_type"`
	Price       float64     `json:"price"`        // 中间价或标记价
	BidPrice    float64     `json:"bid_price"`    // 买一价（真实bid，不是伪造）
	AskPrice    float64     `json:"ask_price"`    // 卖一价（真实ask，不是伪造）
	BidQty      float64     `json:"bid_qty"`      // 买一量
	AskQty      float64     `json:"ask_qty"`      // 卖一量
	Volume24h   float64     `json:"volume_24h"`   // 24h成交量
	Timestamp   time.Time   `json:"timestamp"`    // 交易所行情时间（关键！）
	LastUpdated time.Time   `json:"last_updated"` // 本地接收时间（用于过期判断）
	Source      PriceSource `json:"source"`       // 数据来源：WebSocket或REST

	// === Quote Normalization 扩展字段 ===
	QuoteCurrency      QuoteCurrency `json:"quote_currency"`        // 原始报价货币
	OriginalBidPrice   float64       `json:"original_bid_price"`    // 原始bid价格(转换前)
	OriginalAskPrice   float64       `json:"original_ask_price"`    // 原始ask价格(转换前)
	ExchangeRate       float64       `json:"exchange_rate"`         // 使用的汇率
	ExchangeRateSource string        `json:"exchange_rate_source"`  // 汇率来源
	IsNormalized       bool          `json:"is_normalized"`         // 是否已标准化
}

// NormalizeToUSDT 标准化价格到USDT
func (p *Price) NormalizeToUSDT(rate float64, rateSource string) {
	if p.QuoteCurrency == QuoteCurrencyUSDT {
		// 已经是USDT,无需转换
		p.IsNormalized = true
		p.ExchangeRate = 1.0
		p.ExchangeRateSource = "IDENTITY"
		return
	}

	// 保存原始价格
	p.OriginalBidPrice = p.BidPrice
	p.OriginalAskPrice = p.AskPrice

	// 应用汇率转换
	p.BidPrice = p.BidPrice * rate
	p.AskPrice = p.AskPrice * rate
	p.Price = (p.BidPrice + p.AskPrice) / 2

	// 记录转换信息
	p.ExchangeRate = rate
	p.ExchangeRateSource = rateSource
	p.IsNormalized = true
}

// ArbitrageOpportunity 套利机会
type ArbitrageOpportunity struct {
	ID               string     `json:"id"`
	Symbol           string     `json:"symbol"`
	Type             string     `json:"type"` // "spot-spot", "spot-future", "future-future"
	Exchange1        Exchange   `json:"exchange1"`
	Exchange2        Exchange   `json:"exchange2"`
	Market1Type      MarketType `json:"market1_type"`
	Market2Type      MarketType `json:"market2_type"`
	Price1           float64    `json:"price1"`
	Price2           float64    `json:"price2"`
	SpreadPercent    float64    `json:"spread_percent"`
	SpreadAbsolute   float64    `json:"spread_absolute"`
	Volume24h        float64    `json:"volume_24h"`
	ProfitPotential  float64    `json:"profit_potential"`
	Timestamp        time.Time  `json:"timestamp"`
	NotificationSent bool       `json:"notification_sent"`
}

// Ticker WebSocket ticker 数据
type Ticker struct {
	Symbol    string    `json:"symbol"`
	Price     float64   `json:"price"`
	BidPrice  float64   `json:"bid_price"`
	AskPrice  float64   `json:"ask_price"`
	Volume    float64   `json:"volume"`
	Timestamp time.Time `json:"timestamp"`
}

// OrderBook 订单簿
type OrderBook struct {
	Symbol    string      `json:"symbol"`
	Bids      [][]float64 `json:"bids"` // [price, quantity]
	Asks      [][]float64 `json:"asks"`
	Timestamp time.Time   `json:"timestamp"`
}
