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

	fmt.Println("测试Market 0 (应该是ETH)...")

	// 只订阅market 0
	sub := map[string]interface{}{
		"type":    "subscribe",
		"channel": "market_stats/0",
	}
	if err := conn.WriteJSON(sub); err != nil {
		log.Fatal("订阅失败:", err)
	}

	// 等待3秒收集数据
	timeout := time.After(3 * time.Second)
	for {
		select {
		case <-timeout:
			fmt.Println("未收到数据")
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("读取失败:", err)
				return
			}

			var update MarketStatsUpdate
			if err := json.Unmarshal(message, &update); err == nil {
				if update.Type == "update/market_stats" {
					fmt.Printf("\n收到Market %d数据:\n", update.MarketStats.MarketID)
					fmt.Printf("  mark_price: %s\n", update.MarketStats.MarkPrice)
					fmt.Printf("  index_price: %s\n", update.MarketStats.IndexPrice)
					fmt.Printf("  last_trade_price: %s\n", update.MarketStats.LastTradePrice)
					return
				}
			}
		}
	}
}
