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
)

// Price 价格信息
type Price struct {
	Symbol      string     `json:"symbol"`
	Exchange    Exchange   `json:"exchange"`
	MarketType  MarketType `json:"market_type"`
	Price       float64    `json:"price"`
	BidPrice    float64    `json:"bid_price"`
	AskPrice    float64    `json:"ask_price"`
	BidQty      float64    `json:"bid_qty"`
	AskQty      float64    `json:"ask_qty"`
	Volume24h   float64    `json:"volume_24h"`
	Timestamp   time.Time  `json:"timestamp"`
	LastUpdated time.Time  `json:"last_updated"`
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
