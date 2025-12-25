package binance

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"time"
)

// WSBookTickerData WebSocket BookTicker 数据（实时最优买卖价）
type WSBookTickerData struct {
	EventType string `json:"e"` // 事件类型 "bookTicker"
	UpdateID  int64  `json:"u"` // Order book updateId
	EventTime int64  `json:"E"` // 事件推送时间（毫秒）
	TxnTime   int64  `json:"T"` // 撮合时间（毫秒）- 期货有此字段
	Symbol    string `json:"s"` // 交易对
	BidPrice  string `json:"b"` // 最优买价（真实bid）
	BidQty    string `json:"B"` // 最优买量
	AskPrice  string `json:"a"` // 最优卖价（真实ask）
	AskQty    string `json:"A"` // 最优卖量
}

// WSMiniTickerData WebSocket MiniTicker 数据（仅用于获取24h成交量）
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

// ConvertWSBookTickerToPrice 将 WebSocket BookTicker 转换为通用 Price（推荐使用）
func ConvertWSBookTickerToPrice(ticker *WSBookTickerData, exchange common.Exchange, marketType common.MarketType) *common.Price {
	bidPrice := parseFloat(ticker.BidPrice)
	askPrice := parseFloat(ticker.AskPrice)
	bidQty := parseFloat(ticker.BidQty)
	askQty := parseFloat(ticker.AskQty)

	// 计算中间价
	midPrice := (bidPrice + askPrice) / 2

	// 确定交易所时间戳（期货优先用TxnTime撮合时间，否则用EventTime事件时间）
	var exchangeTimestamp time.Time
	if ticker.TxnTime > 0 {
		exchangeTimestamp = time.UnixMilli(ticker.TxnTime)
	} else if ticker.EventTime > 0 {
		exchangeTimestamp = time.UnixMilli(ticker.EventTime)
	} else {
		exchangeTimestamp = time.Now() // fallback
	}

	return &common.Price{
		Symbol:      ticker.Symbol,
		Exchange:    exchange,
		MarketType:  marketType,
		Price:       midPrice,
		BidPrice:    bidPrice,  // 真实bid价格
		AskPrice:    askPrice,  // 真实ask价格
		BidQty:      bidQty,
		AskQty:      askQty,
		Volume24h:   0, // BookTicker不包含成交量，需要从其他地方获取
		Timestamp:   exchangeTimestamp, // 使用交易所时间
		LastUpdated: time.Now(),        // 本地接收时间
		Source:      common.PriceSourceWebSocket,
	}
}

// ConvertWSMiniTickerToPrice 将 WebSocket MiniTicker 转换为通用 Price（不推荐，仅用于成交量）
// 注意：MiniTicker只有last trade price，没有真实的bid/ask，会导致系统误差
func ConvertWSMiniTickerToPrice(ticker *WSMiniTickerData, exchange common.Exchange, marketType common.MarketType) *common.Price {
	price := parseFloat(ticker.LastPrice)
	quoteVolume := parseFloat(ticker.QuoteVolume)

	return &common.Price{
		Symbol:      ticker.Symbol,
		Exchange:    exchange,
		MarketType:  marketType,
		Price:       price,
		BidPrice:    0, // MiniTicker没有真实bid/ask，不要伪造
		AskPrice:    0,
		BidQty:      0,
		AskQty:      0,
		Volume24h:   quoteVolume,
		Timestamp:   time.UnixMilli(ticker.EventTime), // 使用交易所时间
		LastUpdated: time.Now(),                       // 本地接收时间
		Source:      common.PriceSourceWebSocket,
	}
}
