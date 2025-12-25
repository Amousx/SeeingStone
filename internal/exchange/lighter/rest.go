package lighter

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// OrderBookDetailsResponse REST API 响应
type OrderBookDetailsResponse struct {
	Code              int                     `json:"code"`
	OrderBookDetails  []OrderBookDetailItem   `json:"order_book_details"`
}

// OrderBookDetailItem 订单簿详情
type OrderBookDetailItem struct {
	MarketID              int     `json:"market_id"`
	Symbol                string  `json:"symbol"`
	Status                string  `json:"status"`
	LastTradePrice        float64 `json:"last_trade_price"`
	DailyBaseTokenVolume  float64 `json:"daily_base_token_volume"`
	DailyQuoteTokenVolume float64 `json:"daily_quote_token_volume"`
	DailyPriceLow         float64 `json:"daily_price_low"`
	DailyPriceHigh        float64 `json:"daily_price_high"`
	OpenInterest          float64 `json:"open_interest"`
}

// 价格缓存
var (
	priceCache     = make(map[string]*common.Price)
	priceCacheMu   sync.RWMutex
	lastFetchTime  time.Time
	lastFetchCount int
	fetchErrorCount int
)

// FetchMarketData 从 REST API 获取市场数据（并发多次请求 + 合并结果）
func FetchMarketData(apiURL string, marketIDs []int) ([]*common.Price, error) {
	const parallelRequests = 3 // 并发请求数
	const requestTimeout = 5 * time.Second

	type result struct {
		prices []*common.Price
		err    error
	}

	resultChan := make(chan result, parallelRequests)

	// 并发发起多个请求
	for i := 0; i < parallelRequests; i++ {
		go func(requestID int) {
			prices, err := fetchMarketDataOnce(apiURL, marketIDs)
			resultChan <- result{prices: prices, err: err}
		}(i)
	}

	// 收集结果
	var bestResult *result
	var allErrors []error
	successCount := 0

	// 等待所有请求完成或超时
	timeout := time.After(requestTimeout)
collectResults:
	for i := 0; i < parallelRequests; i++ {
		select {
		case res := <-resultChan:
			if res.err == nil {
				successCount++
				// 选择数据最多的结果
				if bestResult == nil || len(res.prices) > len(bestResult.prices) {
					bestResult = &res
				}
			} else {
				allErrors = append(allErrors, res.err)
			}
		case <-timeout:
			log.Printf("Warning: Some Lighter API requests timed out after %v", requestTimeout)
			break collectResults
		}
	}

	// 如果有成功的请求
	if bestResult != nil {
		lastFetchTime = time.Now()
		lastFetchCount = len(bestResult.prices)

		// 更新缓存
		priceCacheMu.Lock()
		for _, price := range bestResult.prices {
			key := fmt.Sprintf("%s-%s-%s", price.Exchange, price.MarketType, price.Symbol)
			priceCache[key] = price
		}
		priceCacheMu.Unlock()

		// 重置错误计数
		if fetchErrorCount > 0 {
			log.Printf("Lighter API recovered after %d errors", fetchErrorCount)
			fetchErrorCount = 0
		}

		if successCount < parallelRequests {
			log.Printf("Lighter API: %d/%d requests succeeded, using best result with %d prices",
				successCount, parallelRequests, len(bestResult.prices))
		}

		return bestResult.prices, nil
	}

	// 所有请求都失败
	fetchErrorCount++
	log.Printf("Lighter API: all %d parallel requests failed", parallelRequests)
	for i, err := range allErrors {
		log.Printf("  Request %d error: %v", i+1, err)
	}

	// 使用缓存数据
	priceCacheMu.RLock()
	cachedPrices := make([]*common.Price, 0, len(priceCache))
	for _, price := range priceCache {
		// 只返回不超过 5 分钟的缓存
		if time.Since(price.LastUpdated) < 5*time.Minute {
			cachedPrices = append(cachedPrices, price)
		}
	}
	priceCacheMu.RUnlock()

	if len(cachedPrices) > 0 {
		log.Printf("Using %d cached Lighter prices (age: %v)",
			len(cachedPrices), time.Since(lastFetchTime))
		return cachedPrices, nil
	}

	return nil, fmt.Errorf("all %d requests failed and no cache available", parallelRequests)
}

