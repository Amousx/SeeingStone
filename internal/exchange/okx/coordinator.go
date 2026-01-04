package okx

import (
	"crypto-arbitrage-monitor/internal/pricestore"
	"crypto-arbitrage-monitor/pkg/common"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"
)

// DirectionalTask 单向任务（只获取bid或ask）
type DirectionalTask struct {
	TokenConfig *TokenConfig
	Direction   QuoteDirection
	TaskID      string // 用于关联同一代币的bid/ask任务
}

// MergedPriceResult 合并后的价格结果
type MergedPriceResult struct {
	TokenConfig       *TokenConfig
	Price             *common.Price
	Error             error
	BidLatency        time.Duration // bid请求延迟
	AskLatency        time.Duration // ask请求延迟
	TimeDiff          time.Duration // bid和ask的时间差
	ValidationWarning string        // 验证警告信息
}

// TaskResultCollector 收集单个代币的bid/ask结果
type TaskResultCollector struct {
	mu          sync.Mutex
	taskID      string
	tokenConfig *TokenConfig
	bidResult   *FetchResult
	askResult   *FetchResult
	resultChan  chan *MergedPriceResult
	timeout     *time.Timer
	startTime   time.Time

	// 用channel通知代替轮询（优化CPU使用）
	bidNotify   chan struct{}
	askNotify   chan struct{}
	bidNotified bool // 防止重复关闭channel
	askNotified bool
}

// BidirectionalTaskCoordinator 双向任务协调器
// 负责将代币任务拆分为bid/ask两个子任务，分配给不同Worker，并合并结果
type BidirectionalTaskCoordinator struct {
	workers          []*KeyWorker
	resultCollectors map[string]*TaskResultCollector
	statsManager     *StatsManager
	priceStore       *pricestore.PriceStore
	mu               sync.Mutex

	// 配置项
	enableParallel         bool
	maxSpreadPercent       float64 // 最大价差百分比
	maxPriceChangePercent  float64 // 最大价格变化百分比
	rejectInvalidPrices    bool    // 是否拒绝异常价格
}

// NewBidirectionalTaskCoordinator 创建双向任务协调器
func NewBidirectionalTaskCoordinator(
	workers []*KeyWorker,
	priceStore *pricestore.PriceStore,
	maxSpreadPercent float64,
	maxPriceChangePercent float64,
	rejectInvalidPrices bool,
) *BidirectionalTaskCoordinator {
	if len(workers) == 0 {
		log.Println("[OKX Coordinator] Warning: no workers provided")
		return nil
	}

	return &BidirectionalTaskCoordinator{
		workers:               workers,
		resultCollectors:      make(map[string]*TaskResultCollector),
		statsManager:          NewStatsManager(),
		priceStore:            priceStore,
		enableParallel:        len(workers) >= 2, // 至少2个Worker才启用并行
		maxSpreadPercent:      maxSpreadPercent,
		maxPriceChangePercent: maxPriceChangePercent,
		rejectInvalidPrices:   rejectInvalidPrices,
	}
}

// selectTwoWorkers 选择两个不同的Worker（基于时间的负载均衡）
// 返回两个Worker用于bid和ask任务
// 优先选择最久未分配任务的Worker，避免基于len(chan)的不准确负载判断
func (c *BidirectionalTaskCoordinator) selectTwoWorkers() (*KeyWorker, *KeyWorker) {
	if len(c.workers) < 2 {
		// Worker不足，降级为串行：返回同一个Worker
		log.Printf("[OKX Coordinator] Only %d worker(s), using same worker for bid and ask", len(c.workers))
		return c.workers[0], c.workers[0]
	}

	// 定义Worker时间信息
	type workerTime struct {
		worker       *KeyWorker
		lastAssigned time.Time
	}

	// 收集每个Worker的最后分配时间
	workers := make([]workerTime, len(c.workers))
	for i, w := range c.workers {
		w.assignMu.Lock()
		workers[i] = workerTime{
			worker:       w,
			lastAssigned: w.lastAssignedTime,
		}
		w.assignMu.Unlock()
	}

	// 按时间排序（升序，最久的在前）
	sort.Slice(workers, func(i, j int) bool {
		return workers[i].lastAssigned.Before(workers[j].lastAssigned)
	})

	// 选择最久的两个并更新分配时间
	now := time.Now()

	workers[0].worker.assignMu.Lock()
	workers[0].worker.lastAssignedTime = now
	workers[0].worker.assignMu.Unlock()

	workers[1].worker.assignMu.Lock()
	workers[1].worker.lastAssignedTime = now
	workers[1].worker.assignMu.Unlock()

	return workers[0].worker, workers[1].worker
}

