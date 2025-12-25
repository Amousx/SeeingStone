package lighter

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSPool Lighter WebSocket 连接池
// 解决 order_book/all 不支持的问题，使用分片订阅模式
type WSPool struct {
	markets           []*Market                   // 所有需要订阅的市场
	connections       []*WSPoolConnection         // WebSocket 连接池
	priceHandler      func(*common.Price)         // 价格处理器
	marketsPerConn    int                         // 每个连接订阅的市场数量
	mu                sync.RWMutex
	done              chan struct{}
}

// WSPoolConnection 单个 WebSocket 连接
type WSPoolConnection struct {
	ID                int
	URL               string
	Conn              *websocket.Conn
	Markets           []*Market
	orderBookData     map[int]*OrderBookData
	marketStatsData   map[int]*MarketStatsData
	mu                sync.RWMutex
	reconnect         bool
	done              chan struct{}
	connectedAt       time.Time
	lastPongTime      time.Time
	priceHandler      func(*common.Price)
}

// NewWSPool 创建 Lighter WebSocket 连接池
func NewWSPool(markets []*Market, marketsPerConn int) *WSPool {
	if marketsPerConn <= 0 {
		marketsPerConn = 60 // 默认每个连接 60 个市场
	}

	return &WSPool{
		markets:        markets,
		connections:    make([]*WSPoolConnection, 0),
		marketsPerConn: marketsPerConn,
		done:           make(chan struct{}),
	}
}

// SetPriceHandler 设置价格处理器
func (p *WSPool) SetPriceHandler(handler func(*common.Price)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.priceHandler = handler
}

// Start 启动连接池
func (p *WSPool) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// 计算需要的连接数
	numConnections := (len(p.markets) + p.marketsPerConn - 1) / p.marketsPerConn
	log.Printf("[Lighter Pool] Starting %d WebSocket connections for %d markets (%d markets/conn)",
		numConnections, len(p.markets), p.marketsPerConn)

	// 创建连接
	for i := 0; i < numConnections; i++ {
		startIdx := i * p.marketsPerConn
		endIdx := startIdx + p.marketsPerConn
		if endIdx > len(p.markets) {
			endIdx = len(p.markets)
		}

		markets := p.markets[startIdx:endIdx]
		conn := NewWSPoolConnection(i, markets)
		conn.SetPriceHandler(p.priceHandler)

		if err := conn.Connect(); err != nil {
			log.Printf("[Lighter Pool] Failed to start connection #%d: %v", i, err)
			continue
		}

		p.connections = append(p.connections, conn)
	}

	log.Printf("[Lighter Pool] Successfully started %d/%d connections", len(p.connections), numConnections)
	return nil
}

// Close 关闭所有连接
func (p *WSPool) Close() error {
	close(p.done)

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, conn := range p.connections {
		conn.Close()
	}
	return nil
}

// NewWSPoolConnection 创建单个 WebSocket 连接
func NewWSPoolConnection(id int, markets []*Market) *WSPoolConnection {
	return &WSPoolConnection{
		ID:              id,
		URL:             "wss://mainnet.zklighter.elliot.ai/stream",
		Markets:         markets,
		orderBookData:   make(map[int]*OrderBookData),
		marketStatsData: make(map[int]*MarketStatsData),
		reconnect:       true,
		done:            make(chan struct{}),
	}
}

// SetPriceHandler 设置处理器
func (c *WSPoolConnection) SetPriceHandler(handler func(*common.Price)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.priceHandler = handler
}

// Connect 连接到 WebSocket
func (c *WSPoolConnection) Connect() error {
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

	log.Printf("[Lighter Pool #%d] Connected, subscribing to %d markets", c.ID, len(c.Markets))

	// 设置 Pong 处理器
	conn.SetPongHandler(func(appData string) error {
		c.mu.Lock()
		c.lastPongTime = time.Now()
		c.mu.Unlock()
		return nil
	})

	// 订阅市场
	if err := c.subscribe(); err != nil {
		return fmt.Errorf("failed to subscribe: %w", err)
	}

	// 启动消息读取
	go c.readMessages()

	// 启动心跳检查
	go c.keepAlive()

	return nil
}

