package lighter

import (
	"log"
	"sort"
	"sync"
	"time"
)

// LocalOrderBook 本地维护的订单簿（支持增量更新）
type LocalOrderBook struct {
	MarketID        int
	Symbol          string
	Bids            map[float64]*Order // price -> order
	Asks            map[float64]*Order // price -> order
	lastNonce       int64              // 最后一次更新的 nonce
	lastOffset      int64              // 最后一次更新的 offset
	updateCount     int64              // 更新计数器（用于定期同步）
	initialized     bool               // 是否已从快照初始化
	lastSyncTime    int64              // 最后一次全量同步时间戳
	mu              sync.RWMutex
}

// NewLocalOrderBook 创建本地订单簿
func NewLocalOrderBook(marketID int, symbol string) *LocalOrderBook {
	return &LocalOrderBook{
		MarketID: marketID,
		Symbol:   symbol,
		Bids:     make(map[float64]*Order),
		Asks:     make(map[float64]*Order),
	}
}

// InitializeFromSnapshot 从快照初始化订单簿
func (ob *LocalOrderBook) InitializeFromSnapshot(bids, asks []PriceLevel, nonce, offset int64) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	// 清空现有数据
	ob.Bids = make(map[float64]*Order)
	ob.Asks = make(map[float64]*Order)

	// 初始化买单
	for _, bid := range bids {
		price := parseFloat(bid.Price)
		amount := parseFloat(bid.Size)
		if price > 0 && amount > 0 {
			ob.Bids[price] = &Order{
				Price:  price,
				Amount: amount,
			}
		}
	}

	// 初始化卖单
	for _, ask := range asks {
		price := parseFloat(ask.Price)
		amount := parseFloat(ask.Size)
		if price > 0 && amount > 0 {
			ob.Asks[price] = &Order{
				Price:  price,
				Amount: amount,
			}
		}
	}

	// 更新 nonce/offset 状态
	ob.lastNonce = nonce
	ob.lastOffset = offset
	ob.initialized = true
	ob.lastSyncTime = getCurrentTimestamp()
	ob.updateCount = 0

	log.Printf("[OrderBook %s] Initialized with %d bids, %d asks (nonce=%d, offset=%d)",
		ob.Symbol, len(ob.Bids), len(ob.Asks), nonce, offset)
}

// UpdateOrder 更新订单（处理 add/update/remove 事件）
func (ob *LocalOrderBook) UpdateOrder(side, event string, price, amount float64) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	var orderMap map[float64]*Order
	if side == "bid" {
		orderMap = ob.Bids
	} else if side == "ask" {
		orderMap = ob.Asks
	} else {
		log.Printf("[OrderBook %s] Unknown side: %s", ob.Symbol, side)
		return
	}

	switch event {
	case "add", "update":
		if amount > 0 {
			orderMap[price] = &Order{
				Price:  price,
				Amount: amount,
			}
		} else {
			// amount 为 0，相当于删除
			delete(orderMap, price)
		}

	case "remove":
		delete(orderMap, price)

	default:
		log.Printf("[OrderBook %s] Unknown event: %s", ob.Symbol, event)
	}
}