// DispatchBidirectionalTask 分发双向任务
// 将一个代币任务拆分为bid和ask两个子任务，分配给不同Worker
func (c *BidirectionalTaskCoordinator) DispatchBidirectionalTask(
	tc *TokenConfig,
	timeout time.Duration,
) *MergedPriceResult {
	// 检查是否同一Worker（降级为串行）
	workerBid, workerAsk := c.selectTwoWorkers()

	if workerBid == workerAsk {
		// 同一个Worker，使用串行执行
		return c.dispatchSerial(tc, workerBid, timeout)
	}

	// 不同Worker，使用并行执行
	return c.dispatchParallel(tc, workerBid, workerAsk, timeout)
}

// dispatchSerial 串行执行（降级方案，使用同一个Worker）
func (c *BidirectionalTaskCoordinator) dispatchSerial(
	tc *TokenConfig,
	worker *KeyWorker,
	timeout time.Duration,
) *MergedPriceResult {
	startTime := time.Now()

	// 生成TaskID
	taskID := fmt.Sprintf("%s_%s_%d", tc.Symbol, tc.ChainIndex, time.Now().UnixNano())

	// 创建结果收集器
	collector := &TaskResultCollector{
		taskID:      taskID,
		tokenConfig: tc,
		resultChan:  make(chan *MergedPriceResult, 1),
		timeout:     time.NewTimer(timeout),
		startTime:   startTime,
		bidNotify:   make(chan struct{}),
		askNotify:   make(chan struct{}),
		bidNotified: false,
		askNotified: false,
	}

	c.mu.Lock()
	c.resultCollectors[taskID] = collector
	c.mu.Unlock()

	// 创建两个方向的任务
	bidTask := &DirectionalTask{
		TokenConfig: tc,
		Direction:   DirectionTokenToUSDT,
		TaskID:      taskID,
	}

	askTask := &DirectionalTask{
		TokenConfig: tc,
		Direction:   DirectionUSDTToToken,
		TaskID:      taskID,
	}

	// 串行发送（先bid后ask）
	err1 := c.sendTaskToWorker(worker, bidTask)
	err2 := c.sendTaskToWorker(worker, askTask)

	// 如果两个都失败，提前返回错误
	if err1 != nil && err2 != nil {
		c.mu.Lock()
		delete(c.resultCollectors, taskID)
		c.mu.Unlock()
		return &MergedPriceResult{
			TokenConfig: tc,
			Error:       fmt.Errorf("both send failed: bid=%v, ask=%v", err1, err2),
		}
	}

	// 启动结果收集goroutine（即使部分失败也要收集）
	go c.collectResults(collector)

	// 等待结果
	result := <-collector.resultChan

	// 清理collector
	c.mu.Lock()
	delete(c.resultCollectors, taskID)
	c.mu.Unlock()

	return result
}

// dispatchParallel 并行执行（使用两个不同的Worker）
func (c *BidirectionalTaskCoordinator) dispatchParallel(
	tc *TokenConfig,
	workerBid *KeyWorker,
	workerAsk *KeyWorker,
	timeout time.Duration,
) *MergedPriceResult {
	startTime := time.Now()

	// 生成唯一TaskID
	taskID := fmt.Sprintf("%s_%s_%d", tc.Symbol, tc.ChainIndex, time.Now().UnixNano())

	// 创建结果收集器
	collector := &TaskResultCollector{
		taskID:      taskID,
		tokenConfig: tc,
		resultChan:  make(chan *MergedPriceResult, 1),
		timeout:     time.NewTimer(timeout),
		startTime:   startTime,
		bidNotify:   make(chan struct{}),
		askNotify:   make(chan struct{}),
		bidNotified: false,
		askNotified: false,
	}

	c.mu.Lock()
	c.resultCollectors[taskID] = collector
	c.mu.Unlock()

	// 创建两个方向的子任务
	bidTask := &DirectionalTask{
		TokenConfig: tc,
		Direction:   DirectionTokenToUSDT,
		TaskID:      taskID,
	}

	askTask := &DirectionalTask{
		TokenConfig: tc,
		Direction:   DirectionUSDTToToken,
		TaskID:      taskID,
	}

	// 同步分发到两个Worker（sendTaskToWorker内部只是select，很快返回）
	err1 := c.sendTaskToWorker(workerBid, bidTask)
	err2 := c.sendTaskToWorker(workerAsk, askTask)

	// 如果两个都失败，提前返回错误
	if err1 != nil && err2 != nil {
		c.mu.Lock()
		delete(c.resultCollectors, taskID)
		c.mu.Unlock()
		return &MergedPriceResult{
			TokenConfig: tc,
			Error:       fmt.Errorf("both send failed: bid=%v, ask=%v", err1, err2),
		}
	}

	// 启动结果收集goroutine（即使部分失败也要收集）
	go c.collectResults(collector)

	// 等待结果
	result := <-collector.resultChan

	// 清理collector
	c.mu.Lock()
	delete(c.resultCollectors, taskID)
	c.mu.Unlock()

	return result
}

