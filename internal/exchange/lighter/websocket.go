package lighter

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSClient Lighter WebSocket 客户端
type WSClient struct {
	URL             string
	Conn            *websocket.Conn
	markets         map[int]*Market // marketID -> Market
	orderBookData   map[int]*OrderBookData
	marketStatsData map[int]*MarketStatsData
	mu              sync.RWMutex
	messageHandler  func(*common.Price)
	reconnect       bool
	done            chan struct{}
	apiURL          string        // API URL for market updates
	refreshInterval time.Duration // 市场刷新间隔
}

// NewWSClient 创建新的 WebSocket 客户端
func NewWSClient(url string, markets []*Market, apiURL string, refreshInterval int) *WSClient {
	marketMap := make(map[int]*Market)
	for _, m := range markets {
		marketMap[m.MarketID] = m
	}

	client := &WSClient{
		URL:             url,
		markets:         marketMap,
		orderBookData:   make(map[int]*OrderBookData),
		marketStatsData: make(map[int]*MarketStatsData),
		reconnect:       true,
		done:            make(chan struct{}),
		apiURL:          apiURL,
	}

	// 设置刷新间隔（存储在结构体中）
	client.refreshInterval = time.Duration(refreshInterval) * time.Minute

	return client
}

// Connect 连接到 WebSocket
func (c *WSClient) Connect() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.URL, nil)
	if err != nil {
		return fmt.Errorf("failed to connect to %s: %v", c.URL, err)
	}

	c.Conn = conn
	log.Printf("WebSocket connected to %s", c.URL)

	// 启动读取协程
	go c.readMessages()

	// 启动心跳保活
	go c.keepAlive()

	// 启动市场刷新协程（仅当设置了刷新间隔时）
	if c.apiURL != "" && c.refreshInterval > 0 {
		log.Printf("Market auto-refresh enabled (interval: %v)", c.refreshInterval)
		go c.refreshMarkets()
	}

	return nil
}

// SetMessageHandler 设置消息处理器
func (c *WSClient) SetMessageHandler(handler func(*common.Price)) {
	c.messageHandler = handler
}

// Subscribe 订阅市场数据
func (c *WSClient) Subscribe(marketIDs []int) error {
	if c.Conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	// 订阅每个市场的 order_book 和 market_stats
	for _, marketID := range marketIDs {
		// 订阅 order book
		orderBookSub := SubscribeMessage{
			Type:    "subscribe",
			Channel: fmt.Sprintf("order_book/%d", marketID),
		}
		if err := c.Conn.WriteJSON(orderBookSub); err != nil {
			return fmt.Errorf("failed to subscribe to order_book/%d: %v", marketID, err)
		}

		// 订阅 market stats
		marketStatsSub := SubscribeMessage{
			Type:    "subscribe",
			Channel: fmt.Sprintf("market_stats/%d", marketID),
		}
		if err := c.Conn.WriteJSON(marketStatsSub); err != nil {
			return fmt.Errorf("failed to subscribe to market_stats/%d: %v", marketID, err)
		}
	}

	log.Printf("Subscribed to %d markets (order_book + market_stats)", len(marketIDs))
	return nil
}

// SubscribeAll 订阅所有市场（使用 order_book/all 和 market_stats/all）
func (c *WSClient) SubscribeAll() error {
	if c.Conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	// 订阅所有市场的 order book
	orderBookSub := SubscribeMessage{
		Type:    "subscribe",
		Channel: "order_book/all",
	}
	if err := c.Conn.WriteJSON(orderBookSub); err != nil {
		return fmt.Errorf("failed to subscribe to order_book/all: %v", err)
	}

	// 订阅所有市场的 market stats
	marketStatsSub := SubscribeMessage{
		Type:    "subscribe",
		Channel: "market_stats/all",
	}
	if err := c.Conn.WriteJSON(marketStatsSub); err != nil {
		return fmt.Errorf("failed to subscribe to market_stats/all: %v", err)
	}

	log.Printf("Subscribed to order_book/all and market_stats/all")
	return nil
}

