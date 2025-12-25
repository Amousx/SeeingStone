package aster

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient WebSocket客户端
type WSClient struct {
	URL               string
	Conn              *websocket.Conn
	MarketType        common.MarketType
	mu                sync.RWMutex
	subscriptions     map[string]bool
	messageHandler    func(*WSMessage)
	bookTickerHandler func(*WSBookTickerData)
	miniTickerHandler func([]*WSMiniTickerData)
	reconnect         bool
	done              chan struct{}
	connectedAt       time.Time
	lastPongTime      time.Time
}

// WSMessage WebSocket消息 (Combined Stream 格式)
type WSMessage struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

// WSBookTickerData 最优挂单数据（实时bookTicker）
type WSBookTickerData struct {
	EventType string `json:"e"` // 事件类型 "bookTicker"
	UpdateID  int64  `json:"u"` // 更新ID
	EventTime int64  `json:"E"` // 事件推送时间（毫秒）
	TxnTime   int64  `json:"T"` // 撮合时间（毫秒）
	Symbol    string `json:"s"` // 交易对
	BidPrice  string `json:"b"` // 买单最优挂单价格
	BidQty    string `json:"B"` // 买单最优挂单数量
	AskPrice  string `json:"a"` // 卖单最优挂单价格
	AskQty    string `json:"A"` // 卖单最优挂单数量
}

// WSTickerData Ticker数据
type WSTickerData struct {
	EventType          string `json:"e"`
	EventTime          int64  `json:"E"`
	Symbol             string `json:"s"`
	PriceChange        string `json:"p"`
	PriceChangePercent string `json:"P"`
	LastPrice          string `json:"c"`
	Volume             string `json:"v"`
	QuoteVolume        string `json:"q"`
}

// WSMiniTickerData MiniTicker数据
type WSMiniTickerData struct {
	EventType   string `json:"e"` // 事件类型: 24hrMiniTicker
	EventTime   int64  `json:"E"` // 事件时间
	Symbol      string `json:"s"` // 交易对
	LastPrice   string `json:"c"` // 最新成交价格
	OpenPrice   string `json:"o"` // 24小时前开始第一笔成交价格
	HighPrice   string `json:"h"` // 24小时内最高成交价
	LowPrice    string `json:"l"` // 24小时内最低成交价
	Volume      string `json:"v"` // 24小时内成交量
	QuoteVolume string `json:"q"` // 24小时内成交额
}

// NewWSClient 创建WebSocket客户端
func NewWSClient(url string, marketType common.MarketType) *WSClient {
	return &WSClient{
		URL:           url,
		MarketType:    marketType,
		subscriptions: make(map[string]bool),
		reconnect:     true,
		done:          make(chan struct{}),
	}
}

// Connect 连接WebSocket
func (w *WSClient) Connect() error {
	dialer := websocket.DefaultDialer
	dialer.HandshakeTimeout = 10 * time.Second

	conn, _, err := dialer.Dial(w.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to websocket: %w", err)
	}

	now := time.Now()
	w.mu.Lock()
	w.Conn = conn
	w.connectedAt = now
	w.lastPongTime = now
	w.mu.Unlock()

	log.Printf("WebSocket connected to %s (%s)", w.URL, w.MarketType)

	// 设置pong处理器
	conn.SetPongHandler(func(appData string) error {
		w.mu.Lock()
		w.lastPongTime = time.Now()
		w.mu.Unlock()
		return nil
	})

	// 启动消息读取
	go w.readMessages()

	// 启动24小时重连检查
	go w.check24HourReconnect()

	return nil
}

// Subscribe 订阅流
func (w *WSClient) Subscribe(streams []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.Conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	// 构建订阅消息
	msg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": streams,
		"id":     time.Now().Unix(),
	}

	if err := w.Conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// 记录订阅
	for _, stream := range streams {
		w.subscriptions[stream] = true
	}

	log.Printf("Subscribed to %d streams (%s)", len(streams), w.MarketType)

	return nil
}

// Unsubscribe 取消订阅
func (w *WSClient) Unsubscribe(streams []string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.Conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	msg := map[string]interface{}{
		"method": "UNSUBSCRIBE",
		"params": streams,
		"id":     time.Now().Unix(),
	}

	if err := w.Conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("failed to unsubscribe: %w", err)
	}

	// 删除订阅记录
	for _, stream := range streams {
		delete(w.subscriptions, stream)
	}

	return nil
}

// SetMessageHandler 设置消息处理器
func (w *WSClient) SetMessageHandler(handler func(*WSMessage)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messageHandler = handler
}

// SetBookTickerHandler 设置BookTicker处理器（推荐使用）
func (w *WSClient) SetBookTickerHandler(handler func(*WSBookTickerData)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.bookTickerHandler = handler
}

// SetMiniTickerHandler 设置MiniTicker处理器（仅用于成交量数据）
func (w *WSClient) SetMiniTickerHandler(handler func([]*WSMiniTickerData)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.miniTickerHandler = handler
}

