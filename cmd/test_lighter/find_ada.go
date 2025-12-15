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
}

func main() {
	url := "wss://mainnet.zklighter.elliot.ai/stream"
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		log.Fatal("连接失败:", err)
	}
	defer conn.Close()

	fmt.Println("搜索ADA市场 (价格应该在 $0.40 左右)...\n")

	// 订阅前20个市场
	for i := 0; i <= 19; i++ {
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
			fmt.Println("=== 所有市场价格 ===\n")
			for i := 0; i <= 19; i++ {
				if data, ok := marketData[i]; ok {
					fmt.Printf("Market %2d: mark=%10s  ", i, data.MarkPrice)
					
					// 标注可能的币种
					mark := data.MarkPrice
					if mark >= "3000" && mark <= "3200" {
						fmt.Printf("← 可能是 ETH")
					} else if mark >= "89000" && mark <= "92000" {
						fmt.Printf("← 可能是 BTC")
					} else if mark >= "0.38" && mark <= "0.42" {
						fmt.Printf("← 可能是 ADA")
					}
					fmt.Println()
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
