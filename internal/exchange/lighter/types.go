package lighter

import (
	"encoding/json"
)

// WebSocket 消息类型
type WSMessage struct {
	Type    string          `json:"type"`
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// 订阅消息
type SubscribeMessage struct {
	Type    string `json:"type"`
	Channel string `json:"channel"`
}

// Order Book 数据
type OrderBookUpdate struct {
	Channel   string          `json:"channel"`
	Offset    int64           `json:"offset"`
	OrderBook OrderBookData   `json:"order_book"`
	Type      string          `json:"type"`
}

type OrderBookData struct {
	Code       int           `json:"code"`
	MarketID   int           `json:"market_id,omitempty"` // 用于 order_book/all
	Asks       []PriceLevel  `json:"asks"`
	Bids       []PriceLevel  `json:"bids"`
	BeginNonce int64         `json:"begin_nonce,omitempty"` // 用于增量更新的连续性验证
	Nonce      int64         `json:"nonce"`
	Timestamp  int64         `json:"timestamp"`
}

type PriceLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// Market Stats 数据
type MarketStatsUpdate struct {
	Channel     string           `json:"channel"`
	MarketStats MarketStatsData  `json:"market_stats"`
	Type        string           `json:"type"`
}

type MarketStatsData struct {
	MarketID               int     `json:"market_id"`
	IndexPrice             string  `json:"index_price"`
	MarkPrice              string  `json:"mark_price"`
	OpenInterest           string  `json:"open_interest"`
	LastTradePrice         string  `json:"last_trade_price"`
	CurrentFundingRate     string  `json:"current_funding_rate"`
	FundingRate            string  `json:"funding_rate"`
	FundingTimestamp       int64   `json:"funding_timestamp"`
	DailyBaseTokenVolume   float64 `json:"daily_base_token_volume"`
	DailyQuoteTokenVolume  float64 `json:"daily_quote_token_volume"`
	DailyPriceLow          float64 `json:"daily_price_low"`
	DailyPriceHigh         float64 `json:"daily_price_high"`
	DailyPriceChange       float64 `json:"daily_price_change"`
}

// Market 信息（从配置或 API 获取）
type Market struct {
	MarketID int    `json:"market_id"`
	Symbol   string `json:"symbol"`
	Type     string `json:"type"` // "perp" 或 "spot"
}

// Order 订单结构（本地维护）
type Order struct {
	Price  float64
	Amount float64
}
