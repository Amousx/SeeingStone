package main

import (
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/arbitrage"
	"crypto-arbitrage-monitor/internal/exchange/aster"
	"fmt"
	"log"
	"strconv"
)

func main() {
	cfg := config.LoadConfig()

	// 创建计算器
	calc := arbitrage.NewCalculator(0.0)

	// 创建客户端
	spotClient := aster.NewSpotClient(cfg.AsterSpotBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	futuresClient := aster.NewFuturesClient(cfg.AsterFutureBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)

	fmt.Println("Loading prices...")
	initPrices(spotClient, futuresClient, calc)

	// 计算套利机会
	fmt.Println("Calculating arbitrage opportunities...\n")
	calc.CalculateArbitrage()
	opportunities := calc.GetOpportunities()

	// 按类型分组统计
	typeCount := make(map[string]int)
	for _, opp := range opportunities {
		typeCount[opp.Type]++
	}

	fmt.Printf("Total opportunities: %d\n\n", len(opportunities))
	fmt.Println("Breakdown by type:")
	fmt.Println("==================")
	for typeName, count := range typeCount {
		fmt.Printf("%-15s: %d\n", typeName, count)
	}

	fmt.Println("\nSample opportunities by type:")
	fmt.Println("=====================================")

	// 显示每种类型的示例
	shown := make(map[string]bool)
	for _, opp := range opportunities {
		if !shown[opp.Type] {
			fmt.Printf("\n%s:\n", opp.Type)
			fmt.Printf("  %s: Buy from %s %s @%.4f → Sell to %s %s @%.4f (%.2f%%)\n",
				opp.Symbol,
				opp.Exchange1, opp.Market1Type, opp.Price1,
				opp.Exchange2, opp.Market2Type, opp.Price2,
				opp.SpreadPercent)
			shown[opp.Type] = true

			if len(shown) == 4 {
				break
			}
		}
	}
}

func initPrices(spotClient *aster.SpotClient, futuresClient *aster.FuturesClient, calc *arbitrage.Calculator) {
	// Spot prices
	spotTickers, err := spotClient.GetAllBookTickers()
	if err != nil {
		log.Printf("Failed to fetch spot tickers: %v", err)
		return
	}

	spotTickers24h, err := spotClient.GetAll24hrTickers()
	if err != nil {
		log.Printf("Failed to fetch spot 24h tickers: %v", err)
		return
	}

	volumeMap := make(map[string]float64)
	for _, t := range spotTickers24h {
		volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
	}

	for _, ticker := range spotTickers {
		volume := volumeMap[ticker.Symbol]
		price := spotClient.ConvertToCommonPrice(&ticker, volume)
		calc.UpdatePrice(price)
	}

	// Futures prices
	futuresTickers, err := futuresClient.GetAllBookTickers()
	if err != nil {
		log.Printf("Failed to fetch futures tickers: %v", err)
		return
	}

	futuresTickers24h, err := futuresClient.GetAll24hrTickers()
	if err != nil {
		log.Printf("Failed to fetch futures 24h tickers: %v", err)
		return
	}

	volumeMap = make(map[string]float64)
	for _, t := range futuresTickers24h {
		volumeMap[t.Symbol] = parseFloat(t.QuoteVolume)
	}

	for _, ticker := range futuresTickers {
		volume := volumeMap[ticker.Symbol]
		price := futuresClient.ConvertToCommonPrice(&ticker, volume)
		calc.UpdatePrice(price)
	}
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
