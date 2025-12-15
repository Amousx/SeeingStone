package main

import (
	"crypto-arbitrage-monitor/internal/exchange/lighter"
	"log"
)

func main() {
	log.Println("=== 测试 Lighter REST API ===")

	markets := lighter.GetCommonMarkets()
	marketIDs := lighter.GetMarketIDs(markets)

	log.Printf("获取 %d 个市场的数据...\n", len(marketIDs))

	prices, err := lighter.FetchMarketData(lighter.LighterAPIBaseURL, marketIDs)
	if err != nil {
		log.Fatalf("Failed: %v", err)
	}

	log.Printf("✓ 成功获取 %d 个市场的价格\n", len(prices))

	// 显示关键币种
	log.Println("\n关键币种价格:")
	for _, price := range prices {
		if price.Symbol == "ETHUSDT" || price.Symbol == "BTCUSDT" || price.Symbol == "SOLUSDT" {
			log.Printf("  %s: %.2f (bid=%.2f, ask=%.2f, volume=%.0f)",
				price.Symbol, price.Price, price.BidPrice, price.AskPrice, price.Volume24h)
		}
	}
}
