package lighter

import (
	"log"
)

const (
	// LighterAPIBaseURL Lighter官方API base URL
	LighterAPIBaseURL = "https://mainnet.zklighter.elliot.ai"
)

// GetCommonMarkets 从Lighter官方API获取市场配置
//
// 自动从API获取所有active市场，无需手动配置
func GetCommonMarkets() []*Market {
	// 尝试从API获取
	markets, err := FetchMarketsFromAPI(LighterAPIBaseURL + "/api/v1/orderBookDetails")
	if err != nil {
		log.Printf("Failed to fetch markets from API: %v, using fallback", err)
		// API失败时使用fallback配置
		return getFallbackMarkets()
	}

	if len(markets) == 0 {
		log.Println("No markets returned from API, using fallback")
		return getFallbackMarkets()
	}

	return markets
}

// getFallbackMarkets 获取fallback市场配置（仅在API失败时使用）
func getFallbackMarkets() []*Market {
	return []*Market{
		{MarketID: 0, Symbol: "ETHUSDT", Type: "perp"},
		{MarketID: 1, Symbol: "BTCUSDT", Type: "perp"},
		{MarketID: 2, Symbol: "SOLUSDT", Type: "perp"},
	}
}

// GetMarketIDs 获取所有市场ID列表
func GetMarketIDs(markets []*Market) []int {
	ids := make([]int, len(markets))
	for i, m := range markets {
		ids[i] = m.MarketID
	}
	return ids
}
