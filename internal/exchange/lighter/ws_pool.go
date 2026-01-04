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
	markets        []*Market           // 所有需要订阅的市场
	connections    []*WSPoolConnection // WebSocket 连接池
	priceHandler   func(*common.Price) // 价格处理器
	marketsPerConn int                 // 每个连接订阅的市场数量
	mu             sync.RWMutex
	done           chan struct{}
}

// WSPoolConnection 单个 WebSocket 连接
type WSPoolConnection struct {
	ID              int
	URL             string
	Conn            *websocket.Conn
	Markets         []*Market
	orderBookData   map[int]*OrderBookData // 快照数据（兼容旧逻辑）
	marketStatsData map[int]*MarketStatsData
	localOrderBooks map[int]*LocalOrderBook // 本地维护的订单簿（增量更新）
	mu              sync.RWMutex
	reconnect       bool
	done            chan struct{}
	connectedAt     time.Time
	lastPongTime    time.Time
	priceHandler    func(*common.Price)
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
	// 初始化本地订单簿
	localOrderBooks := make(map[int]*LocalOrderBook)
	for _, market := range markets {
		localOrderBooks[market.MarketID] = NewLocalOrderBook(market.MarketID, market.Symbol)
	}

	return &WSPoolConnection{
		ID:              id,
		URL:             "wss://mainnet.zklighter.elliot.ai/stream",
		Markets:         markets,
		orderBookData:   make(map[int]*OrderBookData),
		marketStatsData: make(map[int]*MarketStatsData),
		localOrderBooks: localOrderBooks,
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

	//log.Printf("[Lighter Pool #%d] Connected, subscribing to %d markets", c.ID, len(c.Markets))

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
	case "subscribed/order_book":
		// 订阅时返回的快照数据 - 用于初始化本地订单簿
		var snapshot OrderBookUpdate
		if err := json.Unmarshal(message, &snapshot); err != nil {
			log.Printf("[Lighter Pool #%d] Failed to unmarshal order_book snapshot: %v", c.ID, err)
			return
		}
		c.handleOrderBookSnapshot(&snapshot)

	case "update/order_book":
		// 增量更新 - 应用到本地订单簿
		var update OrderBookUpdate
		if err := json.Unmarshal(message, &update); err != nil {
			log.Printf("[Lighter Pool #%d] Failed to unmarshal order_book update: %v", c.ID, err)
			return
		}
		c.handleOrderBookIncrementalUpdate(&update)

	case "subscribed/market_stats":
		// 订阅确认消息（market stats 快照）
		var statsSnapshot MarketStatsUpdate
		if err := json.Unmarshal(message, &statsSnapshot); err != nil {
			log.Printf("[Lighter Pool #%d] Failed to unmarshal market_stats snapshot: %v", c.ID, err)
			return
		}
		c.handleMarketStatsUpdate(&statsSnapshot)

	case "update/market_stats":
		// Market stats 增量更新
		var update MarketStatsUpdate
		if err := json.Unmarshal(message, &update); err != nil {
			log.Printf("[Lighter Pool #%d] Failed to unmarshal market_stats update: %v", c.ID, err)
			return
		}
		c.handleMarketStatsUpdate(&update)
	}
}

// handleOrderBookSnapshot 处理订单簿快照（subscribed/order_book）
func (c *WSPoolConnection) handleOrderBookSnapshot(snapshot *OrderBookUpdate) {
	var marketID int

	// 从 channel 解析 market_id: order_book:{MARKET_INDEX} 或 order_book/{MARKET_INDEX}
	n, err := fmt.Sscanf(snapshot.Channel, "order_book:%d", &marketID)
	if err != nil || n != 1 {
		n, err = fmt.Sscanf(snapshot.Channel, "order_book/%d", &marketID)
		if err != nil || n != 1 {
			// 尝试从 order_book 数据中获取
			if snapshot.OrderBook.MarketID > 0 {
				marketID = snapshot.OrderBook.MarketID
			} else {
				log.Printf("[Lighter Pool #%d] Failed to parse market ID from channel '%s'", c.ID, snapshot.Channel)
				return
			}
		}
	}

	c.mu.Lock()
	c.orderBookData[marketID] = &snapshot.OrderBook

	// 从快照初始化本地订单簿
	if localOB, exists := c.localOrderBooks[marketID]; exists {
		localOB.InitializeFromSnapshot(
			snapshot.OrderBook.Bids,
			snapshot.OrderBook.Asks,
			snapshot.OrderBook.Nonce,
			snapshot.Offset,
		)
	}
	c.mu.Unlock()

	// 合并数据并发送
	c.sendCombinedPrice(marketID)
}

