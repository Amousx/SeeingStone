package main

import (
	"context"
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/exchange/aster"
	"crypto-arbitrage-monitor/internal/exchange/binance"
	"crypto-arbitrage-monitor/internal/exchange/lighter"
	"crypto-arbitrage-monitor/internal/pricestore"
	"crypto-arbitrage-monitor/internal/web"
	"crypto-arbitrage-monitor/pkg/common"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"time"
)

func main() {
	// 加载配置
	cfg := config.LoadConfig()

	// 创建日志文件
	logFile, err := os.OpenFile("arbitrage.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
	}

	log.Println("=== Starting Crypto Price Collector ===")

	// 创建价格存储器（双索引结构）
	store := pricestore.NewPriceStore()

	// 启动Aster WebSocket
	asterWS := startAsterWebSocket(store)
	if asterWS != nil {
		defer asterWS.Close()
	}

	// 启动Aster REST初始化和定期更新
	asterSpotClient := aster.NewSpotClient(cfg.AsterSpotBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	asterFuturesClient := aster.NewFuturesClient(cfg.AsterFutureBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)

	// 启动Lighter WebSocket连接池和REST
	lighterMarkets := lighter.GetCommonMarkets()
	lighterAPIBaseURL := lighter.LighterAPIBaseURL
	marketIDs := lighter.GetMarketIDs(lighterMarkets)
	lighterWSPool := startLighterWSPool(store, lighterMarkets, lighterAPIBaseURL, marketIDs)
	if lighterWSPool != nil {
		defer lighterWSPool.Close()
	}

	// Binance（可选，需要代理）
	var binanceSpotWSPool *binance.SpotWSPool
	var binanceFuturesWS *binance.WSClient

	log.Println("[Binance] Enabled")
	// 配置Binance代理
	if cfg.HTTPSProxy != "" {
		binance.SetProxyURL(cfg.HTTPSProxy)
	} else if cfg.HTTPProxy != "" {
		binance.SetProxyURL(cfg.HTTPProxy)
	}

	// 启动Binance现货 WebSocket 连接池（分片模式）
	binanceSpotWSPool = startBinanceSpotWSPool(store)
	if binanceSpotWSPool != nil {
		defer binanceSpotWSPool.Close()
	}

	// 启动Binance合约 WebSocket
	binanceFuturesWS = startBinanceFuturesWebSocket(store)
	if binanceFuturesWS != nil {
		defer binanceFuturesWS.Close()
	}

	// 启动Web服务器
	webServer := web.NewServer(store, ":8080")
	go func() {
		if err := webServer.Start(); err != nil {
			log.Printf("[Web Server] Error: %v", err)
		}
	}()
	log.Println("[Web Server] Access at http://localhost:8080")
	println("[Web Server] Access at http://localhost:8080")

	// 等待一小段时间确保服务器启动，然后自动打开浏览器
	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser("http://localhost:8080/")
	}()

	// 启动后台任务
	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// 任务1: Aster REST数据获取
	wg.Add(1)
	go func() {
		defer wg.Done()
		runAsterRESTUpdater(asterSpotClient, asterFuturesClient, store, stopChan)
	}()

	// 任务2: Lighter REST数据获取
	wg.Add(1)
	go func() {
		defer wg.Done()
		runLighterRESTUpdater(lighterAPIBaseURL, marketIDs, store, stopChan)
	}()

	// 任务3: Binance REST数据获取（可选）
	wg.Add(1)
	go func() {
		defer wg.Done()
		runBinanceRESTUpdater(store, stopChan)
	}()

	// 任务4: 统计信息打印
	wg.Add(1)
	go func() {
		defer wg.Done()
		runStatsReporter(store, stopChan)
	}()

	// 任务5: 定期清理过期数据
	wg.Add(1)
	go func() {
		defer wg.Done()
		runDataCleaner(store, stopChan)
	}()

	// 等待退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	log.Println("Price collector is running. Press Ctrl+C to stop.")

	<-sigChan
	log.Println("Shutting down gracefully...")

	// 通知所有goroutine停止
	close(stopChan)

	// 等待所有goroutine完成
	wg.Wait()

	log.Println("Shutdown complete.")
}

