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

// FetchMarketData 从 REST API 获取市场数据（带重试和缓存）
func FetchMarketData(apiURL string, marketIDs []int) ([]*common.Price, error) {
	const maxRetries = 3
	const retryDelay = 2 * time.Second

	var lastErr error

	// 重试逻辑
	for attempt := 1; attempt <= maxRetries; attempt++ {
		prices, err := fetchMarketDataOnce(apiURL, marketIDs)
		if err == nil {
			// 成功获取数据
			lastFetchTime = time.Now()
			lastFetchCount = len(prices)

			// 更新缓存
			priceCacheMu.Lock()
			for _, price := range prices {
				key := fmt.Sprintf("%s-%s-%s", price.Exchange, price.MarketType, price.Symbol)
				priceCache[key] = price
			}
			priceCacheMu.Unlock()

			// 重置错误计数
			if fetchErrorCount > 0 {
				log.Printf("Lighter API recovered after %d errors", fetchErrorCount)
				fetchErrorCount = 0
			}

			return prices, nil
		}

		lastErr = err
		fetchErrorCount++

		if attempt < maxRetries {
			log.Printf("Lighter API attempt %d/%d failed: %v, retrying in %v...",
				attempt, maxRetries, err, retryDelay)
			time.Sleep(retryDelay)
		}
	}

	// 所有重试都失败，使用缓存数据
	log.Printf("Lighter API failed after %d attempts: %v, using cached data", maxRetries, lastErr)

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

	return nil, fmt.Errorf("no cached data available: %w", lastErr)
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

	// 转换为 Price 对象
	prices := make([]*common.Price, 0)
	for _, data := range apiResp.OrderBookDetails {
		// 只处理我们订阅的市场
		if !marketIDSet[data.MarketID] {
			continue
		}
		totalMarkets++

		// 只处理活跃市场
		if data.Status != "active" {
			continue
		}
		activeMarkets++

		lastPrice := data.LastTradePrice
		if lastPrice == 0 {
			noPrice++
			// 尝试从缓存获取价格
			symbol := data.Symbol + "USDT"
			key := fmt.Sprintf("%s-%s-%s", common.ExchangeLighter, common.MarketTypeFuture, symbol)

			priceCacheMu.RLock()
			cachedPrice, exists := priceCache[key]
			priceCacheMu.RUnlock()

			if exists && time.Since(cachedPrice.LastUpdated) < 5*time.Minute {
				// 使用缓存价格，但更新时间戳
				price := &common.Price{
					Symbol:      cachedPrice.Symbol,
					Exchange:    cachedPrice.Exchange,
					MarketType:  cachedPrice.MarketType,
					Price:       cachedPrice.Price,
					BidPrice:    cachedPrice.BidPrice,
					AskPrice:    cachedPrice.AskPrice,
					BidQty:      0,
					AskQty:      0,
					Volume24h:   data.DailyQuoteTokenVolume,
					Timestamp:   time.Now(),
					LastUpdated: cachedPrice.LastUpdated, // 保留原始更新时间
				}
				prices = append(prices, price)
			}
			continue
		}

		// 使用 last_trade_price 估算 bid/ask（假设 0.01% 价差）
		spread := lastPrice * 0.0001
		bidPrice := lastPrice - spread
		askPrice := lastPrice + spread

		// 所有 Lighter 市场都是永续合约
		marketType := common.MarketTypeFuture

		// Symbol 需要加上 USDT 后缀
		symbol := data.Symbol + "USDT"

		price := &common.Price{
			Symbol:      symbol,
			Exchange:    common.ExchangeLighter,
			MarketType:  marketType,
			Price:       lastPrice,
			BidPrice:    bidPrice,
			AskPrice:    askPrice,
			BidQty:      0, // REST API 不提供订单簿数量
			AskQty:      0,
			Volume24h:   data.DailyQuoteTokenVolume,
			Timestamp:   time.Now(),
			LastUpdated: time.Now(),
		}

		prices = append(prices, price)
	}

	// 记录详细统计
	if noPrice > 0 || totalMarkets-activeMarkets > 0 {
		log.Printf("Lighter API stats: total=%d, active=%d, no_price=%d, returned=%d",
			totalMarkets, activeMarkets, noPrice, len(prices))
	}

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
