package okx

import (
	"context"
	"crypto-arbitrage-monitor/internal/pricestore"
	"fmt"
	"log"
	"time"
)

// PriceFetcher OKX价格获取器（使用多Worker架构）
type PriceFetcher struct {
	apiConfigs     []*APIConfig
	tokenConfigs   []*TokenConfig
	store          *pricestore.PriceStore
	workers        []*KeyWorker
	taskQueue      chan *TokenConfig
	ctx            context.Context
	cancel         context.CancelFunc
	priceUpdater   *TokenPriceUpdater            // DefaultPrice自动更新器
	okxClient      *Client                       // OKX客户端（用于价格更新）
	coordinator    *BidirectionalTaskCoordinator // 双向任务协调器（并行模式）
	enableParallel bool                          // 是否启用并行模式
}

// NewPriceFetcher 创建价格获取器
func NewPriceFetcher(apiConfigs []*APIConfig, tokenConfigs []*TokenConfig, store *pricestore.PriceStore) *PriceFetcher {
	ctx, cancel := context.WithCancel(context.Background())

	// 检查API配置
	if len(apiConfigs) == 0 {
		log.Println("[OKX] Warning: No API configs provided, PriceFetcher will not be functional")
		return &PriceFetcher{
			apiConfigs:     apiConfigs,
			tokenConfigs:   tokenConfigs,
			store:          store,
			workers:        make([]*KeyWorker, 0),
			taskQueue:      make(chan *TokenConfig, len(tokenConfigs)),
			ctx:            ctx,
			cancel:         cancel,
			okxClient:      nil,
			priceUpdater:   nil,
			coordinator:    nil,
			enableParallel: false,
		}
	}

	// 创建OKX客户端（用于价格更新）
	okxClient := NewClient(apiConfigs)

	// 读取并行模式配置（默认启用）
	enableParallel := getEnableParallelFromEnv()

	fetcher := &PriceFetcher{
		apiConfigs:     apiConfigs,
		tokenConfigs:   tokenConfigs,
		store:          store,
		workers:        make([]*KeyWorker, 0),
		taskQueue:      make(chan *TokenConfig, len(tokenConfigs)),
		ctx:            ctx,
		cancel:         cancel,
		okxClient:      okxClient,
		enableParallel: enableParallel,
	}

	// 为每个API Key创建一个Worker
	for i, apiConfig := range apiConfigs {
		worker := NewKeyWorker(i+1, apiConfig, store)
		fetcher.workers = append(fetcher.workers, worker)

		// 启动Worker
		go worker.Run(ctx)
	}

	log.Printf("[OKX] Created %d workers for parallel fetching (total throughput: %d req/s)", len(fetcher.workers), len(fetcher.workers))

	// 创建并配置协调器（如果启用并行且有足够的Worker）
	if enableParallel && len(fetcher.workers) >= 2 {
		// 读取价格验证配置
		maxSpreadPercent := getMaxSpreadPercentFromEnv()
		maxPriceChangePercent := getMaxPriceChangePercentFromEnv()
		rejectInvalidPrices := getRejectInvalidPricesFromEnv()

		// 创建协调器
		fetcher.coordinator = NewBidirectionalTaskCoordinator(
			fetcher.workers,
			store,
			maxSpreadPercent,
			maxPriceChangePercent,
			rejectInvalidPrices,
		)

		// 将协调器注入到每个Worker
		for _, worker := range fetcher.workers {
			worker.coordinator = fetcher.coordinator
		}

		log.Printf("[OKX] Parallel mode enabled with %d workers (spread: %.1f%%, change: %.1f%%, reject: %v)",
			len(fetcher.workers), maxSpreadPercent, maxPriceChangePercent, rejectInvalidPrices)
	} else {
		if !enableParallel {
			log.Println("[OKX] Parallel mode disabled by configuration, using serial mode")
		} else {
			log.Printf("[OKX] Parallel mode disabled: only %d worker(s) available (need >= 2), using serial mode", len(fetcher.workers))
		}
	}

	// 初始化并启动TokenPriceUpdater（自动更新DefaultPrice）
	fetcher.priceUpdater = NewTokenPriceUpdater(okxClient, tokenConfigs)
	if fetcher.priceUpdater != nil {
		fetcher.priceUpdater.Start()
		log.Println("[OKX] TokenPriceUpdater started (will update DefaultPrice every 4 hours)")
	} else {
		log.Println("[OKX] TokenPriceUpdater not started (client or tokens not available)")
	}

	return fetcher
}

// FetchAllPrices 获取所有代币价格（使用coordinator）
func (f *PriceFetcher) FetchAllPrices() error {
	if f == nil {
		return fmt.Errorf("fetcher is nil")
	}

	if len(f.workers) == 0 {
		return fmt.Errorf("no workers available (API configs not loaded)")
	}

	if len(f.tokenConfigs) == 0 {
		return fmt.Errorf("no token configs loaded")
	}

	if f.coordinator == nil {
		return fmt.Errorf("coordinator not initialized")
	}

	return f.fetchAllPricesParallel()
}

