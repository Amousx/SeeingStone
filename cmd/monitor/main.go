package main

import (
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/arbitrage"
	"crypto-arbitrage-monitor/internal/exchange/aster"
	"crypto-arbitrage-monitor/internal/exchange/lighter"
	"crypto-arbitrage-monitor/internal/notification"
	"crypto-arbitrage-monitor/internal/ui"
	"crypto-arbitrage-monitor/pkg/common"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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

	log.Println("Starting Crypto Arbitrage Monitor...")

	// 创建价差计算器
	calc := arbitrage.NewCalculator(cfg.MinSpreadPercent)

	// 创建Telegram通知器
	notifier := notification.NewTelegramNotifier(
		cfg.TelegramBotToken,
		cfg.TelegramChatID,
		cfg.EnableNotification,
	)

	// 创建Aster WebSocket客户端
	asterWS := aster.NewWSClient("wss://fstream.asterdex.com/ws", common.MarketTypeFuture)

	// 设置MiniTicker处理器
	asterWS.SetMiniTickerHandler(func(tickers []*aster.WSMiniTickerData) {
		for _, ticker := range tickers {
			// 转换为通用价格格式并更新
			price := aster.ConvertWSMiniTickerToPrice(ticker, common.ExchangeAster, common.MarketTypeFuture)

			// 打印 BTC/ETH/SOL 转换后的价格
			if ticker.Symbol == "BTCUSDT" || ticker.Symbol == "ETHUSDT" || ticker.Symbol == "SOLUSDT" {
				log.Printf("[Price Update] %s: Price=%.2f, BidPrice=%.2f, AskPrice=%.2f, Volume24h=%.2f, Timestamp=%v",
					price.Symbol, price.Price, price.BidPrice, price.AskPrice, price.Volume24h, price.Timestamp)
			}

			calc.UpdatePrice(price)
		}
	})

	// 连接WebSocket
	log.Println("Connecting to Aster WebSocket...")
	if err := asterWS.Connect(); err != nil {
		log.Fatalf("Failed to connect to Aster WebSocket: %v", err)
	}
	defer asterWS.Close()

	// 订阅全市场MiniTicker
	log.Println("Subscribing to Aster !miniTicker@arr...")
	if err := asterWS.Subscribe([]string{"!miniTicker@arr"}); err != nil {
		log.Fatalf("Failed to subscribe to Aster WebSocket: %v", err)
	}
	log.Println("Aster WebSocket subscribed successfully")

	// Step 1: 启动时拉取 REST API 全市场快照（Aster）
	log.Println("Fetching initial Aster prices via REST API...")
	spotClient := aster.NewSpotClient(cfg.AsterSpotBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	futuresClient := aster.NewFuturesClient(cfg.AsterFutureBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	initAsterPrices(spotClient, futuresClient, calc)

	// 获取Lighter市场配置（从官方API）
	log.Println("Fetching Lighter markets from API...")
	lighterMarkets := lighter.GetCommonMarkets()
	lighterAPIBaseURL := lighter.LighterAPIBaseURL
	log.Printf("Found %d Lighter markets to monitor", len(lighterMarkets))

	// 初始化 Lighter 价格数据（REST API 快照）
	log.Println("Fetching initial Lighter prices via REST API...")
	marketIDs := lighter.GetMarketIDs(lighterMarkets)
	if prices, err := lighter.FetchMarketData(lighterAPIBaseURL, marketIDs); err == nil {
		for _, price := range prices {
			calc.UpdatePrice(price)
		}
		log.Printf("Loaded %d Lighter prices from REST API", len(prices))
	} else {
		log.Printf("Warning: Failed to fetch initial Lighter prices: %v", err)
	}

	// 创建 Lighter WebSocket 客户端
	log.Println("Connecting to Lighter WebSocket...")
	var lighterWS *lighter.WSClient
	lighterWS = lighter.NewWSClient("wss://api.lighter.xyz/v1/ws", lighterMarkets, lighterAPIBaseURL, 60)
	lighterWS.SetMessageHandler(func(price *common.Price) {
		calc.UpdatePrice(price)
	})

	if err := lighterWS.Connect(); err != nil {
		log.Printf("Warning: Failed to connect to Lighter WebSocket: %v", err)
		log.Println("Will continue using REST API only for Lighter")
		lighterWS = nil // 设置为 nil 表示未连接
	} else {
		defer lighterWS.Close() // 程序退出时关闭连接

		// 尝试订阅 order_book/all
		log.Println("Subscribing to Lighter order_book/all...")
		if err := lighterWS.SubscribeAll(); err != nil {
			log.Printf("Failed to subscribe to order_book/all: %v", err)
			log.Println("Falling back to individual market subscriptions...")
			// 如果 order_book/all 失败，回退到逐个订阅
			if err := lighterWS.Subscribe(marketIDs); err != nil {
				log.Printf("Failed to subscribe to individual markets: %v", err)
			}
		} else {
			log.Println("Lighter WebSocket subscribed to order_book/all successfully")
		}
	}

	// 创建UI模型（传入calculator引用）
	model := ui.NewModel(calc)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// 记录启动时间（用于冷启动判断）
	startTime := time.Now()

	// 启动价差计算和通知协程
	var wg sync.WaitGroup
	wg.Add(5)

	// 协程1: Aster 智能 REST 补充刷新
	go func() {
		defer wg.Done()

		// 冷启动阶段：前60秒，每2秒刷新一次
		coldStartTicker := time.NewTicker(2 * time.Second)
		defer coldStartTicker.Stop()

		for {
			select {
			case <-coldStartTicker.C:
				if time.Since(startTime) < 60*time.Second {
					// 冷启动阶段：高频全量刷新
					initAsterPrices(spotClient, futuresClient, calc)
					log.Printf("[Cold Start] Aster REST refresh (%.0fs elapsed)", time.Since(startTime).Seconds())
				} else {
					// 切换到正常模式
					coldStartTicker.Stop()
					goto normalMode
				}
			}
		}

	normalMode:
		// 正常阶段：每10秒检查一次，只刷新过期的 symbol
		normalTicker := time.NewTicker(10 * time.Second)
		defer normalTicker.Stop()

		for range normalTicker.C {
			// 获取超过10秒没更新的 Aster symbol
			staleSymbols := calc.GetStaleSymbols(10 * time.Second)
			if len(staleSymbols) > 0 {
				log.Printf("[Normal] Found %d stale Aster symbols, refreshing via REST...", len(staleSymbols))
				initAsterPrices(spotClient, futuresClient, calc)
			}
		}
	}()

	// 协程2: Lighter 智能 REST 补充刷新（类似 Aster）
	go func() {
		defer wg.Done()

		// 冷启动阶段：前60秒，每2秒刷新一次
		coldStartTicker := time.NewTicker(2 * time.Second)
		defer coldStartTicker.Stop()

		for {
			select {
			case <-coldStartTicker.C:
				if time.Since(startTime) < 60*time.Second {
					// 冷启动阶段：高频全量刷新
					if prices, err := lighter.FetchMarketData(lighterAPIBaseURL, marketIDs); err == nil {
						for _, price := range prices {
							calc.UpdatePrice(price)
						}
					}
					log.Printf("[Cold Start] Lighter REST refresh (%.0fs elapsed)", time.Since(startTime).Seconds())
				} else {
					// 切换到正常模式
					coldStartTicker.Stop()
					goto normalMode
				}
			}
		}

	normalMode:
		// 正常阶段：每10秒检查一次，只刷新过期的 symbol
		normalTicker := time.NewTicker(10 * time.Second)
		defer normalTicker.Stop()

		for range normalTicker.C {
			// 获取超过10秒没更新的 Lighter symbol
			staleSymbols := calc.GetStaleSymbols(10 * time.Second)
			if len(staleSymbols) > 0 {
				log.Printf("[Normal] Found %d stale Lighter symbols, refreshing via REST...", len(staleSymbols))
				if prices, err := lighter.FetchMarketData(lighterAPIBaseURL, marketIDs); err == nil {
					for _, price := range prices {
						calc.UpdatePrice(price)
					}
				} else {
					log.Printf("Failed to fetch Lighter REST API: %v", err)
				}
			}
		}
	}()

	// 协程3: 定期计算价差（UI会自己获取数据）
	go func() {
		defer wg.Done()

		ticker := time.NewTicker(time.Duration(cfg.UpdateInterval) * time.Second)
		defer ticker.Stop()

		// 立即执行一次计算
		calc.CalculateArbitrage()
		opportunities := calc.GetOpportunities()
		log.Printf("Initial calculation: %d arbitrage opportunities", len(opportunities))
		if len(opportunities) > 0 {
			log.Printf("Sample opportunity: %s - %s to %s - Spread: %.2f%%",
				opportunities[0].Symbol,
				opportunities[0].Exchange1,
				opportunities[0].Exchange2,
				opportunities[0].SpreadPercent)
		}

		for range ticker.C {
			// 计算套利机会
			calc.CalculateArbitrage()
			opportunities := calc.GetOpportunities()

			if len(opportunities) > 0 {
				log.Printf("Calculated %d arbitrage opportunities (spread range: %.2f%% - %.2f%%)",
					len(opportunities),
					opportunities[len(opportunities)-1].SpreadPercent,
					opportunities[0].SpreadPercent)
			} else {
				log.Printf("Calculated %d arbitrage opportunities", len(opportunities))
			}
		}
	}()

	// 协程4: 监听新的套利机会并发送通知
	go func() {
		defer wg.Done()
		oppChan := calc.GetOpportunityChan()

		for opp := range oppChan {
			// 只通知高价差的机会
			if opp.SpreadPercent >= cfg.MinSpreadPercent && !opp.NotificationSent && notifier.IsEnabled() {
				if err := notifier.SendOpportunity(opp); err != nil {
					log.Printf("Failed to send notification: %v", err)
				} else {
					opp.NotificationSent = true
					log.Printf("Sent notification for %s: %.2f%%", opp.Symbol, opp.SpreadPercent)
				}
			}
		}
	}()

	// 协程5: Symbol 覆盖率监控
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			stats := calc.GetCoverageStats()
			log.Printf("[Coverage] Total: %d, Active: %d, Aster: %d, Lighter: %d",
				stats.TotalSymbols,
				stats.ActiveSymbols,
				stats.AsterSymbols,
				stats.LighterSymbols)

			// 检查覆盖率是否正常
			if stats.ActiveSymbols < stats.TotalSymbols/2 {
				log.Printf("[Warning] Low coverage: only %d/%d symbols are active",
					stats.ActiveSymbols, stats.TotalSymbols)
			}
		}
	}()

	// 运行UI
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running UI: %v", err)
	}

	// 清理
	log.Println("Shutting down...")
}

