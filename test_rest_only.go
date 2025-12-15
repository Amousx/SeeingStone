package main

import (
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/arbitrage"
	"crypto-arbitrage-monitor/internal/exchange/aster"
	"crypto-arbitrage-monitor/internal/exchange/lighter"
	"log"
	"strconv"
)

func main() {
	log.Println("=== 测试纯 REST API 模式（无 WebSocket）===")

	// 创建 Calculator
	calc := arbitrage.NewCalculator(0.1)

	// 1. 测试 Aster 数据
	log.Println("\n--- Step 1: 获取 Aster 价格 (REST API) ---")
	cfg := config.LoadConfig()
	spotClient := aster.NewSpotClient(cfg.AsterSpotBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	futuresClient := aster.NewFuturesClient(cfg.AsterFutureBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)

	// 获取 ETH 现货价格
	spotTickers, err := spotClient.GetAllBookTickers()
	if err == nil {
		tickers24h, _ := spotClient.GetAll24hrTickers()
		volumeMap := make(map[string]float64)
		for _, t := range tickers24h {
			volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
		}

		for _, ticker := range spotTickers {
			if ticker.Symbol == "ETHUSDT" {
				price := spotClient.ConvertToCommonPrice(&ticker, volumeMap["ETHUSDT"])
				calc.UpdatePrice(price)
				log.Printf("  ✓ Aster Spot ETH: %.2f (bid=%.2f, ask=%.2f)", price.Price, price.BidPrice, price.AskPrice)
				break
			}
		}
	}

	// 获取 ETH 合约价格
	futureTickers, err := futuresClient.GetAllBookTickers()
	if err == nil {
		tickers24h, _ := futuresClient.GetAll24hrTickers()
		volumeMap := make(map[string]float64)
		for _, t := range tickers24h {
			volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
		}

		for _, ticker := range futureTickers {
			if ticker.Symbol == "ETHUSDT" {
				price := futuresClient.ConvertToCommonPrice(&ticker, volumeMap["ETHUSDT"])
				calc.UpdatePrice(price)
				log.Printf("  ✓ Aster Future ETH: %.2f (bid=%.2f, ask=%.2f)", price.Price, price.BidPrice, price.AskPrice)
				break
			}
		}
	}

	// 2. 测试 Lighter 数据 (REST API)
	log.Println("\n--- Step 2: 获取 Lighter 价格 (REST API) ---")
	lighterMarkets := lighter.GetCommonMarkets()
	marketIDs := lighter.GetMarketIDs(lighterMarkets)

	prices, err := lighter.FetchMarketData(lighter.LighterAPIBaseURL, marketIDs)
	if err == nil {
		for _, price := range prices {
			calc.UpdatePrice(price)
			if price.Symbol == "ETHUSDT" {
				log.Printf("  ✓ Lighter Future ETH: %.2f (bid=%.2f, ask=%.2f)", price.Price, price.BidPrice, price.AskPrice)
			}
		}
		log.Printf("  总共加载 %d 个 Lighter 市场价格", len(prices))
	} else {
		log.Printf("  ✗ Lighter REST API 失败: %v", err)
	}

	log.Printf("\n  Calculator 中的价格数量: %d", calc.GetPriceCount())

	// 3. 查看所有 ETH 价格
	log.Println("\n--- Step 3: 所有 ETH 价格 ---")
	allPrices := calc.GetAllPrices()
	for _, p := range allPrices {
		if p.Symbol == "ETHUSDT" {
			log.Printf("  %s %s %s: %.2f (bid=%.2f, ask=%.2f)", p.Symbol, p.Exchange, p.MarketType, p.Price, p.BidPrice, p.AskPrice)
		}
	}

	// 4. 执行套利计算
	log.Println("\n--- Step 4: 执行套利计算 ---")
	calc.CalculateArbitrage()
	opportunities := calc.GetOpportunities()
	log.Printf("  总套利机会数: %d", len(opportunities))

	// 查找 ETH 相关的套利机会
	ethOpps := 0
	for _, opp := range opportunities {
		if opp.Symbol == "ETHUSDT" {
			ethOpps++
			log.Printf("  %s: %s %s -> %s %s (%.2f%%)",
				opp.Symbol,
				opp.Exchange1, opp.Market1Type,
				opp.Exchange2, opp.Market2Type,
				opp.SpreadPercent)
		}
	}
	log.Printf("  ETH 套利机会数: %d", ethOpps)

	// 5. 检查特定组合
	log.Println("\n--- Step 5: 检查 Aster Spot <-> Lighter Future 组合 ---")
	found := false
	for _, opp := range opportunities {
		if opp.Symbol == "ETHUSDT" &&
			((string(opp.Exchange1) == "ASTER" && string(opp.Market1Type) == "SPOT" &&
				string(opp.Exchange2) == "LIGHTER" && string(opp.Market2Type) == "FUTURE") ||
				(string(opp.Exchange1) == "LIGHTER" && string(opp.Market1Type) == "FUTURE" &&
					string(opp.Exchange2) == "ASTER" && string(opp.Market2Type) == "SPOT")) {
			found = true
			log.Printf("  ✓ 找到: %s %s -> %s %s (%.2f%%)",
				opp.Exchange1, opp.Market1Type,
				opp.Exchange2, opp.Market2Type,
				opp.SpreadPercent)
		}
	}

	if !found {
		log.Println("  ✗ 未找到 Aster Spot <-> Lighter Future 组合")
	}

	log.Println("\n=== 测试完成 ===")
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}
