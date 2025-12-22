package binance

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"time"
)

// WSMiniTickerData WebSocket MiniTicker 数据
type WSMiniTickerData struct {
	EventType   string `json:"e"` // 事件类型: 24hrMiniTicker
	EventTime   int64  `json:"E"` // 事件时间 (毫秒)
	Symbol      string `json:"s"` // 交易对
	LastPrice   string `json:"c"` // 最新成交价格
	OpenPrice   string `json:"o"` // 24小时前开始第一笔成交价格
	HighPrice   string `json:"h"` // 24小时内最高成交价
	LowPrice    string `json:"l"` // 24小时内最低成交价
	Volume      string `json:"v"` // 24小时内成交量（基础资产）
	QuoteVolume string `json:"q"` // 24小时内成交额（报价资产）
}

// WSMessage WebSocket 组合 Stream 消息
type WSMessage struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

// SubscribeMessage 订阅消息
type SubscribeMessage struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     int      `json:"id"`
}

// UnsubscribeMessage 取消订阅消息
type UnsubscribeMessage struct {
	Method string   `json:"method"`
	Params []string `json:"params"`
	ID     int      `json:"id"`
}

// MarketType 市场类型
type MarketType string

const (
	MarketTypeSpot   MarketType = "SPOT"
	MarketTypeFuture MarketType = "FUTURE"
)

// ExchangeInfo 交易所信息
type ExchangeInfo struct {
	Timezone   string         `json:"timezone"`
	ServerTime int64          `json:"serverTime"`
	Symbols    []SymbolInfo   `json:"symbols"`
}

// SymbolInfo 交易对信息
type SymbolInfo struct {
	Symbol     string `json:"symbol"`
	Status     string `json:"status"`
	BaseAsset  string `json:"baseAsset"`
	QuoteAsset string `json:"quoteAsset"`
}

// ConvertWSMiniTickerToPrice 将 WebSocket MiniTicker 转换为通用 Price
func ConvertWSMiniTickerToPrice(ticker *WSMiniTickerData, exchange common.Exchange, marketType common.MarketType) *common.Price {
	price := parseFloat(ticker.LastPrice)
	quoteVolume := parseFloat(ticker.QuoteVolume)

	return &common.Price{
		Symbol:      ticker.Symbol,
		Exchange:    exchange,
		MarketType:  marketType,
		Price:       price,
		BidPrice:    price, // MiniTicker不提供买卖价，使用LastPrice作为近似值
		AskPrice:    price, // MiniTicker不提供买卖价，使用LastPrice作为近似值
		BidQty:      0,
		AskQty:      0,
		Volume24h:   quoteVolume,
		Timestamp:   time.UnixMilli(ticker.EventTime),
		LastUpdated: time.Now(),
	}
}