// handleOrderBookIncrementalUpdate 处理增量订单簿更新（update/order_book）
func (c *WSPoolConnection) handleOrderBookIncrementalUpdate(update *OrderBookUpdate) {
	var marketID int

	// 从 channel 解析 market_id
	n, err := fmt.Sscanf(update.Channel, "order_book:%d", &marketID)
	if err != nil || n != 1 {
		n, err = fmt.Sscanf(update.Channel, "order_book/%d", &marketID)
		if err != nil || n != 1 {
			if update.OrderBook.MarketID > 0 {
				marketID = update.OrderBook.MarketID
			} else {
				log.Printf("[Lighter Pool #%d] Failed to parse market ID from channel '%s'", c.ID, update.Channel)
				return
			}
		}
	}

	c.mu.RLock()
	localOB, exists := c.localOrderBooks[marketID]
	c.mu.RUnlock()

	if !exists {
		log.Printf("[Lighter Pool #%d] Local order book not found for market %d", c.ID, marketID)
		return
	}

	// 应用增量更新（带连续性验证）
	applied, needsResync := localOB.ApplyIncrementalUpdate(
		update.OrderBook.Bids,
		update.OrderBook.Asks,
		update.OrderBook.BeginNonce,
		update.OrderBook.Nonce,
		update.Offset,
	)

	// 如果需要重新同步，触发 REST 快照获取
	if needsResync {
		log.Printf("[Lighter Pool #%d] ⚠️  Triggering REST snapshot resync for market %d", c.ID, marketID)
		go c.resyncOrderBookFromREST(marketID)
		return
	}

	if !applied {
		// 应用失败但不需要重新同步（例如：未初始化）
		return
	}

	// 检查是否需要定期全量同步
	if localOB.NeedsPeriodicSync() {
		//log.Printf("[Lighter Pool #%d] 🔄 Periodic sync triggered for market %d", c.ID, marketID)
		go c.resyncOrderBookFromREST(marketID)
	}

	// 重新计算并发送价格
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

// resyncOrderBookFromREST 从 REST API 重新同步订单簿（用于恢复连续性）
func (c *WSPoolConnection) resyncOrderBookFromREST(marketID int) {
	// TODO: 实现 REST API 快照获取
	// 目前的实现策略：
	// 1. 调用 Lighter REST API 获取完整订单簿快照
	// 2. 使用快照重新初始化本地订单簿
	// 3. 重置同步计数器

	// 临时解决方案：标记本地订单簿为未初始化，等待下次 WS 快照
	c.mu.RLock()
	localOB, exists := c.localOrderBooks[marketID]
	c.mu.RUnlock()

	if exists {
		// 不清空订单簿，但重置同步计数器，避免频繁触发
		localOB.ResetSyncCounter()
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

	// 优先使用本地订单簿（增量更新的准确数据）
	localOB, hasLocalOB := c.localOrderBooks[marketID]
	marketStats, hasMarketStats := c.marketStatsData[marketID]

	var bidPrice, askPrice, bidQty, askQty float64
	var markPrice float64
	hasBothSides := false

	// 1. 优先从本地订单簿获取最优 bid/ask
	if hasLocalOB {
		const minNotional = 5.0
		bidCount, askCount := localOB.GetStats()

		if bidCount > 0 && askCount > 0 {
			var hasBid, hasAsk bool
			bidPrice, bidQty, hasBid = localOB.GetBestBid(minNotional)
			askPrice, askQty, hasAsk = localOB.GetBestAsk(minNotional)

			if hasBid && hasAsk {
				hasBothSides = true
				markPrice = (bidPrice + askPrice) / 2
			}
		}
	}

	// 2. 如果本地订单簿没有数据，回退到快照数据（兼容性）
	if !hasBothSides {
		orderBook, hasOrderBook := c.orderBookData[marketID]
		hasPartialOrderBook := hasOrderBook && (len(orderBook.Bids) > 0 || len(orderBook.Asks) > 0)
		hasMarkPrice := hasMarketStats && marketStats.MarkPrice != "" && marketStats.MarkPrice != "0"

		if !hasPartialOrderBook && !hasMarkPrice {
			return
		}

		hasBothSides = hasOrderBook && len(orderBook.Bids) > 0 && len(orderBook.Asks) > 0

		if hasBothSides {
			var hasBid, hasAsk bool
			bidPrice, bidQty, hasBid = c.getBestBid(orderBook.Bids)
			askPrice, askQty, hasAsk = c.getBestAsk(orderBook.Asks)

			if hasBid && hasAsk {
				markPrice = (bidPrice + askPrice) / 2
			} else {
				hasBothSides = false
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
					markPrice = bidPrice * 1.0001
				} else {
					return
				}
			} else if len(orderBook.Asks) > 0 {
				var hasAsk bool
				askPrice, askQty, hasAsk = c.getBestAsk(orderBook.Asks)
				if hasAsk {
					bidPrice = askPrice * 0.9998
					bidQty = 0
					markPrice = askPrice * 0.9999
				} else {
					return
				}
			}
		} else if !hasBothSides && hasMarkPrice {
			// 只有mark price
			if hasMarketStats {
				markPrice = parseFloat(marketStats.MarkPrice)
			}
			spread := markPrice * 0.0001
			bidPrice = markPrice - spread
			askPrice = markPrice + spread
			bidQty = 0
			askQty = 0
		}
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

	// 获取时间戳（尝试从快照数据获取，否则使用当前时间）
	var timestamp time.Time
	if orderBookData, exists := c.orderBookData[marketID]; exists && orderBookData.Timestamp > 0 {
		timestamp = time.UnixMilli(orderBookData.Timestamp)
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
