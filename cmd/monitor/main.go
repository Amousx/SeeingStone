package main

import (
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/arbitrage"
	"crypto-arbitrage-monitor/internal/exchange/aster"
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

	// 创建Aster客户端
	spotClient := aster.NewSpotClient(cfg.AsterSpotBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	futuresClient := aster.NewFuturesClient(cfg.AsterFutureBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)

	// 创建WebSocket客户端
	// 使用 /stream 端点，用于组合订阅多个stream
	wsSpotURL := cfg.AsterWSSpotURL + "/stream"
	wsFuturesURL := cfg.AsterWSFutureURL + "/stream"
	wsSpot := aster.NewWSClient(wsSpotURL, common.MarketTypeSpot)
	wsFutures := aster.NewWSClient(wsFuturesURL, common.MarketTypeFuture)

	// 获取所有可用的交易对
	log.Println("Fetching available symbols...")
	allSymbols := getAllSymbols(spotClient, futuresClient)
	log.Printf("Found %d symbols to monitor", len(allSymbols))

	// 初始化价格数据（使用REST API）
	log.Println("Fetching initial prices...")
	initPrices(spotClient, futuresClient, calc, allSymbols)

	// 连接WebSocket并订阅（不阻塞程序启动）
	log.Println("Connecting to WebSocket...")
	if err := connectWebSockets(wsSpot, wsFutures, calc, allSymbols); err != nil {
		log.Printf("Warning: Failed to connect WebSocket: %v", err)
		log.Println("Continuing with REST API polling only...")
	} else {
		log.Println("WebSocket connected successfully")
	}

	// 创建UI模型（传入calculator引用）
	model := ui.NewModel(calc)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// 启动价差计算和通知协程
	var wg sync.WaitGroup
	wg.Add(3)

	// 协程1: 定期刷新价格（使用REST API作为备用）
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(30 * time.Second) // 每30秒刷新一次
		defer ticker.Stop()

		for range ticker.C {
			log.Println("Refreshing prices via REST API...")
			initPrices(spotClient, futuresClient, calc, allSymbols)
		}
	}()

	// 协程2: 定期计算价差（UI会自己获取数据）
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

	// 协程3: 监听新的套利机会并发送通知
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

	// 运行UI
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running UI: %v", err)
	}

	// 清理
	log.Println("Shutting down...")
	wsSpot.Close()
	wsFutures.Close()
}

// initPrices 初始化价格数据
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

// connectWebSockets 连接WebSocket并订阅
func connectWebSockets(wsSpot, wsFutures *aster.WSClient, calc *arbitrage.Calculator, symbols []string) error {
	// 限制订阅数量以符合API限制
	// 现货最多1024个streams，合约最多200个streams
	const maxFuturesStreams = 180 // 留一些余量
	const maxSpotStreams = 180    // 为了平衡，现货也限制180个

	spotSymbols := symbols
	futuresSymbols := symbols

	if len(symbols) > maxSpotStreams {
		spotSymbols = symbols[:maxSpotStreams]
		log.Printf("Limiting spot WebSocket subscriptions to %d symbols", maxSpotStreams)
	}

	if len(symbols) > maxFuturesStreams {
		futuresSymbols = symbols[:maxFuturesStreams]
		log.Printf("Limiting futures WebSocket subscriptions to %d symbols", maxFuturesStreams)
	}

	// 连接现货WebSocket
	if err := wsSpot.Connect(); err != nil {
		return err
	}

	// 设置现货消息处理器
	wsSpot.SetMessageHandler(func(msg *aster.WSMessage) {
		// 解析BookTicker消息
		ticker, err := aster.ParseBookTickerMessage(msg.Data)
		if err != nil {
			return
		}

		// 转换为通用价格
		price := aster.ConvertWSBookTickerToPrice(ticker, common.ExchangeAster, common.MarketTypeSpot)
		calc.UpdatePrice(price)
	})

	// 订阅现货流
	spotStreams := aster.BuildBookTickerStreams(spotSymbols)
	if err := wsSpot.Subscribe(spotStreams); err != nil {
		return err
	}

	// 连接合约WebSocket
	if err := wsFutures.Connect(); err != nil {
		return err
	}

	// 设置合约消息处理器
	wsFutures.SetMessageHandler(func(msg *aster.WSMessage) {
		// 解析BookTicker消息
		ticker, err := aster.ParseBookTickerMessage(msg.Data)
		if err != nil {
			return
		}

		// 转换为通用价格
		price := aster.ConvertWSBookTickerToPrice(ticker, common.ExchangeAster, common.MarketTypeFuture)
		calc.UpdatePrice(price)
	})

	// 订阅合约流
	futuresStreams := aster.BuildBookTickerStreams(futuresSymbols)
	if err := wsFutures.Subscribe(futuresStreams); err != nil {
		return err
	}

	return nil
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