// readMessages 读取消息
func (w *WSClient) readMessages() {
	defer func() {
		w.mu.Lock()
		if w.Conn != nil {
			w.Conn.Close()
		}
		w.mu.Unlock()

		// 如果需要重连
		if w.reconnect {
			log.Printf("Reconnecting WebSocket in 5 seconds... (%s)", w.MarketType)
			time.Sleep(5 * time.Second)
			if err := w.Connect(); err != nil {
				log.Printf("Failed to reconnect: %v", err)
			} else {
				// 重新订阅
				w.mu.RLock()
				streams := make([]string, 0, len(w.subscriptions))
				for stream := range w.subscriptions {
					streams = append(streams, stream)
				}
				w.mu.RUnlock()

				if len(streams) > 0 {
					if err := w.Subscribe(streams); err != nil {
						log.Printf("Failed to resubscribe: %v", err)
					}
				}
			}
		}
	}()

	for {
		select {
		case <-w.done:
			return
		default:
			w.mu.RLock()
			conn := w.Conn
			w.mu.RUnlock()

			if conn == nil {
				return
			}

			msgType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}

			// 处理Ping消息（服务端发送）
			if msgType == websocket.PingMessage {
				w.mu.RLock()
				c := w.Conn
				w.mu.RUnlock()
				if c != nil {
					if err := c.WriteMessage(websocket.PongMessage, nil); err != nil {
						log.Printf("Failed to send pong: %v", err)
					}
				}
				continue
			}

			// 1️⃣ 优先尝试解析 BookTicker（真实bid/ask）
			var bookTicker WSBookTickerData
			if err := json.Unmarshal(message, &bookTicker); err == nil && bookTicker.Symbol != "" && bookTicker.BidPrice != "" {
				// 打印 BTC/ETH/SOL 相关的数据用于调试
				if bookTicker.Symbol == "BTCUSDT" || bookTicker.Symbol == "ETHUSDT" || bookTicker.Symbol == "SOLUSDT" {
					log.Printf("[Aster WS %s] BookTicker %s: bid=%s, ask=%s, txnTime=%d, eventTime=%d",
						w.MarketType, bookTicker.Symbol, bookTicker.BidPrice, bookTicker.AskPrice, bookTicker.TxnTime, bookTicker.EventTime)
				}

				w.mu.RLock()
				handler := w.bookTickerHandler
				w.mu.RUnlock()

				if handler != nil {
					handler(&bookTicker)
				}
				continue
			}

			// 2️⃣ 如果不是 bookTicker，尝试解析为 MiniTicker 数组（向后兼容）
			var miniTickers []*WSMiniTickerData
			if err := json.Unmarshal(message, &miniTickers); err == nil && len(miniTickers) > 0 {
				w.mu.RLock()
				handler := w.miniTickerHandler
				w.mu.RUnlock()

				if handler != nil {
					handler(miniTickers)
				}
				continue
			}
		}
	}
}

// check24HourReconnect 检查24小时重连
// Aster WS 连接最长 24 小时，需要定期重连
func (w *WSClient) check24HourReconnect() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.mu.RLock()
			connectedAt := w.connectedAt
			w.mu.RUnlock()

			// 如果连接已经超过 23 小时，主动重连
			if time.Since(connectedAt) > 23*time.Hour {
				log.Printf("Connection has been up for >23 hours, reconnecting... (%s)", w.MarketType)
				w.mu.Lock()
				if w.Conn != nil {
					w.Conn.Close()
				}
				w.mu.Unlock()
				return // readMessages 中的 defer 会处理重连
			}
		}
	}
}

// checkPongTimeout 检查pong超时
// 服务端 5 分钟 ping，15 分钟内必须 pong
func (w *WSClient) checkPongTimeout() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.mu.RLock()
			lastPongTime := w.lastPongTime
			w.mu.RUnlock()

			// 如果超过 15 分钟没有收到服务端的 ping（没有机会 pong），可能连接有问题
			if time.Since(lastPongTime) > 15*time.Minute {
				log.Printf("No ping/pong for >15 minutes, reconnecting... (%s)", w.MarketType)
				w.mu.Lock()
				if w.Conn != nil {
					w.Conn.Close()
				}
				w.mu.Unlock()
				return
			}
		}
	}
}

// Close 关闭连接
func (w *WSClient) Close() {
	w.reconnect = false
	close(w.done)

	w.mu.Lock()
	if w.Conn != nil {
		w.Conn.Close()
		w.Conn = nil
	}
	w.mu.Unlock()
}

// ConvertWSBookTickerToPrice 将WebSocket BookTicker转换为通用价格（推荐）
func ConvertWSBookTickerToPrice(ticker *WSBookTickerData, exchange common.Exchange, marketType common.MarketType) *common.Price {
	bidPrice := parseFloat(ticker.BidPrice)
	askPrice := parseFloat(ticker.AskPrice)
	bidQty := parseFloat(ticker.BidQty)
	askQty := parseFloat(ticker.AskQty)

	// 计算中间价
	midPrice := (bidPrice + askPrice) / 2

	// 使用交易所时间（优先用TxnTime撮合时间，否则用EventTime事件时间）
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
		BidPrice:    bidPrice, // 真实bid价格
		AskPrice:    askPrice, // 真实ask价格
		BidQty:      bidQty,
		AskQty:      askQty,
		Volume24h:   0, // BookTicker不包含成交量
		Timestamp:   exchangeTimestamp, // 使用交易所时间
		LastUpdated: time.Now(),        // 本地接收时间
		Source:      common.PriceSourceWebSocket,
	}
}

// ConvertWSMiniTickerToPrice 将WebSocket MiniTicker转换为通用价格（不推荐）
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