// sendTaskToWorker 发送任务到Worker
// 返回error表示发送失败，此时已通知collector该方向失败
func (c *BidirectionalTaskCoordinator) sendTaskToWorker(worker *KeyWorker, task *DirectionalTask) error {
	select {
	case worker.DirectionalTaskChan <- task:
		// 成功发送
		return nil
	case <-time.After(1 * time.Second):
		// 超时，构造失败结果并通知collector
		log.Printf("[OKX Coordinator] Warning: timeout sending %s task for %s to worker %d",
			task.Direction, task.TokenConfig.Symbol, worker.ID)

		failedResult := &FetchResult{
			TokenConfig: task.TokenConfig,
			Direction:   task.Direction,
			Error:       fmt.Errorf("timeout sending task to worker %d", worker.ID),
		}
		c.OnDirectionalResult(task.TaskID, task.Direction, failedResult)
		return fmt.Errorf("send timeout to worker %d", worker.ID)
	}
}

// OnDirectionalResult 接收单向结果（由Worker调用）
func (c *BidirectionalTaskCoordinator) OnDirectionalResult(
	taskID string,
	direction QuoteDirection,
	result *FetchResult,
) {
	c.mu.Lock()
	collector, exists := c.resultCollectors[taskID]
	c.mu.Unlock()

	if !exists {
		log.Printf("[OKX Coordinator] Warning: no collector for taskID %s", taskID)
		return
	}

	collector.mu.Lock()
	if direction == DirectionTokenToUSDT {
		collector.bidResult = result
		// 通知bid结果已到（关闭channel）
		if !collector.bidNotified {
			close(collector.bidNotify)
			collector.bidNotified = true
		}
	} else {
		collector.askResult = result
		// 通知ask结果已到（关闭channel）
		if !collector.askNotified {
			close(collector.askNotify)
			collector.askNotified = true
		}
	}
	collector.mu.Unlock()
}

// collectResults 收集bid和ask结果并合并
func (c *BidirectionalTaskCoordinator) collectResults(collector *TaskResultCollector) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[OKX Coordinator] Recovered from panic in collectResults: %v", r)

			// 确保发送错误结果，避免上层永久阻塞
			select {
			case collector.resultChan <- &MergedPriceResult{
				TokenConfig: collector.tokenConfig,
				Error:       fmt.Errorf("panic in collectResults: %v", r),
			}:
				log.Printf("[OKX Coordinator] Sent panic error result for %s", collector.tokenConfig.Symbol)
			default:
				// channel可能已有结果，忽略
				log.Printf("[OKX Coordinator] Result channel already has data, skipping panic error")
			}
		}
	}()

	bidReceived := false
	askReceived := false
	bidTime := time.Time{}
	askTime := time.Time{}

	// 使用channel通知替代轮询，优化CPU使用
	for {
		select {
		case <-collector.timeout.C:
			// 超时处理
			c.handleTimeout(collector, bidReceived, askReceived)
			return

		case <-collector.bidNotify:
			// bid结果已到
			bidReceived = true
			bidTime = time.Now()
			if askReceived {
				// 两个都收到，合并返回
				result := c.mergeResults(collector, bidTime, askTime)
				collector.resultChan <- result
				return
			}

		case <-collector.askNotify:
			// ask结果已到
			askReceived = true
			askTime = time.Now()
			if bidReceived {
				// 两个都收到，合并返回
				result := c.mergeResults(collector, bidTime, askTime)
				collector.resultChan <- result
				return
			}
		}
	}
}

