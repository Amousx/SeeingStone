package binance

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// SpotWSPool Binance 现货 WebSocket 连接池
// 解决现货不支持 !bookTicker 全量流的问题
type SpotWSPool struct {
	symbols           []string                // 所有需要订阅的 symbol
	connections       []*SpotWSConnection     // WebSocket 连接池
	bookTickerHandler func(*WSBookTickerData) // BookTicker 处理器
	symbolsPerConn    int                     // 每个连接订阅的 symbol 数量
	mu                sync.RWMutex
	done              chan struct{}
}

// SpotWSConnection 单个 WebSocket 连接
type SpotWSConnection struct {
	ID                int
	URL               string
	Conn              *websocket.Conn
	Symbols           []string
	mu                sync.RWMutex
	reconnect         bool
	done              chan struct{}
	connectedAt       time.Time
	lastPongTime      time.Time
	bookTickerHandler func(*WSBookTickerData)
}

// NewSpotWSPool 创建现货 WebSocket 连接池
func NewSpotWSPool(symbols []string, symbolsPerConn int) *SpotWSPool {
	if symbolsPerConn <= 0 {
		symbolsPerConn = 50 // 默认每个连接 50 个 symbol
	}

	return &SpotWSPool{
		symbols:        symbols,
		connections:    make([]*SpotWSConnection, 0),
		symbolsPerConn: symbolsPerConn,
		done:           make(chan struct{}),
	}
}

// SetBookTickerHandler 设置 BookTicker 处理器
func (p *SpotWSPool) SetBookTickerHandler(handler func(*WSBookTickerData)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.bookTickerHandler = handler
}

// Start 启动连接池
func (p *SpotWSPool) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 计算需要的连接数
	numConnections := (len(p.symbols) + p.symbolsPerConn - 1) / p.symbolsPerConn
	log.Printf("[Binance Spot Pool] Starting %d WebSocket connections for %d symbols (%d symbols/conn)",
		numConnections, len(p.symbols), p.symbolsPerConn)

	// 创建连接
	for i := 0; i < numConnections; i++ {
		startIdx := i * p.symbolsPerConn
		endIdx := startIdx + p.symbolsPerConn
		if endIdx > len(p.symbols) {
			endIdx = len(p.symbols)
		}

		symbols := p.symbols[startIdx:endIdx]
		conn := NewSpotWSConnection(i, symbols)
		conn.SetBookTickerHandler(p.bookTickerHandler)

		if err := conn.Connect(); err != nil {
			log.Printf("[Binance Spot Pool] Failed to start connection #%d: %v", i, err)
			continue
		}

		p.connections = append(p.connections, conn)
	}

	log.Printf("[Binance Spot Pool] Successfully started %d/%d connections", len(p.connections), numConnections)
	return nil
}

// Close 关闭所有连接
func (p *SpotWSPool) Close() {
	close(p.done)

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.connections {
		conn.Close()
	}
}

// NewSpotWSConnection 创建单个 WebSocket 连接
func NewSpotWSConnection(id int, symbols []string) *SpotWSConnection {
	return &SpotWSConnection{
		ID:        id,
		URL:       "wss://stream.binance.com:9443/ws",
		Symbols:   symbols,
		reconnect: true,
		done:      make(chan struct{}),
	}
}

// SetBookTickerHandler 设置处理器
func (c *SpotWSConnection) SetBookTickerHandler(handler func(*WSBookTickerData)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bookTickerHandler = handler
}

// Connect 连接到 WebSocket
func (c *SpotWSConnection) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}

	now := time.Now()
	c.mu.Lock()
	c.Conn = conn
	c.connectedAt = now
	c.lastPongTime = now
	c.mu.Unlock()

	//log.Printf("[Binance Spot #%d] Connected, subscribing to %d symbols", c.ID, len(c.Symbols))

	// 设置 Pong 处理器
	conn.SetPongHandler(func(appData string) error {
		c.mu.Lock()
		c.lastPongTime = time.Now()
		c.mu.Unlock()
		return nil
	})

	// 订阅 symbol
	if err := c.subscribe(); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// 启动消息读取
	go c.readMessages()

	// 启动心跳和重连检查
	go c.keepAlive()
	go c.check24HourReconnect()

	return nil
}