// startAsterWebSocket 启动Aster WebSocket连接
func startAsterWebSocket(store *pricestore.PriceStore) *aster.WSClient {
	log.Println("[Aster] Connecting to WebSocket...")

	asterWS := aster.NewWSClient("wss://fstream.asterdex.com/ws", common.MarketTypeFuture)

	// 使用BookTicker获取真实的bid/ask价格（推荐）
	asterWS.SetBookTickerHandler(func(ticker *aster.WSBookTickerData) {
		price := aster.ConvertWSBookTickerToPrice(ticker, common.ExchangeAster, common.MarketTypeFuture)
		store.UpdatePrice(price)
	})

	if err := asterWS.Connect(); err != nil {
		log.Printf("[Aster] Failed to connect WebSocket: %v", err)
		return nil
	}

	// 订阅全市场最优挂单信息（实时bid/ask）
	if err := asterWS.Subscribe([]string{"!bookTicker"}); err != nil {
		log.Printf("[Aster] Failed to subscribe: %v", err)
		return nil
	}

	log.Println("[Aster] WebSocket connected and subscribed to bookTicker")
	return asterWS
}

// startLighterWSPool 启动Lighter WebSocket连接池（分片模式）
func startLighterWSPool(store *pricestore.PriceStore, markets []*lighter.Market, apiBaseURL string, marketIDs []int) *lighter.WSPool {
	log.Println("[Lighter] Initializing WebSocket pool...")

	// 步骤1：冷启动 - 使用 REST API 获取所有市场的快照数据
	log.Println("[Lighter] Fetching initial snapshot via REST API...")
	prices, err := lighter.FetchMarketData(apiBaseURL, marketIDs)
	if err != nil {
		log.Printf("[Lighter] Failed to fetch initial snapshot: %v", err)
		// 继续启动 WebSocket，即使 REST 失败
	} else {
		// 更新到 store（冷启动数据）
		for _, price := range prices {
			store.UpdatePrice(price)
		}
		log.Printf("[Lighter] Loaded %d markets from REST snapshot", len(prices))
	}

	// 步骤2：创建 WebSocket 连接池（每个连接 60 个市场）
	pool := lighter.NewWSPool(markets, 60)

	// 设置价格处理器
	pool.SetPriceHandler(func(price *common.Price) {
		store.UpdatePrice(price)
	})

	// 步骤3：启动连接池
	if err := pool.Start(); err != nil {
		log.Printf("[Lighter] Failed to start WebSocket pool: %v", err)
		return nil
	}

	log.Println("[Lighter] WebSocket pool started successfully")
	return pool
}

// startBinanceSpotWSPool 启动Binance现货WebSocket连接池（分片模式）
func startBinanceSpotWSPool(store *pricestore.PriceStore) *binance.SpotWSPool {
	log.Println("[Binance Spot] Initializing WebSocket pool...")

	// 步骤1：冷启动 - 使用 REST API 获取所有交易对的快照数据
	log.Println("[Binance Spot] Fetching initial snapshot via REST API...")
	prices, err := binance.FetchSpotPrices()
	if err != nil {
		log.Printf("[Binance Spot] Failed to fetch initial snapshot: %v", err)
		return nil
	}

	// 更新到 store（冷启动数据）
	symbols := make([]string, 0, len(prices))
	for _, price := range prices {
		store.UpdatePrice(price)
		symbols = append(symbols, price.Symbol)
	}
	log.Printf("[Binance Spot] Loaded %d symbols from REST snapshot", len(symbols))

	// 步骤2：创建 WebSocket 连接池（每个连接 50 个 symbol）
	pool := binance.NewSpotWSPool(symbols, 50)

	// 设置 BookTicker 处理器
	pool.SetBookTickerHandler(func(ticker *binance.WSBookTickerData) {
		price := binance.ConvertWSBookTickerToPrice(ticker, common.ExchangeBinance, common.MarketTypeSpot)
		store.UpdatePrice(price)
	})

	// 步骤3：启动连接池
	if err := pool.Start(); err != nil {
		log.Printf("[Binance Spot] Failed to start WebSocket pool: %v", err)
		return nil
	}

	log.Println("[Binance Spot] WebSocket pool started successfully")
	return pool
}

// startBinanceFuturesWebSocket 启动Binance合约WebSocket（使用BookTicker获取真实bid/ask）
func startBinanceFuturesWebSocket(store *pricestore.PriceStore) *binance.WSClient {
	log.Println("[Binance Futures] Connecting to WebSocket...")

	// 使用bookTicker获取真实的bid/ask价格
	binanceFuturesWS := binance.NewWSClient("wss://fstream.binance.com/ws/!bookTicker", common.MarketTypeFuture)

	// 设置BookTicker处理器（真实bid/ask）
	binanceFuturesWS.SetBookTickerHandler(func(ticker *binance.WSBookTickerData) {
		price := binance.ConvertWSBookTickerToPrice(ticker, common.ExchangeBinance, common.MarketTypeFuture)
		store.UpdatePrice(price)
	})

	if err := binanceFuturesWS.Connect(); err != nil {
		log.Printf("[Binance Futures] Failed to connect WebSocket: %v", err)
		return nil
	}

	log.Println("[Binance Futures] WebSocket connected (BookTicker)")
	return binanceFuturesWS
}

