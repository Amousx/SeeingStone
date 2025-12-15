package main

import (
	"crypto-arbitrage-monitor/internal/exchange/lighter"
	"log"
	"time"
)

func main() {
	log.Println("=== 测试 Lighter API 韧性机制 ===")

	markets := lighter.GetCommonMarkets()
	marketIDs := lighter.GetMarketIDs(markets)

	log.Printf("监控 %d 个市场\n", len(markets))

	// 连续请求 5 次，观察统计信息和缓存行为
	for i := 1; i <= 5; i++ {
		log.Printf("\n--- 第 %d 次请求 ---", i)

		startTime := time.Now()
		prices, err := lighter.FetchMarketData(lighter.LighterAPIBaseURL, marketIDs)
		elapsed := time.Since(startTime)

		if err != nil {
			log.Printf("✗ 请求失败: %v (耗时: %v)", err, elapsed)
		} else {
			log.Printf("✓ 请求成功: 获取 %d 个价格 (耗时: %v)", len(prices), elapsed)

			// 检查 ETH 数据
			for _, price := range prices {
				if price.Symbol == "ETHUSDT" {
					log.Printf("  ETH 价格: %.2f (更新时间: %v)",
						price.Price, time.Since(price.LastUpdated))
					break
				}
			}
		}

		// 每次请求间隔 3 秒
		if i < 5 {
			time.Sleep(3 * time.Second)
		}
	}

	log.Println("\n=== 测试完成 ===")
}