// initAsterPrices 初始化 Aster 价格数据（不需要 symbols 参数，拉取所有）
func initAsterPrices(spotClient *aster.SpotClient, futuresClient *aster.FuturesClient, calc *arbitrage.Calculator) {
	var wg sync.WaitGroup

	// 获取现货价格
	wg.Add(1)
	go func() {
		defer wg.Done()
		tickers, err := spotClient.GetAllBookTickers()
		if err != nil {
			log.Printf("Failed to fetch spot tickers: %v", err)
			return
		}

		// 获取24h数据
		tickers24h, err := spotClient.GetAll24hrTickers()
		if err != nil {
			log.Printf("Failed to fetch spot 24h tickers: %v", err)
			return
		}

		// 建立volume映射
		volumeMap := make(map[string]float64)
		for _, t := range tickers24h {
			volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
		}

		// 更新价格
		for _, ticker := range tickers {
			volume := volumeMap[ticker.Symbol]
			price := spotClient.ConvertToCommonPrice(&ticker, volume)
			calc.UpdatePrice(price)
		}

		log.Printf("Loaded %d spot prices", len(tickers))
	}()

	// 获取合约价格
	wg.Add(1)
	go func() {
		defer wg.Done()
		tickers, err := futuresClient.GetAllBookTickers()
		if err != nil {
			log.Printf("Failed to fetch futures tickers: %v", err)
			return
		}

		// 获取24h数据
		tickers24h, err := futuresClient.GetAll24hrTickers()
		if err != nil {
			log.Printf("Failed to fetch futures 24h tickers: %v", err)
			return
		}

		// 建立volume映射
		volumeMap := make(map[string]float64)
		for _, t := range tickers24h {
			volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
		}

		// 更新价格
		for _, ticker := range tickers {
			volume := volumeMap[ticker.Symbol]
			price := futuresClient.ConvertToCommonPrice(&ticker, volume)
			calc.UpdatePrice(price)
		}

		log.Printf("Loaded %d futures prices", len(tickers))
	}()

	wg.Wait()
}