// readMessages 读取 WebSocket 消息
func (c *WSClient) readMessages() {
	defer func() {
		if c.reconnect {
			log.Println("Reconnecting WebSocket in 5 seconds...")
			time.Sleep(5 * time.Second)
			if err := c.Connect(); err != nil {
				log.Printf("Failed to reconnect: %v", err)
			} else {
				// 重新订阅
				marketIDs := make([]int, 0, len(c.markets))
				for id := range c.markets {
					marketIDs = append(marketIDs, id)
				}
				c.Subscribe(marketIDs)
			}
		}
	}()

	for {
		select {
		case <-c.done:
			return
		default:
			_, message, err := c.Conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket error: %v", err)
				}
				return
			}

			c.processMessage(message)
		}
	}
}

// processMessage 处理接收到的消息
func (c *WSClient) processMessage(message []byte) {
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
			log.Printf("Failed to unmarshal order_book: %v", err)
			return
		}
		c.handleOrderBookUpdate(&update)

	case "update/market_stats":
		var update MarketStatsUpdate
		if err := json.Unmarshal(message, &update); err != nil {
			log.Printf("Failed to unmarshal market_stats: %v", err)
			return
		}
		c.handleMarketStatsUpdate(&update)
	}
}

// handleOrderBookUpdate 处理订单簿更新
func (c *WSClient) handleOrderBookUpdate(update *OrderBookUpdate) {
	var marketID int

	// 1. 优先从 OrderBook.MarketID 字段获取（order_book/all 返回的数据）
	if update.OrderBook.MarketID > 0 {
		marketID = update.OrderBook.MarketID
	} else {
		// 2. 从 channel 解析（order_book:123 格式）
		n, err := fmt.Sscanf(update.Channel, "order_book:%d", &marketID)
		if err != nil || n != 1 {
			// 尝试其他格式 order_book/123
			n, err = fmt.Sscanf(update.Channel, "order_book/%d", &marketID)
			if err != nil || n != 1 {
				log.Printf("Failed to parse market ID from channel '%s' and no market_id in data", update.Channel)
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
func (c *WSClient) handleMarketStatsUpdate(update *MarketStatsUpdate) {
	marketID := update.MarketStats.MarketID

	c.mu.Lock()
	c.marketStatsData[marketID] = &update.MarketStats
	c.mu.Unlock()

	// 合并数据并发送
	c.sendCombinedPrice(marketID)
}

// sendCombinedPrice 合并 order book 和 market stats 数据，发送给处理器
func (c *WSClient) sendCombinedPrice(marketID int) {
	if c.messageHandler == nil {
		return
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// 获取市场信息
	market, ok := c.markets[marketID]
	if !ok {
		return
	}

	// 获取 order book 和 market stats
	orderBook, hasOrderBook := c.orderBookData[marketID]
	marketStats, hasMarketStats := c.marketStatsData[marketID]

	// 需要至少有某种价格数据：完整order book, mark_price, 或部分order book
	hasBothSides := hasOrderBook && len(orderBook.Bids) > 0 && len(orderBook.Asks) > 0
	hasMarkPrice := hasMarketStats && marketStats.MarkPrice != "" && marketStats.MarkPrice != "0"
	hasPartialOrderBook := hasOrderBook && (len(orderBook.Bids) > 0 || len(orderBook.Asks) > 0)

	if !hasBothSides && !hasMarkPrice && !hasPartialOrderBook {
		return
	}

	// 使用 mark_price 作为基准价格，而不是 order book 价格
	var markPrice float64
	var bidPrice, askPrice, bidQty, askQty float64

	if hasMarketStats {
		markPrice = parseFloat(marketStats.MarkPrice)
	}

	// 如果没有mark price但有完整order book，使用order book中间价
	if markPrice == 0 && hasBothSides {
		bidPriceOB := parseFloat(orderBook.Bids[0].Price)
		askPriceOB := parseFloat(orderBook.Asks[0].Price)
		markPrice = (bidPriceOB + askPriceOB) / 2
	}

	// 如果有完整的order book，使用实际的bid/ask
	if hasBothSides {
		bidPrice = parseFloat(orderBook.Bids[0].Price)
		askPrice = parseFloat(orderBook.Asks[0].Price)
		bidQty = parseFloat(orderBook.Bids[0].Size)
		askQty = parseFloat(orderBook.Asks[0].Size)
		if markPrice == 0 {
			markPrice = (bidPrice + askPrice) / 2
		}
	} else if hasPartialOrderBook {
		// 只有部分order book数据
		if len(orderBook.Bids) > 0 {
			bidPrice = parseFloat(orderBook.Bids[0].Price)
			bidQty = parseFloat(orderBook.Bids[0].Size)
			// 使用bid价格估算ask价格（假设0.02%的价差）
			askPrice = bidPrice * 1.0002
			askQty = 0
			if markPrice == 0 {
				markPrice = bidPrice * 1.0001 // 中间价
			}
		} else {
			// 只有asks
			askPrice = parseFloat(orderBook.Asks[0].Price)
			askQty = parseFloat(orderBook.Asks[0].Size)
			// 使用ask价格估算bid价格
			bidPrice = askPrice * 0.9998
			bidQty = 0
			if markPrice == 0 {
				markPrice = askPrice * 0.9999 // 中间价
			}
		}
	} else {
		// 只有mark price，计算买卖价差（基于mark price的小幅偏移）
		spread := markPrice * 0.0001 // 假设0.01%的价差
		bidPrice = markPrice - spread
		askPrice = markPrice + spread
		bidQty = 0
		askQty = 0
	}

	// 解析交易量（如果有 market stats）
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
	}

	c.messageHandler(price)
}

// keepAlive 保持连接活跃
func (c *WSClient) keepAlive() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			if c.Conn != nil {
				if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					log.Printf("Failed to send ping: %v", err)
					return
				}
			}
		}
	}
}

