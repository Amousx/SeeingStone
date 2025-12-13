package main

import (
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/arbitrage"
	"crypto-arbitrage-monitor/internal/exchange/aster"
	"fmt"
	"log"
)

func main() {
	cfg := config.LoadConfig()

	// 创建客户端
	spotClient := aster.NewSpotClient(cfg.AsterSpotBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	futuresClient := aster.NewFuturesClient(cfg.AsterFutureBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)

	// 创建计算器（minSpread设置为0，显示所有价差）
	calc := arbitrage.NewCalculator(0.0)

	fmt.Println("Fetching spot prices...")
	spotTickers, err := spotClient.GetAllBookTickers()
	if err != nil {
		log.Fatal(err)
	}

	spotTickers24h, err := spotClient.GetAll24hrTickers()
	if err != nil {
		log.Fatal(err)
	}

	volumeMap := make(map[string]float64)
	for _, t := range spotTickers24h {
		vol := 0.0
		fmt.Sscanf(t.QuoteVolume, "%f", &vol)
		volumeMap[t.Symbol] = vol
	}

	for _, ticker := range spotTickers {
		volume := volumeMap[ticker.Symbol]
		price := spotClient.ConvertToCommonPrice(&ticker, volume)
		calc.UpdatePrice(price)
	}

	fmt.Printf("Loaded %d spot prices\n", len(spotTickers))

	fmt.Println("Fetching futures prices...")
	futuresTickers, err := futuresClient.GetAllBookTickers()
	if err != nil {
		log.Fatal(err)
	}

	futuresTickers24h, err := futuresClient.GetAll24hrTickers()
	if err != nil {
		log.Fatal(err)
	}

	volumeMap = make(map[string]float64)
	for _, t := range futuresTickers24h {
		vol := 0.0
		fmt.Sscanf(t.QuoteVolume, "%f", &vol)
		volumeMap[t.Symbol] = vol
	}

	for _, ticker := range futuresTickers {
		volume := volumeMap[ticker.Symbol]
		price := futuresClient.ConvertToCommonPrice(&ticker, volume)
		calc.UpdatePrice(price)
	}

	fmt.Printf("Loaded %d futures prices\n", len(futuresTickers))

	// 计算套利机会
	fmt.Println("\nCalculating arbitrage opportunities...")
	calc.CalculateArbitrage()
	opportunities := calc.GetOpportunities()

	fmt.Printf("Found %d arbitrage opportunities\n\n", len(opportunities))

	// 排序并显示前10个
	sorted := arbitrage.SortOpportunities(opportunities, "spread", true)

	fmt.Println("Top 10 arbitrage opportunities:")
	fmt.Println("================================================================================")
	fmt.Printf("%-12s %-15s %-25s %-25s %10s\n", "Symbol", "Type", "Buy From", "Sell To", "Spread %")
	fmt.Println("================================================================================")

	count := 10
	if len(sorted) < 10 {
		count = len(sorted)
	}

	for i := 0; i < count; i++ {
		opp := sorted[i]
		buyFrom := fmt.Sprintf("↓ %s %s @%.2f", opp.Exchange1, opp.Market1Type, opp.Price1)
		sellTo := fmt.Sprintf("↑ %s %s @%.2f", opp.Exchange2, opp.Market2Type, opp.Price2)
		fmt.Printf("%-12s %-15s %-25s %-25s %10.2f%%\n",
			opp.Symbol,
			opp.Type,
			buyFrom,
			sellTo,
			opp.SpreadPercent)
	}

	fmt.Println("\nPrice count in calculator:", calc.GetPriceCount())
}
