package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type MarketStatsUpdate struct {
	Channel     string          `json:"channel"`
	MarketStats MarketStatsData `json:"market_stats"`
	Type        string          `json:"type"`
}

type MarketStatsData struct {
	MarketID               int     `json:"market_id"`
	IndexPrice             string  `json:"index_price"`
	MarkPrice              string  `json:"mark_price"`
	LastTradePrice         string  `json:"last_trade_price"`
	DailyQuoteTokenVolume  float64 `json:"daily_quote_token_volume"`
}

func main() {
	url := "wss://mainnet.zklighter.elliot.ai/stream"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("连接失败:", err)
	}
	defer conn.Close()

	fmt.Println("检查Market 5是否是ADA...\n")

	// 订阅market 5
	sub := map[string]interface{}{
		"type":    "subscribe",
		"channel": "market_stats/5",
	}
	if err := conn.WriteJSON(sub); err != nil {
		log.Fatal("订阅失败:", err)
	}

	timeout := time.After(3 * time.Second)
	for {
		select {
		case <-timeout:
			fmt.Println("未收到数据")
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var update MarketStatsUpdate
			if err := json.Unmarshal(message, &update); err == nil {
				if update.Type == "update/market_stats" {
					fmt.Printf("Market %d 数据:\n", update.MarketStats.MarketID)
					fmt.Printf("  mark_price: %s\n", update.MarketStats.MarkPrice)
					fmt.Printf("  index_price: %s\n", update.MarketStats.IndexPrice)
					fmt.Printf("  last_trade_price: %s\n", update.MarketStats.LastTradePrice)
					fmt.Printf("  daily_volume: %.2f USDC\n\n", update.MarketStats.DailyQuoteTokenVolume)
					fmt.Println("如果这是ADA，价格应该在 $0.39-0.41 范围内")
					fmt.Println("请对比 https://app.lighter.xyz/trade/ADA 的实际价格")
					return
				}
			}
		}
	}
}