// subscribe 订阅交易对
func (c *SpotWSConnection) subscribe() error {
	c.mu.RLock()
	symbols := c.Symbols
	conn := c.Conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("connection not established")
	}

	// 构建订阅流列表：symbol1@bookTicker, symbol2@bookTicker, ...
	streams := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		// Binance 要求小写
		stream := fmt.Sprintf("%s@bookTicker", toLower(symbol))
		streams = append(streams, stream)
	}

	// 发送订阅消息
	msg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": streams,
		"id":     c.ID,
	}

	if err := conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("failed to send subscribe message: %w", err)
	}

	log.Printf("[Binance Spot #%d] Subscribed to %d bookTicker streams", c.ID, len(streams))
	return nil
}

// readMessages 读取消息
func (c *SpotWSConnection) readMessages() {
	messageCount := 0

	defer func() {
		log.Printf("[Binance Spot #%d] readMessages exited (received %d messages)", c.ID, messageCount)

		c.mu.Lock()
		if c.Conn != nil {
			c.Conn.Close()
		}
		c.mu.Unlock()

		// 重连
		if c.reconnect {
			log.Printf("[Binance Spot #%d] Reconnecting in 5 seconds...", c.ID)
			time.Sleep(5 * time.Second)
			if err := c.Connect(); err != nil {
				log.Printf("[Binance Spot #%d] Failed to reconnect: %v", c.ID, err)
			}
		}
	}()

	for {
		select {
		case <-c.done:
			return
		default:
			c.mu.RLock()
			conn := c.Conn
			c.mu.RUnlock()

			if conn == nil {
				return
			}

			// 设置读取超时
			conn.SetReadDeadline(time.Now().Add(120 * time.Second))

			msgType, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("[Binance Spot #%d] Connection closed unexpectedly: %v", c.ID, err)
				}
				return
			}

			// 处理 PING 消息
			if msgType == websocket.PingMessage {
				c.mu.RLock()
				conn := c.Conn
				c.mu.RUnlock()
				if conn != nil {
					conn.WriteMessage(websocket.PongMessage, message)
				}
				continue
			}

			messageCount++
			c.processMessage(message)
		}
	}
}

// processMessage 处理消息
func (c *SpotWSConnection) processMessage(message []byte) {
	// 尝试解析 BookTicker
	var bookTicker WSBookTickerData
	if err := json.Unmarshal(message, &bookTicker); err == nil && bookTicker.Symbol != "" && bookTicker.BidPrice != "" {
		c.mu.RLock()
		handler := c.bookTickerHandler
		c.mu.RUnlock()

		if handler != nil {
			handler(&bookTicker)
		}
		return
	}

	// 尝试解析 Combined Stream 格式
	var wsMsg WSMessage
	if err := json.Unmarshal(message, &wsMsg); err == nil && len(wsMsg.Data) > 0 {
		var bookTickerCombined WSBookTickerData
		if err := json.Unmarshal(wsMsg.Data, &bookTickerCombined); err == nil && bookTickerCombined.Symbol != "" && bookTickerCombined.BidPrice != "" {
			c.mu.RLock()
			handler := c.bookTickerHandler
			c.mu.RUnlock()

			if handler != nil {
				handler(&bookTickerCombined)
			}
			return
		}
	}

	// 忽略订阅确认等其他消息
}

// keepAlive 心跳检查
func (c *SpotWSConnection) keepAlive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.RLock()
			lastPong := c.lastPongTime
			c.mu.RUnlock()

			if time.Since(lastPong) > 90*time.Second {
				log.Printf("[Binance Spot #%d] No PONG for %.0fs, connection may be dead", c.ID, time.Since(lastPong).Seconds())
			}
		}
	}
}

// check24HourReconnect 检查 24 小时重连
func (c *SpotWSConnection) check24HourReconnect() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.RLock()
			connectedAt := c.connectedAt
			c.mu.RUnlock()

			if time.Since(connectedAt) > 23*time.Hour {
				log.Printf("[Binance Spot #%d] Connection approaching 24h limit, reconnecting...", c.ID)
				c.mu.Lock()
				if c.Conn != nil {
					c.Conn.Close()
				}
				c.mu.Unlock()
				return // defer 中会重连
			}
		}
	}
}

// Close 关闭连接
func (c *SpotWSConnection) Close() {
	c.reconnect = false
	close(c.done)

	c.mu.Lock()
	if c.Conn != nil {
		c.Conn.Close()
		c.Conn = nil
	}
	c.mu.Unlock()
}

// toLower 转小写
func toLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c = c + ('a' - 'A')
		}
		result[i] = c
	}
	return string(result)
}
