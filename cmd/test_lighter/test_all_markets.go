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

	fmt.Println("测试前15个市场...")

	// 订阅前15个市场
	for i := 0; i <= 14; i++ {
		sub := map[string]interface{}{
			"type":    "subscribe",
			"channel": fmt.Sprintf("market_stats/%d", i),
		}
		conn.WriteJSON(sub)
	}

	marketData := make(map[int]*MarketStatsData)
	timeout := time.After(4 * time.Second)
	
	for {
		select {
		case <-timeout:
			fmt.Println("\n=== 收集到的市场数据 ===\n")
			for i := 0; i <= 14; i++ {
				if data, ok := marketData[i]; ok {
					fmt.Printf("Market %2d: mark_price=%10s  (可能的币种需要手动判断)\n",
						data.MarketID, data.MarkPrice)
				}
			}
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			var update MarketStatsUpdate
			if err := json.Unmarshal(message, &update); err == nil {
				if update.Type == "update/market_stats" {
					marketData[update.MarketStats.MarketID] = &update.MarketStats
				}
			}
		}
	}
}