// handleTimeout 处理超时情况
func (c *BidirectionalTaskCoordinator) handleTimeout(
	collector *TaskResultCollector,
	bidReceived, askReceived bool,
) {
	collector.mu.Lock()
	defer collector.mu.Unlock()

	result := &MergedPriceResult{
		TokenConfig: collector.tokenConfig,
		Error:       fmt.Errorf("timeout waiting for results"),
	}

	// 如果有部分结果，也返回
	if bidReceived && collector.bidResult != nil && collector.bidResult.Error == nil {
		result.Price = collector.bidResult.Price
		result.Error = fmt.Errorf("timeout: only bid received")
	} else if askReceived && collector.askResult != nil && collector.askResult.Error == nil {
		result.Price = collector.askResult.Price
		result.Error = fmt.Errorf("timeout: only ask received")
	}

	// 记录统计
	partial := result.Price != nil
	c.statsManager.RecordUpdate(
		collector.tokenConfig.Symbol,
		false, // timeout视为失败
		partial,
		0,
	)

	collector.resultChan <- result
}

// mergeResults 合并bid和ask结果
func (c *BidirectionalTaskCoordinator) mergeResults(
	collector *TaskResultCollector,
	bidTime, askTime time.Time,
) *MergedPriceResult {
	bidResult := collector.bidResult
	askResult := collector.askResult

	// 计算延迟
	bidLatency := bidTime.Sub(collector.startTime)
	askLatency := askTime.Sub(collector.startTime)
	timeDiff := time.Duration(0)
	if bidTime.After(askTime) {
		timeDiff = bidTime.Sub(askTime)
	} else {
		timeDiff = askTime.Sub(bidTime)
	}

	result := &MergedPriceResult{
		TokenConfig: collector.tokenConfig,
		BidLatency:  bidLatency,
		AskLatency:  askLatency,
		TimeDiff:    timeDiff,
	}

	// 处理各种情况
	if bidResult.Error == nil && askResult.Error == nil {
		// 两个都成功 - 完美情况
		mergedPrice := bidResult.Price
		mergedPrice.AskPrice = askResult.Price.AskPrice
		mergedPrice.Price = (mergedPrice.BidPrice + mergedPrice.AskPrice) / 2
		result.Price = mergedPrice

		// 价格验证
		result.ValidationWarning = c.validatePrice(mergedPrice)

		// 记录统计
		c.statsManager.RecordUpdate(
			collector.tokenConfig.Symbol,
			true,  // 成功
			false, // 完整价格
			timeDiff,
		)

	} else if bidResult.Error == nil {
		// 只有bid成功
		result.Price = bidResult.Price
		result.Error = fmt.Errorf("ask failed: %w", askResult.Error)

		// 记录统计
		c.statsManager.RecordUpdate(
			collector.tokenConfig.Symbol,
			true,  // 有部分数据算成功
			true,  // 部分价格
			timeDiff,
		)

	} else if askResult.Error == nil {
		// 只有ask成功
		result.Price = askResult.Price
		result.Error = fmt.Errorf("bid failed: %w", bidResult.Error)

		// 记录统计
		c.statsManager.RecordUpdate(
			collector.tokenConfig.Symbol,
			true,  // 有部分数据算成功
			true,  // 部分价格
			timeDiff,
		)

	} else {
		// 都失败
		result.Error = fmt.Errorf("both failed - bid: %v, ask: %v",
			bidResult.Error, askResult.Error)

		// 记录统计
		c.statsManager.RecordUpdate(
			collector.tokenConfig.Symbol,
			false, // 失败
			false,
			0,
		)
	}

	return result
}

// validatePrice 验证价格合理性
func (c *BidirectionalTaskCoordinator) validatePrice(price *common.Price) string {
	return c.ValidatePriceWithHistory(price)
}

// GetStatsManager 获取统计管理器（用于外部访问）
func (c *BidirectionalTaskCoordinator) GetStatsManager() *StatsManager {
	return c.statsManager
}

// Close 关闭协调器
func (c *BidirectionalTaskCoordinator) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 清理所有collector
	for taskID, collector := range c.resultCollectors {
		if collector.timeout != nil {
			collector.timeout.Stop()
		}
		close(collector.resultChan)
		delete(c.resultCollectors, taskID)
	}
}
