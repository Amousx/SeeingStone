package binance

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient Binance WebSocket 客户端
type WSClient struct {
	URL                string
	Conn               *websocket.Conn
	MarketType         common.MarketType
	mu                 sync.RWMutex
	subscriptions      map[string]bool
	bookTickerHandler  func(*WSBookTickerData)
	miniTickerHandler  func([]*WSMiniTickerData)
	reconnect          bool
	done               chan struct{}
	connectedAt        time.Time
	lastPongTime       time.Time
	subscriptionID     int
}

// NewWSClient 创建新的 WebSocket 客户端
func NewWSClient(url string, marketType common.MarketType) *WSClient {
	return &WSClient{
		URL:           url,
		MarketType:    marketType,
		subscriptions: make(map[string]bool),
		reconnect:     true,
		done:          make(chan struct{}),
	}
}

// SetBookTickerHandler 设置 BookTicker 处理器（推荐使用）
func (w *WSClient) SetBookTickerHandler(handler func(*WSBookTickerData)) {
	w.bookTickerHandler = handler
}

// SetMiniTickerHandler 设置 MiniTicker 处理器（仅用于成交量数据）
func (w *WSClient) SetMiniTickerHandler(handler func([]*WSMiniTickerData)) {
	w.miniTickerHandler = handler
}

// Connect 连接到 WebSocket
func (w *WSClient) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(w.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %v", w.URL, err)
	}

	w.mu.Lock()
	w.Conn = conn
	w.connectedAt = time.Now()
	w.lastPongTime = time.Now()
	w.mu.Unlock()

	log.Printf("[Binance WS] Connected to %s", w.URL)

	// 设置 Pong 处理器
	conn.SetPongHandler(func(appData string) error {
		w.mu.Lock()
		w.lastPongTime = time.Now()
		w.mu.Unlock()
		return nil
	})

	// 启动消息读取
	go w.readMessages()

	// 启动心跳检查（Binance 服务器每 20 秒发送 PING）
	go w.keepAlive()

	// 启动 24 小时重连检查
	go w.check24HourReconnect()

	return nil
}

// Subscribe 订阅 streams
func (w *WSClient) Subscribe(streams []string) error {
	if w.Conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	w.mu.Lock()
	w.subscriptionID++
	id := w.subscriptionID
	w.mu.Unlock()

	sub := SubscribeMessage{
		Method: "SUBSCRIBE",
		Params: streams,
		ID:     id,
	}

	if err := w.Conn.WriteJSON(sub); err != nil {
		return fmt.Errorf("failed to subscribe: %v", err)
	}

	w.mu.Lock()
	for _, stream := range streams {
		w.subscriptions[stream] = true
	}
	w.mu.Unlock()

	log.Printf("[Binance WS] Subscribed to %d streams (ID: %d)", len(streams), id)
	return nil
}

// SubscribeAll 订阅全市场 BookTicker（实时最优买卖价）
func (w *WSClient) SubscribeAll() error {
	// Binance 使用 !bookTicker 订阅全市场的实时最优买卖价
	// 注意：这是逐个推送，不是数组格式
	return w.Subscribe([]string{"!bookTicker"})
}

// SubscribeAllMiniTicker 订阅全市场 MiniTicker（仅用于24h成交量）
func (w *WSClient) SubscribeAllMiniTicker() error {
	// Binance 使用 !miniTicker@arr 订阅全市场
	return w.Subscribe([]string{"!miniTicker@arr"})
}