// initPrices 初始化价格数据（保留旧版本以兼容）
func initPrices(spotClient *aster.SpotClient, futuresClient *aster.FuturesClient, calc *arbitrage.Calculator, symbols []string) {
	var wg sync.WaitGroup

	// 获取现货价格
	wg.Add(1)
	go func() {
		defer wg.Done()
		tickers, err := spotClient.GetAllBookTickers()
		if err != nil {
			log.Printf("Failed to fetch spot tickers: %v", err)
			return
		}

		// 获取24h数据
		tickers24h, err := spotClient.GetAll24hrTickers()
		if err != nil {
			log.Printf("Failed to fetch spot 24h tickers: %v", err)
			return
		}

		// 建立volume映射
		volumeMap := make(map[string]float64)
		for _, t := range tickers24h {
			volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
		}

		// 更新价格
		for _, ticker := range tickers {
			volume := volumeMap[ticker.Symbol]
			price := spotClient.ConvertToCommonPrice(&ticker, volume)
			calc.UpdatePrice(price)
		}

		log.Printf("Loaded %d spot prices", len(tickers))
	}()

	// 获取合约价格
	wg.Add(1)
	go func() {
		defer wg.Done()
		tickers, err := futuresClient.GetAllBookTickers()
		if err != nil {
			log.Printf("Failed to fetch futures tickers: %v", err)
			return
		}

		// 获取24h数据
		tickers24h, err := futuresClient.GetAll24hrTickers()
		if err != nil {
			log.Printf("Failed to fetch futures 24h tickers: %v", err)
			return
		}

		// 建立volume映射
		volumeMap := make(map[string]float64)
		for _, t := range tickers24h {
			volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
		}

		// 更新价格
		for _, ticker := range tickers {
			volume := volumeMap[ticker.Symbol]
			price := futuresClient.ConvertToCommonPrice(&ticker, volume)
			calc.UpdatePrice(price)
		}

		log.Printf("Loaded %d futures prices", len(tickers))
	}()

	wg.Wait()
}


