package lighter

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// APIMarketDetail Lighter API返回的市场详情
type APIMarketDetail struct {
	Symbol   string `json:"symbol"`
	MarketID int    `json:"market_id"`
	Status   string `json:"status"`
}

// APIResponse Lighter API响应
type APIResponse struct {
	Code                 int               `json:"code"`
	OrderBookDetails     []APIMarketDetail `json:"order_book_details"`
	SpotOrderBookDetails []APIMarketDetail `json:"spot_order_book_details"`
}

// FetchMarketsFromAPI 从Lighter官方API获取市场配置
func FetchMarketsFromAPI(apiURL string) ([]*Market, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get(apiURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch markets from API: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned non-200 status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	var apiResp APIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %v", err)
	}

	if apiResp.Code != 200 {
		return nil, fmt.Errorf("API returned error code: %d", apiResp.Code)
	}

	// 转换为内部Market结构
	markets := make([]*Market, 0)

	// 处理futures市场
	for _, detail := range apiResp.OrderBookDetails {
		// 只添加active状态的市场
		if detail.Status == "active" {
			markets = append(markets, &Market{
				MarketID: detail.MarketID,
				Symbol:   detail.Symbol + "USDT", // Lighter futures的symbol不带USDT后缀，需要加上（例如 "PYTH" -> "PYTHUSDT"）
				Type:     "perp",
			})
		}
	}

	// 处理spot市场
	for _, detail := range apiResp.SpotOrderBookDetails {
		// 只添加active状态的市场
		if detail.Status == "active" {
			// Spot市场symbol格式为 "LIT/USDC"，需要将斜杠去掉（例如 "LIT/USDC" -> "LITUSDC"）
			symbol := strings.ReplaceAll(detail.Symbol, "/", "")
			markets = append(markets, &Market{
				MarketID: detail.MarketID,
				Symbol:   symbol,
				Type:     "spot",
			})
		}
	}

	log.Printf("Fetched %d active markets from Lighter API (%d futures, %d spot)",
		len(markets), len(apiResp.OrderBookDetails), len(apiResp.SpotOrderBookDetails))
	return markets, nil
}