// subscribe 订阅市场
func (c *WSPoolConnection) subscribe() error {
	c.mu.RLock()
	markets := c.Markets
	conn := c.Conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("connection not established")
	}

	// 订阅每个市场的 order_book 和 market_stats
	for _, market := range markets {
		// 订阅 order book: order_book/{market_id}
		orderBookSub := SubscribeMessage{
			Type:    "subscribe",
			Channel: fmt.Sprintf("order_book/%d", market.MarketID),
		}
		if err := conn.WriteJSON(orderBookSub); err != nil {
			log.Printf("[Lighter Pool #%d] Failed to subscribe to order_book/%d: %v", c.ID, market.MarketID, err)
			continue
		}

		// 订阅 market stats: market_stats/{market_id}
		marketStatsSub := SubscribeMessage{
			Type:    "subscribe",
			Channel: fmt.Sprintf("market_stats/%d", market.MarketID),
		}
		if err := conn.WriteJSON(marketStatsSub); err != nil {
			log.Printf("[Lighter Pool #%d] Failed to subscribe to market_stats/%d: %v", c.ID, market.MarketID, err)
			continue
		}
	}

	log.Printf("[Lighter Pool #%d] Subscribed to %d markets (order_book + market_stats)", c.ID, len(markets))
	return nil
}