// readMessages 读取 WebSocket 消息
func (w *WSClient) readMessages() {
	log.Printf("[Binance WS] Starting readMessages loop")
	messageCount := 0

	defer func() {
		log.Printf("[Binance WS] readMessages exited (received %d messages total)", messageCount)
		if w.reconnect {
			log.Println("[Binance WS] Connection lost, reconnecting in 5 seconds...")
			time.Sleep(5 * time.Second)
			if err := w.Connect(); err != nil {
				log.Printf("[Binance WS] Failed to reconnect: %v", err)
			} else {
				log.Println("[Binance WS] Reconnected successfully")
				// 重新订阅
				w.mu.RLock()
				streams := make([]string, 0, len(w.subscriptions))
				for stream := range w.subscriptions {
					streams = append(streams, stream)
				}
				w.mu.RUnlock()

				if len(streams) > 0 {
					if err := w.Subscribe(streams); err != nil {
						log.Printf("[Binance WS] Failed to resubscribe: %v", err)
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

			// 设置读取超时（120 秒）- Binance 正常 20 秒一个 PING
			conn.SetReadDeadline(time.Now().Add(120 * time.Second))

			msgType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("[Binance WS] WebSocket connection closed unexpectedly: %v", err)
				} else if err != nil {
					log.Printf("[Binance WS] Read error (may be timeout): %v", err)
				}
				return
			}

			// 处理 PING 消息（Binance 服务器主动发送）
			if msgType == websocket.PingMessage {
				w.mu.RLock()
				c := w.Conn
				w.mu.RUnlock()
				if c != nil {
					// 回复 PONG，payload 与 PING 一致
					if err := c.WriteMessage(websocket.PongMessage, message); err != nil {
						log.Printf("[Binance WS] Failed to send PONG: %v", err)
					}
				}
				continue
			}

			messageCount++
			if messageCount%100 == 0 {
				log.Printf("[Binance WS] Received %d messages so far", messageCount)
			}

			w.processMessage(message)
		}
	}
}

// processMessage 处理接收到的消息
func (w *WSClient) processMessage(message []byte) {
	// 1️⃣ 先尝试解析 BookTicker（优先处理，因为这是我们想要的）
	var bookTicker WSBookTickerData
	if err := json.Unmarshal(message, &bookTicker); err == nil && bookTicker.Symbol != "" && bookTicker.BidPrice != "" {
		// 打印BTC/ETH/SOL的bookTicker数据用于调试
		if bookTicker.Symbol == "BTCUSDT" || bookTicker.Symbol == "ETHUSDT" || bookTicker.Symbol == "SOLUSDT" {
			log.Printf("[Binance WS %s] BookTicker %s: bid=%s, ask=%s, txnTime=%d, eventTime=%d",
				w.MarketType, bookTicker.Symbol, bookTicker.BidPrice, bookTicker.AskPrice, bookTicker.TxnTime, bookTicker.EventTime)
		}

		w.mu.RLock()
		handler := w.bookTickerHandler
		w.mu.RUnlock()

		if handler != nil {
			handler(&bookTicker)
		}
		return
	}

	// 2️⃣ 尝试解析 Combined Stream 格式 {"stream":"...", "data":...}
	var wsMsg WSMessage
	if err := json.Unmarshal(message, &wsMsg); err == nil && len(wsMsg.Data) > 0 {
		// 尝试解析 Combined Stream 中的 BookTicker
		var bookTickerCombined WSBookTickerData
		if err := json.Unmarshal(wsMsg.Data, &bookTickerCombined); err == nil && bookTickerCombined.Symbol != "" && bookTickerCombined.BidPrice != "" {
			w.mu.RLock()
			handler := w.bookTickerHandler
			w.mu.RUnlock()

			if handler != nil {
				handler(&bookTickerCombined)
			}
			return
		}

		// Combined Stream 格式 - MiniTicker数组
		var miniTickers []*WSMiniTickerData
		if err := json.Unmarshal(wsMsg.Data, &miniTickers); err == nil && len(miniTickers) > 0 {
			w.mu.RLock()
			handler := w.miniTickerHandler
			w.mu.RUnlock()

			if handler != nil {
				handler(miniTickers)
			}
			return
		}

		// 尝试解析单个 MiniTicker
		var singleTicker WSMiniTickerData
		if err := json.Unmarshal(wsMsg.Data, &singleTicker); err == nil && singleTicker.EventType == "24hrMiniTicker" {
			w.mu.RLock()
			handler := w.miniTickerHandler
			w.mu.RUnlock()

			if handler != nil {
				handler([]*WSMiniTickerData{&singleTicker})
			}
			return
		}
	}

	// 3️⃣ 如果不是 Combined Stream 格式，尝试直接解析为 MiniTicker 数组
	var miniTickers []*WSMiniTickerData
	if err := json.Unmarshal(message, &miniTickers); err == nil && len(miniTickers) > 0 {
		w.mu.RLock()
		handler := w.miniTickerHandler
		w.mu.RUnlock()

		if handler != nil {
			handler(miniTickers)
		}
		return
	}

	// 4️⃣ 尝试解析单个 MiniTicker（直接格式）
	var singleTicker WSMiniTickerData
	if err := json.Unmarshal(message, &singleTicker); err == nil && singleTicker.EventType == "24hrMiniTicker" {
		w.mu.RLock()
		handler := w.miniTickerHandler
		w.mu.RUnlock()

		if handler != nil {
			handler([]*WSMiniTickerData{&singleTicker})
		}
		return
	}

	// 5️⃣ 如果所有格式都无法解析，打印警告（每100条打印一次）
	w.mu.Lock()
	if w.subscriptionID%100 == 0 {
		log.Printf("[Binance WS %s] Warning: Unable to parse message format: %s", w.MarketType, string(message[:min(200, len(message))]))
	}
	w.subscriptionID++
	w.mu.Unlock()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// keepAlive 保持连接活跃（Binance 服务器会主动发送 PING，这里只是监控）
func (w *WSClient) keepAlive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.done:
			return
		case <-ticker.C:
			w.mu.RLock()
			lastPong := w.lastPongTime
			w.mu.RUnlock()

			// 如果超过 90 秒没有收到 PONG（正常应该每 20 秒收到 PING），可能连接有问题
			if time.Since(lastPong) > 90*time.Second {
				log.Printf("[Binance WS] Warning: No PONG received for %.0fs, connection may be dead", time.Since(lastPong).Seconds())
			}
		}
	}
}

// check24HourReconnect 检查 24 小时重连
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

			// 如果连接超过 23 小时，主动断开重连
			if time.Since(connectedAt) > 23*time.Hour {
				log.Println("[Binance WS] Connection approaching 24h limit, reconnecting...")
				w.Close()
				time.Sleep(2 * time.Second)
				if err := w.Connect(); err != nil {
					log.Printf("[Binance WS] Failed to reconnect: %v", err)
				} else {
					log.Println("[Binance WS] Reconnected successfully")
					// 重新订阅
					w.mu.RLock()
					streams := make([]string, 0, len(w.subscriptions))
					for stream := range w.subscriptions {
						streams = append(streams, stream)
					}
					w.mu.RUnlock()

					if len(streams) > 0 {
						if err := w.Subscribe(streams); err != nil {
							log.Printf("[Binance WS] Failed to resubscribe: %v", err)
						}
					}
				}
			}
		}
	}
}

// Close 关闭连接
func (w *WSClient) Close() error {
	w.reconnect = false
	close(w.done)

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.Conn != nil {
		return w.Conn.Close()
	}
	return nil
}

// parseFloat 解析字符串为 float64
func parseFloat(s string) float64 {
	if s == "" {
		return 0
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return f
}
