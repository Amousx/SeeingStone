package main

import (
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/arbitrage"
	"crypto-arbitrage-monitor/internal/exchange/aster"
	"crypto-arbitrage-monitor/internal/exchange/lighter"
	"crypto-arbitrage-monitor/pkg/common"
	"log"
	"strconv"
	"time"
)

func main() {
	log.Println("=== 测试数据流水线 ===")

	// 1. 创建 Calculator
	calc := arbitrage.NewCalculator(0.1)
	log.Println("\n✓ Step 1: Calculator 已创建")

	// 2. 测试 Aster 数据
	log.Println("\n--- Step 2: 测试 Aster 数据源 ---")
	cfg := config.LoadConfig()
	spotClient := aster.NewSpotClient(cfg.AsterSpotBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	futuresClient := aster.NewFuturesClient(cfg.AsterFutureBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)

	// 获取 ETH 现货价格
	spotTickers, err := spotClient.GetAllBookTickers()
	if err == nil {
		for _, ticker := range spotTickers {
			if ticker.Symbol == "ETHUSDT" {
				tickers24h, _ := spotClient.GetAll24hrTickers()
				volumeMap := make(map[string]float64)
				for _, t := range tickers24h {
					volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
				}
				price := spotClient.ConvertToCommonPrice(&ticker, volumeMap["ETHUSDT"])
				calc.UpdatePrice(price)
				log.Printf("  Aster Spot ETH: %.2f (bid=%.2f, ask=%.2f)", price.Price, price.BidPrice, price.AskPrice)
				break
			}
		}
	}

	// 获取 ETH 合约价格
	futureTickers, err := futuresClient.GetAllBookTickers()
	if err == nil {
		for _, ticker := range futureTickers {
			if ticker.Symbol == "ETHUSDT" {
				tickers24h, _ := futuresClient.GetAll24hrTickers()
				volumeMap := make(map[string]float64)
				for _, t := range tickers24h {
					volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
				}
				price := futuresClient.ConvertToCommonPrice(&ticker, volumeMap["ETHUSDT"])
				calc.UpdatePrice(price)
				log.Printf("  Aster Future ETH: %.2f (bid=%.2f, ask=%.2f)", price.Price, price.BidPrice, price.AskPrice)
				break
			}
		}
	}

	log.Printf("\n  Calculator 中的价格数量: %d", calc.GetPriceCount())

	// 3. 测试 Lighter 数据
	log.Println("\n--- Step 3: 测试 Lighter 数据源 ---")
	lighterMarkets := lighter.GetCommonMarkets()
	log.Printf("  Lighter 市场总数: %d", len(lighterMarkets))

	// 检查是否有 ETHUSDT
	hasETH := false
	for _, m := range lighterMarkets {
		if m.Symbol == "ETHUSDT" {
			hasETH = true
			log.Printf("  ✓ 找到 ETHUSDT (Market ID: %d)", m.MarketID)
			break
		}
	}
	if !hasETH {
		log.Println("  ✗ 未找到 ETHUSDT 市场")
	}

	// 连接 Lighter WebSocket
	wsClient := lighter.NewWSClient(
		"wss://mainnet.zklighter.elliot.ai/stream",
		lighterMarkets,
		lighter.LighterAPIBaseURL,
		0,
	)

	ethReceived := false
	wsClient.SetMessageHandler(func(price *common.Price) {
		if price.Symbol == "ETHUSDT" && !ethReceived {
			ethReceived = true
			log.Printf("  ✓ Lighter Future ETH: %.2f (bid=%.2f, ask=%.2f)", price.Price, price.BidPrice, price.AskPrice)
			calc.UpdatePrice(price)
		}
	})

	if err := wsClient.Connect(); err != nil {
		log.Fatal(err)
	}

	marketIDs := lighter.GetMarketIDs(lighterMarkets)
	if err := wsClient.Subscribe(marketIDs); err != nil {
		log.Fatal(err)
	}

	log.Println("  等待 5 秒接收 Lighter WebSocket 数据...")
	time.Sleep(5 * time.Second)

	if !ethReceived {
		log.Println("  ✗ WebSocket 未收到 Lighter ETHUSDT 数据")
	}

	// 使用 REST API 获取数据
	log.Println("\n  尝试 REST API 获取 Lighter 数据...")
	prices, err := lighter.FetchMarketData(lighter.LighterAPIBaseURL, marketIDs)
	if err == nil {
		for _, price := range prices {
			if price.Symbol == "ETHUSDT" {
				log.Printf("  ✓ REST API 获取 ETH: %.2f", price.Price)
				calc.UpdatePrice(price)
			}
		}
		log.Printf("  REST API 共获取 %d 个市场价格", len(prices))
		// 更新所有价格到 Calculator
		for _, price := range prices {
			calc.UpdatePrice(price)
		}
	} else {
		log.Printf("  ✗ REST API 失败: %v", err)
	}

	log.Printf("\n  Calculator 中的价格数量: %d", calc.GetPriceCount())

	// 4. 查看 Calculator 中的所有价格
	log.Println("\n--- Step 4: Calculator 中的所有价格 ---")
	allPrices := calc.GetAllPrices()
	for _, p := range allPrices {
		if p.Symbol == "ETHUSDT" {
			log.Printf("  %s %s %s: %.2f", p.Symbol, p.Exchange, p.MarketType, p.Price)
		}
	}

	// 5. 执行套利计算
	log.Println("\n--- Step 5: 执行套利计算 ---")
	calc.CalculateArbitrage()
	opportunities := calc.GetOpportunities()
	log.Printf("  总套利机会数: %d", len(opportunities))

	// 查找 ETH 相关的套利机会
	ethOpps := 0
	for _, opp := range opportunities {
		if opp.Symbol == "ETHUSDT" {
			ethOpps++
			if ethOpps <= 3 {
				log.Printf("  %s: %s %s %s -> %s %s %s (%.2f%%)",
					opp.Symbol, opp.Exchange1, opp.Market1Type, opp.Type, opp.Exchange2, opp.Market2Type, opp.SpreadPercent)
			}
		}
	}
	log.Printf("  ETH 套利机会数: %d", ethOpps)

	// 6. 具体检查是否有 Aster Spot -> Lighter Future
	log.Println("\n--- Step 6: 检查特定组合 ---")
	found := false
	for _, opp := range opportunities {
		if opp.Symbol == "ETHUSDT" &&
			opp.Exchange1 == common.ExchangeAster && opp.Market1Type == common.MarketTypeSpot &&
			opp.Exchange2 == common.ExchangeLighter && opp.Market2Type == common.MarketTypeFuture {
			found = true
			log.Printf("  ✓ 找到: ASTER SPOT -> LIGHTER FUTURE (%.2f%%)", opp.SpreadPercent)
			break
		}
		if opp.Symbol == "ETHUSDT" &&
			opp.Exchange1 == common.ExchangeLighter && opp.Market1Type == common.MarketTypeFuture &&
			opp.Exchange2 == common.ExchangeAster && opp.Market2Type == common.MarketTypeSpot {
			found = true
			log.Printf("  ✓ 找到: LIGHTER FUTURE -> ASTER SPOT (%.2f%%)", opp.SpreadPercent)
			break
		}
	}

	if !found {
		log.Println("  ✗ 未找到 Aster Spot <-> Lighter Future 组合")
	}

	log.Println("\n=== 测试完成 ===")
	wsClient.Close()
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