// fetchAllPricesParallel 并行模式获取价格（使用协调器）
func (f *PriceFetcher) fetchAllPricesParallel() error {
	startTime := time.Now()
	log.Printf("[OKX] Fetching prices (PARALLEL mode) for %d tokens using %d workers...", len(f.tokenConfigs), len(f.workers))

	// 创建结果收集channel
	results := make(chan *MergedPriceResult, len(f.tokenConfigs))

	// 使用semaphore限制并发goroutine数量（避免goroutine风暴）
	// 最多workers*2个任务同时执行，减少从~4N到~4*(workers*2)个goroutine
	maxConcurrent := len(f.workers) * 2
	if maxConcurrent < 2 {
		maxConcurrent = 2 // 至少允许2个并发
	}
	sem := make(chan struct{}, maxConcurrent)

	log.Printf("[OKX] Using semaphore to limit concurrent tasks to %d (workers*2)", maxConcurrent)

	// 并发分发所有任务（受限）
	for _, tc := range f.tokenConfigs {
		sem <- struct{}{} // 获取信号量

		go func(tokenConfig *TokenConfig) {
			defer func() { <-sem }() // 释放信号量

			result := f.coordinator.DispatchBidirectionalTask(tokenConfig, 5*time.Second)
			results <- result
		}(tc)
	}

	// 收集结果
	successCount := 0
	errorCount := 0
	partialCount := 0
	timeout := time.After(time.Duration(len(f.tokenConfigs)+10) * time.Second)

	for i := 0; i < len(f.tokenConfigs); i++ {
		select {
		case result := <-results:
			if result.Error != nil {
				log.Printf("[OKX] Failed to fetch %s: %v", result.TokenConfig.Symbol, result.Error)
				errorCount++

				// 检查是否有部分结果
				if result.Price != nil {
					partialCount++
					f.store.UpdatePrice(result.Price)
				}
			} else {
				successCount++

				// 更新价格到store
				if result.Price != nil {
					f.store.UpdatePrice(result.Price)
				}

				// 打印时间差统计（用于监控）
				if result.TimeDiff > 0 {
					log.Printf("[OKX] %s: bid-ask time diff = %.0fms",
						result.TokenConfig.Symbol, result.TimeDiff.Seconds()*1000)
				}

				// 如果有验证警告，记录日志
				if result.ValidationWarning != "" {
					log.Printf("[OKX] %s: validation warning: %s",
						result.TokenConfig.Symbol, result.ValidationWarning)
				}
			}

		case <-timeout:
			log.Printf("[OKX] Fetch timeout, processed %d/%d tokens", i, len(f.tokenConfigs))
			errorCount += len(f.tokenConfigs) - i
			goto done
		}
	}

done:
	elapsed := time.Since(startTime)
	avgTime := elapsed.Seconds() / float64(len(f.tokenConfigs))
	log.Printf("[OKX] Parallel fetch completed in %.2fs: %d success, %d errors, %d partial (avg %.2fs/token, throughput: %.2f req/s)",
		elapsed.Seconds(), successCount, errorCount, partialCount, avgTime, float64(successCount)/elapsed.Seconds())

	// 打印统计摘要
	if f.coordinator != nil && f.coordinator.statsManager != nil {
		f.coordinator.statsManager.PrintSummary()
	}

	return nil
}

// fetchAllPricesSerial 串行模式已移除
// Worker不再支持TaskChan，所有操作现在都通过coordinator的并行模式完成

// Close 关闭Fetcher和所有Workers
func (f *PriceFetcher) Close() {
	// 停止价格更新器
	if f.priceUpdater != nil {
		f.priceUpdater.Stop()
	}

	// 关闭协调器
	if f.coordinator != nil {
		f.coordinator.Close()
	}

	// 关闭OKX客户端
	if f.okxClient != nil {
		f.okxClient.Close()
	}

	// 取消context
	f.cancel()

	// 关闭所有workers
	for _, worker := range f.workers {
		worker.Close()
	}

	close(f.taskQueue)
}

// getEnableParallelFromEnv 从环境变量读取并行模式配置
// 默认启用（true）
func getEnableParallelFromEnv() bool {
	// TODO: 从环境变量读取 OKX_PARALLEL_MODE
	// 暂时硬编码为true，等待配置系统集成
	return true
}

// getMaxSpreadPercentFromEnv 从环境变量读取最大价差百分比
// 默认5.0%
func getMaxSpreadPercentFromEnv() float64 {
	// TODO: 从环境变量读取 OKX_MAX_SPREAD_PERCENT
	// 暂时硬编码为5.0
	return 5.0
}

// getMaxPriceChangePercentFromEnv 从环境变量读取最大价格变化百分比
// 默认30.0%
func getMaxPriceChangePercentFromEnv() float64 {
	// TODO: 从环境变量读取 OKX_MAX_PRICE_CHANGE_PERCENT
	// 暂时硬编码为30.0
	return 30.0
}

// getRejectInvalidPricesFromEnv 从环境变量读取是否拒绝异常价格
// 默认false（只警告不拒绝）
func getRejectInvalidPricesFromEnv() bool {
	// TODO: 从环境变量读取 OKX_REJECT_INVALID_PRICES
	// 暂时硬编码为false
	return false
}