// runAsterRESTUpdater 运行Aster REST API更新任务（状态机模式，带context和timeout）
func runAsterRESTUpdater(spotClient *aster.SpotClient, futuresClient *aster.FuturesClient, store *pricestore.PriceStore, stopChan <-chan struct{}) {
	const (
		stateColdStart = iota
		stateNormal
	)

	// 立即执行一次初始化（带timeout）
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	fetchAsterPrices(ctx, spotClient, futuresClient, store)
	cancel()

	state := stateColdStart
	startTime := time.Now()

	coldStartInterval := 2 * time.Second
	normalInterval := 30 * time.Second

	ticker := time.NewTicker(coldStartInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return

		case <-ticker.C:
			// 状态转换
			if state == stateColdStart && time.Since(startTime) >= 60*time.Second {
				state = stateNormal
				ticker.Reset(normalInterval)
				log.Println("[Aster REST] Switched to normal mode")
			}

			// 执行更新（带timeout和可中断）
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			// 在goroutine中执行，允许被stopChan中断
			done := make(chan struct{})
			go func() {
				fetchAsterPrices(ctx, spotClient, futuresClient, store)
				close(done)
			}()

			select {
			case <-done:
				cancel()
			case <-stopChan:
				cancel()
				return
			case <-ctx.Done():
				cancel()
				log.Println("[Aster REST] Fetch timeout")
			}
		}
	}
}

// runLighterRESTUpdater 运行Lighter REST API更新任务（状态机模式）
func runLighterRESTUpdater(apiBaseURL string, marketIDs []int, store *pricestore.PriceStore, stopChan <-chan struct{}) {
	const (
		stateColdStart = iota
		stateNormal
	)

	// 立即执行一次初始化
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	fetchLighterPrices(ctx, apiBaseURL, marketIDs, store)
	cancel()

	state := stateColdStart
	startTime := time.Now()

	coldStartInterval := 2 * time.Second
	normalInterval := 30 * time.Second

	ticker := time.NewTicker(coldStartInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return

		case <-ticker.C:
			// 状态转换
			if state == stateColdStart && time.Since(startTime) >= 60*time.Second {
				state = stateNormal
				ticker.Reset(normalInterval)
				log.Println("[Lighter REST] Switched to normal mode")
			}

			// 执行更新（带timeout和可中断）
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

			done := make(chan struct{})
			go func() {
				fetchLighterPrices(ctx, apiBaseURL, marketIDs, store)
				close(done)
			}()

			select {
			case <-done:
				cancel()
			case <-stopChan:
				cancel()
				return
			case <-ctx.Done():
				cancel()
				log.Println("[Lighter REST] Fetch timeout")
			}
		}
	}
}

// runBinanceRESTUpdater 运行Binance REST API更新任务（状态机模式）
func runBinanceRESTUpdater(store *pricestore.PriceStore, stopChan <-chan struct{}) {
	const (
		stateColdStart = iota
		stateNormal
	)

	// 立即执行一次初始化
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	fetchBinancePrices(ctx, store)
	cancel()

	state := stateColdStart
	startTime := time.Now()

	coldStartInterval := 5 * time.Second
	normalInterval := 60 * time.Second

	ticker := time.NewTicker(coldStartInterval)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return

		case <-ticker.C:
			// 状态转换
			if state == stateColdStart && time.Since(startTime) >= 60*time.Second {
				state = stateNormal
				ticker.Reset(normalInterval)
				log.Println("[Binance REST] Switched to normal mode")
			}

			// 执行更新（带timeout和可中断）
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

			done := make(chan struct{})
			go func() {
				fetchBinancePrices(ctx, store)
				close(done)
			}()

			select {
			case <-done:
				cancel()
			case <-stopChan:
				cancel()
				return
			case <-ctx.Done():
				cancel()
				log.Println("[Binance REST] Fetch timeout")
			}
		}
	}
}

// runStatsReporter 定期打印统计信息
func runStatsReporter(store *pricestore.PriceStore, stopChan <-chan struct{}) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			stats := store.GetStats()
			activePrices := len(store.GetActivePrices(60 * time.Second))

			log.Printf("[Stats] Total: %d prices, Active: %d, Symbols: %d, Exchanges: %d",
				stats.TotalPrices, activePrices, stats.TotalSymbols, stats.TotalExchanges)

			for exchange, count := range stats.ByExchange {
				log.Printf("  - %s: %d prices", exchange, count)
			}
		}
	}
}

// runDataCleaner 定期清理过期数据
func runDataCleaner(store *pricestore.PriceStore, stopChan <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ticker.C:
			removed := store.CleanStaleData(10 * time.Minute)
			if removed > 0 {
				log.Printf("[Cleaner] Removed %d stale price entries", removed)
			}
		}
	}
}

