package aster

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient WebSocket客户端
type WSClient struct {
	URL            string
	Conn           *websocket.Conn
	MarketType     common.MarketType
	mu             sync.RWMutex
	subscriptions  map[string]bool
	messageHandler func(*WSMessage)
	reconnect      bool
	done           chan struct{}
}

// WSMessage WebSocket消息
type WSMessage struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

// WSBookTickerData 最优挂单数据
type WSBookTickerData struct {
	UpdateID int64  `json:"u"`
	Symbol   string `json:"s"`
	BidPrice string `json:"b"`
	BidQty   string `json:"B"`
	AskPrice string `json:"a"`
	AskQty   string `json:"A"`
	Time     int64  `json:"T"`
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

	w.mu.Lock()
	w.Conn = conn
	w.mu.Unlock()

	log.Printf("WebSocket connected to %s (%s)", w.URL, w.MarketType)

	// 启动消息读取
	go w.readMessages()

	// 启动心跳
	go w.keepAlive()

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

			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				return
			}

			// 解析消息
			var wsMsg WSMessage
			if err := json.Unmarshal(message, &wsMsg); err != nil {
				// 可能是其他类型的消息（如订阅确认），忽略
				continue
			}

			// 调用处理器
			w.mu.RLock()
			handler := w.messageHandler
			w.mu.RUnlock()

			if handler != nil {
				handler(&wsMsg)
			}
		}
	}
}

// keepAlive 保持连接活跃
func (w *WSClient) keepAlive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.mu.RLock()
			conn := w.Conn
			w.mu.RUnlock()

			if conn != nil {
				if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
					log.Printf("Failed to send ping: %v", err)
				}
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

// ParseBookTickerMessage 解析BookTicker消息
func ParseBookTickerMessage(data json.RawMessage) (*WSBookTickerData, error) {
	var ticker WSBookTickerData
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, err
	}
	return &ticker, nil
}

// ParseTickerMessage 解析Ticker消息
func ParseTickerMessage(data json.RawMessage) (*WSTickerData, error) {
	var ticker WSTickerData
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, err
	}
	return &ticker, nil
}

// ConvertWSBookTickerToPrice 将WebSocket BookTicker转换为通用价格
func ConvertWSBookTickerToPrice(ticker *WSBookTickerData, exchange common.Exchange, marketType common.MarketType) *common.Price {
	bidPrice := parseFloat(ticker.BidPrice)
	askPrice := parseFloat(ticker.AskPrice)

	return &common.Price{
		Symbol:      ticker.Symbol,
		Exchange:    exchange,
		MarketType:  marketType,
		Price:       (bidPrice + askPrice) / 2,
		BidPrice:    bidPrice,
		AskPrice:    askPrice,
		BidQty:      parseFloat(ticker.BidQty),
		AskQty:      parseFloat(ticker.AskQty),
		Timestamp:   time.UnixMilli(ticker.Time),
		LastUpdated: time.Now(),
	}
}

// BuildBookTickerStreams 构建BookTicker流
func BuildBookTickerStreams(symbols []string) []string {
	streams := make([]string, len(symbols))
	for i, symbol := range symbols {
		streams[i] = strings.ToLower(symbol) + "@bookTicker"
	}
	return streams
}

// BuildTickerStreams 构建Ticker流
func BuildTickerStreams(symbols []string) []string {
	streams := make([]string, len(symbols))
	for i, symbol := range symbols {
		streams[i] = strings.ToLower(symbol) + "@ticker"
	}
	return streams
}