// readMessages 读取消息
func (c *WSPoolConnection) readMessages() {
	messageCount := 0

	defer func() {
		log.Printf("[Lighter Pool #%d] readMessages exited (received %d messages)", c.ID, messageCount)

		c.mu.Lock()
		if c.Conn != nil {
			c.Conn.Close()
		}
		c.mu.Unlock()

		// 重连
		if c.reconnect {
			log.Printf("[Lighter Pool #%d] Reconnecting in 5 seconds...", c.ID)
			time.Sleep(5 * time.Second)
			if err := c.Connect(); err != nil {
				log.Printf("[Lighter Pool #%d] Failed to reconnect: %v", c.ID, err)
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
					log.Printf("[Lighter Pool #%d] Connection closed unexpectedly: %v", c.ID, err)
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
func (c *WSPoolConnection) processMessage(message []byte) {
	// 解析基础消息类型
	var baseMsg struct {
		Type    string `json:"type"`
		Channel string `json:"channel"`
	}

	if err := json.Unmarshal(message, &baseMsg); err != nil {
		return
	}

	switch baseMsg.Type {
	case "update/order_book":
		var update OrderBookUpdate
		if err := json.Unmarshal(message, &update); err != nil {
			log.Printf("[Lighter Pool #%d] Failed to unmarshal order_book: %v", c.ID, err)
			return
		}
		c.handleOrderBookUpdate(&update)

	case "update/market_stats":
		var update MarketStatsUpdate
		if err := json.Unmarshal(message, &update); err != nil {
			log.Printf("[Lighter Pool #%d] Failed to unmarshal market_stats: %v", c.ID, err)
			return
		}
		c.handleMarketStatsUpdate(&update)

	case "subscribed/order_book":
		// 订阅确认消息
		c.handleSubscriptionConfirm(baseMsg.Channel, "order_book")

	case "subscribed/market_stats":
		// 订阅确认消息
		c.handleSubscriptionConfirm(baseMsg.Channel, "market_stats")
	}
}

// handleOrderBookUpdate 处理订单簿更新
func (c *WSPoolConnection) handleOrderBookUpdate(update *OrderBookUpdate) {
	var marketID int

	// 从 channel 解析 market_id: order_book:{MARKET_INDEX} 或 order_book/{MARKET_INDEX}
	n, err := fmt.Sscanf(update.Channel, "order_book:%d", &marketID)
	if err != nil || n != 1 {
		n, err = fmt.Sscanf(update.Channel, "order_book/%d", &marketID)
		if err != nil || n != 1 {
			// 尝试从 order_book 数据中获取
			if update.OrderBook.MarketID > 0 {
				marketID = update.OrderBook.MarketID
			} else {
				log.Printf("[Lighter Pool #%d] Failed to parse market ID from channel '%s'", c.ID, update.Channel)
				return
			}
		}
	}

	c.mu.Lock()
	c.orderBookData[marketID] = &update.OrderBook
	c.mu.Unlock()

	// 合并数据并发送
	c.sendCombinedPrice(marketID)
}

// handleMarketStatsUpdate 处理市场统计更新
func (c *WSPoolConnection) handleMarketStatsUpdate(update *MarketStatsUpdate) {
	marketID := update.MarketStats.MarketID

	c.mu.Lock()
	c.marketStatsData[marketID] = &update.MarketStats
	c.mu.Unlock()

	// 合并数据并发送
	c.sendCombinedPrice(marketID)
}

// handleSubscriptionConfirm 处理订阅确认消息
func (c *WSPoolConnection) handleSubscriptionConfirm(channel string, streamType string) {
	// 从 channel 解析 market_id
	var marketID int
	n, err := fmt.Sscanf(channel, streamType+":%d", &marketID)
	if err != nil || n != 1 {
		n, err = fmt.Sscanf(channel, streamType+"/%d", &marketID)
		if err != nil || n != 1 {
			// 无法解析 market_id，记录日志
			log.Printf("[Lighter Pool #%d] Subscription confirmed: %s", c.ID, channel)
			return
		}
	}

	// 查找市场名称
	c.mu.RLock()
	var marketName string
	for _, m := range c.Markets {
		if m.MarketID == marketID {
			marketName = m.Symbol
			break
		}
	}
	c.mu.RUnlock()

	if marketName != "" {
		log.Printf("[Lighter Pool #%d] ✓ Subscribed to %s for %s (ID: %d)", c.ID, streamType, marketName, marketID)
	} else {
		log.Printf("[Lighter Pool #%d] ✓ Subscribed to %s/%d", c.ID, streamType, marketID)
	}
}

// sendCombinedPrice 合并 order book 和 market stats 数据，发送给处理器
func (c *WSPoolConnection) sendCombinedPrice(marketID int) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.priceHandler == nil {
		return
	}

	// 查找市场信息
	var market *Market
	for _, m := range c.Markets {
		if m.MarketID == marketID {
			market = m
			break
		}
	}
	if market == nil {
		return
	}

	// 获取 order book 和 market stats
	orderBook, hasOrderBook := c.orderBookData[marketID]
	marketStats, hasMarketStats := c.marketStatsData[marketID]

	// 需要至少有某种价格数据
	hasBothSides := hasOrderBook && len(orderBook.Bids) > 0 && len(orderBook.Asks) > 0
	hasMarkPrice := hasMarketStats && marketStats.MarkPrice != "" && marketStats.MarkPrice != "0"
	hasPartialOrderBook := hasOrderBook && (len(orderBook.Bids) > 0 || len(orderBook.Asks) > 0)

	if !hasBothSides && !hasMarkPrice && !hasPartialOrderBook {
		return
	}

	// 使用 mark_price 作为基准价格
	var markPrice float64
	var bidPrice, askPrice, bidQty, askQty float64

	if hasMarketStats {
		markPrice = parseFloat(marketStats.MarkPrice)
	}

	// 如果没有mark price但有完整order book，使用order book中间价
	if markPrice == 0 && hasBothSides {
		bidPriceOB, _, hasBid := c.getBestBid(orderBook.Bids)
		askPriceOB, _, hasAsk := c.getBestAsk(orderBook.Asks)
		if hasBid && hasAsk {
			markPrice = (bidPriceOB + askPriceOB) / 2
		}
	}

	// 如果有完整的order book，使用实际的bid/ask（过滤低流动性订单）
	if hasBothSides {
		var hasBid, hasAsk bool
		bidPrice, bidQty, hasBid = c.getBestBid(orderBook.Bids)
		askPrice, askQty, hasAsk = c.getBestAsk(orderBook.Asks)

		if hasBid && hasAsk {
			if markPrice == 0 {
				markPrice = (bidPrice + askPrice) / 2
			}
		} else {
			// 没有足够流动性的订单，降级为部分订单簿处理
			hasBothSides = false
			hasPartialOrderBook = hasBid || hasAsk
		}
	}

	if !hasBothSides && hasPartialOrderBook {
		// 只有部分order book数据
		if len(orderBook.Bids) > 0 {
			var hasBid bool
			bidPrice, bidQty, hasBid = c.getBestBid(orderBook.Bids)
			if hasBid {
				askPrice = bidPrice * 1.0002
				askQty = 0
				if markPrice == 0 {
					markPrice = bidPrice * 1.0001
				}
			} else {
				// 没有有效的 bid
				return
			}
		} else if len(orderBook.Asks) > 0 {
			var hasAsk bool
			askPrice, askQty, hasAsk = c.getBestAsk(orderBook.Asks)
			if hasAsk {
				bidPrice = askPrice * 0.9998
				bidQty = 0
				if markPrice == 0 {
					markPrice = askPrice * 0.9999
				}
			} else {
				// 没有有效的 ask
				return
			}
		}
	} else if !hasBothSides && !hasPartialOrderBook {
		// 只有mark price
		spread := markPrice * 0.0001
		bidPrice = markPrice - spread
		askPrice = markPrice + spread
		bidQty = 0
		askQty = 0
	}

	// 解析交易量
	var volume24h float64
	if hasMarketStats {
		volume24h = marketStats.DailyQuoteTokenVolume
	}

	// 确定市场类型
	marketType := common.MarketTypeSpot
	if market.Type == "perp" {
		marketType = common.MarketTypeFuture
	}

	// 获取时间戳
	var timestamp time.Time
	if hasOrderBook && orderBook.Timestamp > 0 {
		timestamp = time.UnixMilli(orderBook.Timestamp)
	} else {
		timestamp = time.Now()
	}

	// 创建 Price 对象
	price := &common.Price{
		Symbol:      market.Symbol,
		Exchange:    common.ExchangeLighter,
		MarketType:  marketType,
		Price:       (bidPrice + askPrice) / 2,
		BidPrice:    bidPrice,
		AskPrice:    askPrice,
		BidQty:      bidQty,
		AskQty:      askQty,
		Volume24h:   volume24h,
		Timestamp:   timestamp,
		LastUpdated: time.Now(),
		Source:      common.PriceSourceWebSocket,
	}

	c.priceHandler(price)
}

// keepAlive 心跳检查
func (c *WSPoolConnection) keepAlive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.RLock()
			conn := c.Conn
			lastPong := c.lastPongTime
			c.mu.RUnlock()

			if conn != nil {
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("[Lighter Pool #%d] Failed to send ping: %v", c.ID, err)
					return
				}
			}

			if time.Since(lastPong) > 90*time.Second {
				log.Printf("[Lighter Pool #%d] No PONG for %.0fs, connection may be dead", c.ID, time.Since(lastPong).Seconds())
			}
		}
	}
}