// ApplyIncrementalUpdate 应用增量更新（带连续性验证）
// 返回 (是否应用成功, 是否需要重新同步)
func (ob *LocalOrderBook) ApplyIncrementalUpdate(bids, asks []PriceLevel, beginNonce, nonce, offset int64) (applied bool, needsResync bool) {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	// 检查是否已初始化
	if !ob.initialized {
		log.Printf("[OrderBook %s] ⚠️  Cannot apply incremental update: not initialized", ob.Symbol)
		return false, true
	}

	// 连续性验证：begin_nonce 必须匹配上一次的 nonce
	if beginNonce != 0 && ob.lastNonce != 0 && beginNonce != ob.lastNonce {
		log.Printf("[OrderBook %s] ⚠️  Nonce mismatch: expected %d, got begin_nonce=%d (offset=%d). Need resync!",
			ob.Symbol, ob.lastNonce, beginNonce, offset)
		return false, true
	}

	// Offset 跳变检测（仅警告，因为 offset 可能在重连时重置）
	if ob.lastOffset != 0 && offset != 0 {
		offsetDiff := offset - ob.lastOffset
		if offsetDiff > 100 {
			log.Printf("[OrderBook %s] ⚠️  Large offset jump: %d -> %d (diff=%d). Possible reconnection.",
				ob.Symbol, ob.lastOffset, offset, offsetDiff)
		} else if offsetDiff < 0 {
			log.Printf("[OrderBook %s] ⚠️  Offset decreased: %d -> %d. Server reconnected, offset reset.",
				ob.Symbol, ob.lastOffset, offset)
		}
	}

	// 应用买单更新
	for _, bid := range bids {
		price := parseFloat(bid.Price)
		amount := parseFloat(bid.Size)

		if price <= 0 {
			continue
		}

		if amount > 0 {
			// 新增或更新
			ob.Bids[price] = &Order{
				Price:  price,
				Amount: amount,
			}
		} else {
			// 删除（amount = 0）
			delete(ob.Bids, price)
		}
	}

	// 应用卖单更新
	for _, ask := range asks {
		price := parseFloat(ask.Price)
		amount := parseFloat(ask.Size)

		if price <= 0 {
			continue
		}

		if amount > 0 {
			// 新增或更新
			ob.Asks[price] = &Order{
				Price:  price,
				Amount: amount,
			}
		} else {
			// 删除（amount = 0）
			delete(ob.Asks, price)
		}
	}

	// 更新状态
	ob.lastNonce = nonce
	ob.lastOffset = offset
	ob.updateCount++

	return true, false
}

// NeedsPeriodicSync 检查是否需要定期全量同步
// 条件：每 1000 次更新 或 每 10 秒
func (ob *LocalOrderBook) NeedsPeriodicSync() bool {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	if !ob.initialized {
		return false
	}

	// 每 1000 次更新触发同步
	if ob.updateCount >= 1000 {
		return true
	}

	// 每 10 秒触发同步
	currentTime := getCurrentTimestamp()
	if currentTime-ob.lastSyncTime >= 10000 { // 10 秒 = 10000 毫秒
		return true
	}

	return false
}

// ResetSyncCounter 重置同步计数器（在完成全量同步后调用）
func (ob *LocalOrderBook) ResetSyncCounter() {
	ob.mu.Lock()
	defer ob.mu.Unlock()

	ob.updateCount = 0
	ob.lastSyncTime = getCurrentTimestamp()
}

// GetBestBid 获取最优买单（价格最高的，且过滤低流动性）
func (ob *LocalOrderBook) GetBestBid(minNotional float64) (float64, float64, bool) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	if len(ob.Bids) == 0 {
		return 0, 0, false
	}

	// 收集所有价格并排序（降序）
	prices := make([]float64, 0, len(ob.Bids))
	for price := range ob.Bids {
		prices = append(prices, price)
	}
	sort.Float64s(prices)

	// 从高到低遍历，找到第一个满足流动性要求的订单
	for i := len(prices) - 1; i >= 0; i-- {
		price := prices[i]
		order := ob.Bids[price]

		notional := price * order.Amount
		if notional >= minNotional {
			return order.Price, order.Amount, true
		}
	}

	return 0, 0, false
}

// GetBestAsk 获取最优卖单（价格最低的，且过滤低流动性）
func (ob *LocalOrderBook) GetBestAsk(minNotional float64) (float64, float64, bool) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()

	if len(ob.Asks) == 0 {
		return 0, 0, false
	}

	// 收集所有价格并排序（升序）
	prices := make([]float64, 0, len(ob.Asks))
	for price := range ob.Asks {
		prices = append(prices, price)
	}
	sort.Float64s(prices)

	// 从低到高遍历，找到第一个满足流动性要求的订单
	for _, price := range prices {
		order := ob.Asks[price]

		notional := price * order.Amount
		if notional >= minNotional {
			return order.Price, order.Amount, true
		}
	}

	return 0, 0, false
}

// GetStats 获取订单簿统计信息
func (ob *LocalOrderBook) GetStats() (bidCount, askCount int) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return len(ob.Bids), len(ob.Asks)
}

// getCurrentTimestamp 获取当前时间戳（毫秒）
func getCurrentTimestamp() int64 {
	return time.Now().UnixMilli()
}

// IsInitialized 检查订单簿是否已初始化
func (ob *LocalOrderBook) IsInitialized() bool {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	return ob.initialized
}
