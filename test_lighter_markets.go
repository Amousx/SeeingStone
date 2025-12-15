package main

import (
	"crypto-arbitrage-monitor/internal/exchange/lighter"
	"crypto-arbitrage-monitor/pkg/common"
	"log"
	"sync"
	"time"
)

func main() {
	log.Println("=== 测试 Lighter 各个市场的数据接收情况 ===")

	markets := lighter.GetCommonMarkets()
	log.Printf("总市场数: %d\n", len(markets))

	wsClient := lighter.NewWSClient(
		"wss://mainnet.zklighter.elliot.ai/stream",
		markets,
		lighter.LighterAPIBaseURL,
		0,
	)

	// 记录收到数据的市场
	received := make(map[string]int)
	var mu sync.Mutex

	wsClient.SetMessageHandler(func(price *common.Price) {
		mu.Lock()
		received[price.Symbol]++
		mu.Unlock()
	})

	if err := wsClient.Connect(); err != nil {
		log.Fatal(err)
	}

	marketIDs := lighter.GetMarketIDs(markets)
	if err := wsClient.Subscribe(marketIDs); err != nil {
		log.Fatal(err)
	}

	log.Println("等待 10 秒接收数据...")
	time.Sleep(10 * time.Second)

	log.Println("\n收到数据的市场:")
	mu.Lock()
	count := 0
	// 检查关键市场
	for _, symbol := range []string{"ETHUSDT", "BTCUSDT", "SOLUSDT", "BNBUSDT", "ADAUSDT"} {
		if n, ok := received[symbol]; ok {
			log.Printf("  ✓ %s: %d 次更新", symbol, n)
			count++
		} else {
			log.Printf("  ✗ %s: 0 次更新", symbol)
		}
	}

	log.Printf("\n总共收到数据的市场数: %d / %d", len(received), len(markets))
	log.Printf("前10个有数据的市场:")
	i := 0
	for symbol, count := range received {
		if i >= 10 {
			break
		}
		log.Printf("  %s: %d 次更新", symbol, count)
		i++
	}
	mu.Unlock()

	wsClient.Close()
}