// Close 关闭连接
func (c *WSPoolConnection) Close() {
	c.reconnect = false
	close(c.done)

	c.mu.Lock()
	if c.Conn != nil {
		c.Conn.Close()
		c.Conn = nil
	}
	c.mu.Unlock()
}

// getBestBid 获取最优买单价格（过滤低流动性订单，选择价格最高的）
// 返回：价格，数量，是否找到有效订单
func (c *WSPoolConnection) getBestBid(bids []PriceLevel) (float64, float64, bool) {
	const minNotional = 5.0 // 最小名义价值 5 USDT

	var bestPrice float64
	var bestQty float64
	found := false

	for _, bid := range bids {
		price := parseFloat(bid.Price)
		size := parseFloat(bid.Size)

		if price == 0 || size == 0 {
			continue
		}

		// 计算名义价值 = price * size
		notional := price * size

		// 过滤掉名义价值小于 5 USDT 的订单
		if notional < minNotional {
			continue
		}

		// 对于买单（bid），选择价格最高的
		if !found || price > bestPrice {
			bestPrice = price
			bestQty = size
			found = true
		}
	}

	return bestPrice, bestQty, found
}

// getBestAsk 获取最优卖单价格（过滤低流动性订单，选择价格最低的）
// 返回：价格，数量，是否找到有效订单
func (c *WSPoolConnection) getBestAsk(asks []PriceLevel) (float64, float64, bool) {
	const minNotional = 5.0 // 最小名义价值 5 USDT

	var bestPrice float64
	var bestQty float64
	found := false

	for _, ask := range asks {
		price := parseFloat(ask.Price)
		size := parseFloat(ask.Size)

		if price == 0 || size == 0 {
			continue
		}

		// 计算名义价值 = price * size
		notional := price * size

		// 过滤掉名义价值小于 5 USDT 的订单
		if notional < minNotional {
			continue
		}

		// 对于卖单（ask），选择价格最低的
		if !found || price < bestPrice {
			bestPrice = price
			bestQty = size
			found = true
		}
	}

	return bestPrice, bestQty, found
}
