package main

import (
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/arbitrage"
	"crypto-arbitrage-monitor/internal/exchange/aster"
	"crypto-arbitrage-monitor/internal/exchange/lighter"
	"crypto-arbitrage-monitor/internal/notification"
	"crypto-arbitrage-monitor/internal/ui"
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

	// 获取所有可用的交易对
	log.Println("Fetching available symbols...")
	allSymbols := getAllSymbols(spotClient, futuresClient)
	log.Printf("Found %d symbols to monitor", len(allSymbols))

	// 初始化价格数据（使用REST API）
	log.Println("Fetching initial Aster prices...")
	initPrices(spotClient, futuresClient, calc, allSymbols)

	// 获取Lighter市场配置（从官方API）
	log.Println("Fetching Lighter markets from API...")
	lighterMarkets := lighter.GetCommonMarkets()
	lighterAPIBaseURL := lighter.LighterAPIBaseURL
	log.Printf("Found %d Lighter markets to monitor", len(lighterMarkets))

	// 初始化 Lighter 价格数据
	log.Println("Fetching initial Lighter prices...")
	marketIDs := lighter.GetMarketIDs(lighterMarkets)
	if prices, err := lighter.FetchMarketData(lighterAPIBaseURL, marketIDs); err == nil {
		for _, price := range prices {
			calc.UpdatePrice(price)
		}
		log.Printf("Loaded %d Lighter prices from REST API", len(prices))
	} else {
		log.Printf("Warning: Failed to fetch initial Lighter prices: %v", err)
	}

	// 创建UI模型（传入calculator引用）
	model := ui.NewModel(calc)
	p := tea.NewProgram(model, tea.WithAltScreen())

	// 启动价差计算和通知协程
	var wg sync.WaitGroup
	wg.Add(4)

	// 协程1: 饱和式刷新 Aster 价格（每5秒）
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			initPrices(spotClient, futuresClient, calc, allSymbols)
		}
	}()

	// 协程2: 饱和式刷新 Lighter 价格（每5秒）
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			if prices, err := lighter.FetchMarketData(lighterAPIBaseURL, marketIDs); err == nil {
				for _, price := range prices {
					calc.UpdatePrice(price)
				}
			} else {
				log.Printf("Failed to fetch Lighter REST API: %v", err)
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

	// 运行UI
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running UI: %v", err)
	}

	// 清理
	log.Println("Shutting down...")
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