// Close 关闭连接
func (c *WSClient) Close() error {
	c.reconnect = false
	close(c.done)

	if c.Conn != nil {
		return c.Conn.Close()
	}
	return nil
}

// parseFloat 解析字符串为 float64
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// refreshMarkets 定期刷新市场列表
func (c *WSClient) refreshMarkets() {
	ticker := time.NewTicker(c.refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.updateMarkets()
		}
	}
}

// updateMarkets 更新市场列表
func (c *WSClient) updateMarkets() {
	log.Println("Refreshing Lighter markets from API...")
	
	newMarkets, err := FetchMarketsFromAPI(c.apiURL)
	if err != nil {
		log.Printf("Failed to refresh markets: %v", err)
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// 检测新增市场
	newlyAdded := make([]*Market, 0)
	for _, market := range newMarkets {
		if _, exists := c.markets[market.MarketID]; !exists {
			newlyAdded = append(newlyAdded, market)
			c.markets[market.MarketID] = market
		}
	}

	// 如果有新市场，订阅它们
	if len(newlyAdded) > 0 && c.Conn != nil {
		log.Printf("Detected %d new markets on Lighter", len(newlyAdded))
		for _, market := range newlyAdded {
			// 订阅 order book
			orderBookSub := SubscribeMessage{
				Type:    "subscribe",
				Channel: fmt.Sprintf("order_book/%d", market.MarketID),
			}
			if err := c.Conn.WriteJSON(orderBookSub); err != nil {
				log.Printf("Failed to subscribe to order_book/%d: %v", market.MarketID, err)
			}

			// 订阅 market stats
			marketStatsSub := SubscribeMessage{
				Type:    "subscribe",
				Channel: fmt.Sprintf("market_stats/%d", market.MarketID),
			}
			if err := c.Conn.WriteJSON(marketStatsSub); err != nil {
				log.Printf("Failed to subscribe to market_stats/%d: %v", market.MarketID, err)
			}

			log.Printf("🆕 New market detected and subscribed: %s (Market ID: %d)", market.Symbol, market.MarketID)
		}
	}

	log.Printf("Market refresh completed. Now monitoring %d markets", len(c.markets))
}
