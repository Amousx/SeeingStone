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

	fmt.Println("已连接到 Lighter WebSocket")

	// 订阅前20个市场的market_stats
	markets := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20}
	for _, marketID := range markets {
		sub := map[string]interface{}{
			"type":    "subscribe",
			"channel": fmt.Sprintf("market_stats/%d", marketID),
		}
		if err := conn.WriteJSON(sub); err != nil {
			log.Println("订阅失败:", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	fmt.Println("已订阅市场，等待数据...\n")

	// 收集数据
	timeout := time.After(5 * time.Second)
	marketData := make(map[int]*MarketStatsData)

	for {
		select {
		case <-timeout:
			// 打印收集到的数据
			fmt.Println("\n=== 收集到的市场数据 ===")
			fmt.Printf("共收集到 %d 个市场\n\n", len(marketData))
			for i := 0; i <= 20; i++ {
				if data, ok := marketData[i]; ok {
					fmt.Printf("Market %2d: mark=%12s  index=%12s  last_trade=%12s  volume=%.2f\n",
						data.MarketID, data.MarkPrice, data.IndexPrice, data.LastTradePrice, data.DailyQuoteTokenVolume)
				}
			}
			return

		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				log.Println("读取消息失败:", err)
				return
			}

			var baseMsg struct {
				Type string `json:"type"`
			}
			if err := json.Unmarshal(message, &baseMsg); err != nil {
				continue
			}

			if baseMsg.Type == "update/market_stats" {
				var update MarketStatsUpdate
				if err := json.Unmarshal(message, &update); err != nil {
					log.Println("解析失败:", err)
					continue
				}
				marketData[update.MarketStats.MarketID] = &update.MarketStats
			}
		}
	}
}