// fetchMarketDataOnce 执行单次 API 请求
func fetchMarketDataOnce(apiURL string, marketIDs []int) ([]*common.Price, error) {
	client := &http.Client{Timeout: 15 * time.Second}

	// 使用 orderBookDetails endpoint
	url := fmt.Sprintf("%s/api/v1/orderBookDetails", apiURL)

	resp, err := client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch market data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp OrderBookDetailsResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if apiResp.Code != 200 {
		return nil, fmt.Errorf("API returned error code: %d", apiResp.Code)
	}

	// 创建市场 ID 映射
	marketIDSet := make(map[int]bool)
	for _, id := range marketIDs {
		marketIDSet[id] = true
	}

	// 统计信息
	totalMarkets := 0
	activeMarkets := 0
	noPrice := 0
	fromCache := 0

	// 转换为 Price 对象
	prices := make([]*common.Price, 0, len(marketIDSet))
	for _, data := range apiResp.OrderBookDetails {
		// 只处理我们订阅的市场
		if !marketIDSet[data.MarketID] {
			continue
		}
		totalMarkets++

		// 处理所有市场，不仅仅是 active 的（可能暂时 inactive 但仍有价值）
		if data.Status != "active" {
			// 尝试使用缓存
			symbol := data.Symbol + "USDT"
			key := fmt.Sprintf("%s-%s-%s", common.ExchangeLighter, common.MarketTypeFuture, symbol)

			priceCacheMu.RLock()
			cachedPrice, exists := priceCache[key]
			priceCacheMu.RUnlock()

			if exists && time.Since(cachedPrice.LastUpdated) < 10*time.Minute {
				prices = append(prices, cachedPrice)
				fromCache++
			}
			continue
		}
		activeMarkets++

		lastPrice := data.LastTradePrice
		if lastPrice == 0 || lastPrice < 0.0000001 {
			noPrice++
			// 尝试从缓存获取价格
			symbol := data.Symbol + "USDT"
			key := fmt.Sprintf("%s-%s-%s", common.ExchangeLighter, common.MarketTypeFuture, symbol)

			priceCacheMu.RLock()
			cachedPrice, exists := priceCache[key]
			priceCacheMu.RUnlock()

			if exists && time.Since(cachedPrice.LastUpdated) < 10*time.Minute {
				// 使用缓存价格
				prices = append(prices, cachedPrice)
				fromCache++
			}
			continue
		}

		// 使用 last_trade_price 估算 bid/ask（假设 0.01% 价差）
		spread := lastPrice * 0.0001
		if spread < 0.00001 {
			spread = lastPrice * 0.001 // 对于非常小的价格，使用 0.1% 价差
		}
		bidPrice := lastPrice - spread
		askPrice := lastPrice + spread

		// 所有 Lighter 市场都是永续合约
		marketType := common.MarketTypeFuture

		// Symbol 需要加上 USDT 后缀
		symbol := data.Symbol + "USDT"

		now := time.Now()
		price := &common.Price{
			Symbol:      symbol,
			Exchange:    common.ExchangeLighter,
			MarketType:  marketType,
			Price:       lastPrice,
			BidPrice:    bidPrice, // 注意：REST API用last trade估算，不是真实bid
			AskPrice:    askPrice, // 注意：REST API用last trade估算，不是真实ask
			BidQty:      0, // REST API 不提供订单簿数量
			AskQty:      0,
			Volume24h:   data.DailyQuoteTokenVolume,
			Timestamp:   now,                    // REST API没有交易所时间戳
			LastUpdated: now,                    // 本地接收时间
			Source:      common.PriceSourceREST, // 标记为REST数据源
		}

		prices = append(prices, price)
	}

	// 记录详细统计
	log.Printf("Lighter API: total=%d, active=%d, no_price=%d, from_cache=%d, returned=%d",
		totalMarkets, activeMarkets, noPrice, fromCache, len(prices))

	return prices, nil
}

// parseFloatStr 解析字符串为 float64
func parseFloatStr(s string) float64 {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}
