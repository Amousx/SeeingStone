package main

import (
	"crypto-arbitrage-monitor/config"
	"crypto-arbitrage-monitor/internal/arbitrage"
	"crypto-arbitrage-monitor/internal/exchange/aster"
	"fmt"
	"log"
	"strconv"
	"time"
)

func main() {
	cfg := config.LoadConfig()

	// 创建计算器（设置为0显示所有价差）
	calc := arbitrage.NewCalculator(0.0)

	// 创建客户端
	spotClient := aster.NewSpotClient(cfg.AsterSpotBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)
	futuresClient := aster.NewFuturesClient(cfg.AsterFutureBaseURL, cfg.AsterAPIKey, cfg.AsterSecretKey)

	fmt.Println("Initializing prices...")
	initPrices(spotClient, futuresClient, calc)

	// 每秒计算并打印一次
	for i := 0; i < 5; i++ {
		fmt.Printf("\n===== Iteration %d =====\n", i+1)

		calc.CalculateArbitrage()
		opportunities := calc.GetOpportunities()

		fmt.Printf("Total opportunities: %d\n", len(opportunities))

		if len(opportunities) > 0 {
			sorted := arbitrage.SortOpportunities(opportunities, "spread", true)
			fmt.Println("\nTop 5:")
			count := 5
			if len(sorted) < 5 {
				count = len(sorted)
			}
			for j := 0; j < count; j++ {
				opp := sorted[j]
				fmt.Printf("  %s: %.2f%% (%s %s -> %s %s)\n",
					opp.Symbol,
					opp.SpreadPercent,
					opp.Exchange1, opp.Market1Type,
					opp.Exchange2, opp.Market2Type)
			}
		}

		time.Sleep(1 * time.Second)
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

	fmt.Printf("Loaded %d spot prices\n", len(spotTickers))

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

	fmt.Printf("Loaded %d futures prices\n", len(futuresTickers))
}

func parseFloat(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func getAllSymbols(spotClient *aster.SpotClient, futuresClient *aster.FuturesClient) []string {
	symbolMap := make(map[string]bool)

	spotInfo, err := spotClient.GetExchangeInfo()
	if err == nil {
		for _, symbol := range spotInfo.Symbols {
			if symbol.Status == "TRADING" {
				symbolMap[symbol.Symbol] = true
			}
		}
	}

	futuresInfo, err := futuresClient.GetExchangeInfo()
	if err == nil {
		for _, symbol := range futuresInfo.Symbols {
			if symbol.Status == "TRADING" {
				symbolMap[symbol.Symbol] = true
			}
		}
	}

	symbols := make([]string, 0, len(symbolMap))
	for symbol := range symbolMap {
		symbols = append(symbols, symbol)
	}

	return symbols
}