// fetchAsterPrices 获取Aster价格数据（支持context取消）
func fetchAsterPrices(ctx context.Context, spotClient *aster.SpotClient, futuresClient *aster.FuturesClient, store *pricestore.PriceStore) {
	var wg sync.WaitGroup
	doneChan := make(chan struct{})

	// 获取现货价格
	wg.Add(1)
	go func() {
		defer wg.Done()
		tickers, err := spotClient.GetAllBookTickers()
		if err != nil {
			log.Printf("[Aster Spot] Failed to fetch prices: %v", err)
			return
		}

		tickers24h, err := spotClient.GetAll24hrTickers()
		if err != nil {
			log.Printf("[Aster Spot] Failed to fetch 24h data: %v", err)
			return
		}

		volumeMap := make(map[string]float64)
		for _, t := range tickers24h {
			volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
		}

		for _, ticker := range tickers {
			volume := volumeMap[ticker.Symbol]
			price := spotClient.ConvertToCommonPrice(&ticker, volume)
			store.UpdatePrice(price)
		}

		log.Printf("[Aster Spot] Fetched %d prices", len(tickers))
	}()

	// 获取合约价格
	wg.Add(1)
	go func() {
		defer wg.Done()
		tickers, err := futuresClient.GetAllBookTickers()
		if err != nil {
			log.Printf("[Aster Futures] Failed to fetch prices: %v", err)
			return
		}

		tickers24h, err := futuresClient.GetAll24hrTickers()
		if err != nil {
			log.Printf("[Aster Futures] Failed to fetch 24h data: %v", err)
			return
		}

		volumeMap := make(map[string]float64)
		for _, t := range tickers24h {
			volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
		}

		for _, ticker := range tickers {
			volume := volumeMap[ticker.Symbol]
			price := futuresClient.ConvertToCommonPrice(&ticker, volume)
			store.UpdatePrice(price)
		}

		log.Printf("[Aster Futures] Fetched %d prices", len(tickers))
	}()

	// 等待完成或context取消
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		// 正常完成
	case <-ctx.Done():
		// Context取消，等待goroutines完成（但不会阻塞太久）
		log.Println("[Aster] Fetch cancelled by context")
	}
}

// fetchLighterPrices 获取Lighter价格数据（支持context取消）
func fetchLighterPrices(ctx context.Context, apiBaseURL string, marketIDs []int, store *pricestore.PriceStore) {
	done := make(chan struct{})

	go func() {
		prices, err := lighter.FetchMarketData(apiBaseURL, marketIDs)
		if err != nil {
			log.Printf("[Lighter] Failed to fetch prices: %v", err)
			close(done)
			return
		}

		for _, price := range prices {
			store.UpdatePrice(price)
		}

		log.Printf("[Lighter] Fetched %d prices", len(prices))
		close(done)
	}()

	select {
	case <-done:
		// 正常完成
	case <-ctx.Done():
		log.Println("[Lighter] Fetch cancelled by context")
	}
}

// fetchBinancePrices 获取Binance价格数据（支持context取消）
func fetchBinancePrices(ctx context.Context, store *pricestore.PriceStore) {
	var wg sync.WaitGroup
	doneChan := make(chan struct{})

	// 获取现货价格
	wg.Add(1)
	go func() {
		defer wg.Done()
		prices, err := binance.FetchSpotPrices()
		if err != nil {
			log.Printf("[Binance Spot] Failed to fetch prices: %v", err)
			return
		}

		for _, price := range prices {
			store.UpdatePrice(price)
		}

		log.Printf("[Binance Spot] Fetched %d prices", len(prices))
	}()

	// 获取合约价格
	wg.Add(1)
	go func() {
		defer wg.Done()
		prices, err := binance.FetchFuturesPrices()
		if err != nil {
			log.Printf("[Binance Futures] Failed to fetch prices: %v", err)
			return
		}

		for _, price := range prices {
			store.UpdatePrice(price)
		}

		log.Printf("[Binance Futures] Fetched %d prices", len(prices))
	}()

	// 等待完成或context取消
	go func() {
		wg.Wait()
		close(doneChan)
	}()

	select {
	case <-doneChan:
		// 正常完成
	case <-ctx.Done():
		log.Println("[Binance] Fetch cancelled by context")
	}
}

// parseFloat 解析字符串为float64
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// openBrowser 根据操作系统打开默认浏览器
func openBrowser(url string) {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default: // linux, freebsd, openbsd, netbsd
		cmd = exec.Command("xdg-open", url)
	}

	err := cmd.Start()
	if err != nil {
		log.Printf("[Browser] Failed to open browser: %v", err)
	} else {
		log.Printf("[Browser] Opening %s in default browser", url)
	}
}