// parseFloat 解析字符串为float64
func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// getAllSymbols 获取所有可用的交易对
func getAllSymbols(spotClient *aster.SpotClient, futuresClient *aster.FuturesClient) []string {
	symbolMap := make(map[string]bool)

	// 从现货获取交易对
	spotInfo, err := spotClient.GetExchangeInfo()
	if err != nil {
		log.Printf("Warning: Failed to get spot exchange info: %v", err)
	} else {
		for _, symbol := range spotInfo.Symbols {
			// 只添加状态为TRADING的交易对
			if symbol.Status == "TRADING" {
				symbolMap[symbol.Symbol] = true
			}
		}
		log.Printf("Loaded %d spot symbols", len(spotInfo.Symbols))
	}

	// 从合约获取交易对
	futuresInfo, err := futuresClient.GetExchangeInfo()
	if err != nil {
		log.Printf("Warning: Failed to get futures exchange info: %v", err)
	} else {
		for _, symbol := range futuresInfo.Symbols {
			// 只添加状态为TRADING的交易对
			if symbol.Status == "TRADING" {
				symbolMap[symbol.Symbol] = true
			}
		}
		log.Printf("Loaded %d futures symbols", len(futuresInfo.Symbols))
	}

	// 如果没有获取到任何交易对，使用默认列表
	if len(symbolMap) == 0 {
		log.Println("Warning: No symbols found from API, using default list")
		return []string{"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "ADAUSDT"}
	}

	// 转换为数组
	symbols := make([]string, 0, len(symbolMap))
	for symbol := range symbolMap {
		symbols = append(symbols, symbol)
	}

	return symbols
}

