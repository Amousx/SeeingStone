package binance

import (
	"context"
	"crypto-arbitrage-monitor/pkg/common"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	binance_connector "github.com/binance/binance-connector-go"
)

// API Base URLs（按优先级排序）
var (
	// 现货 API URLs（优先使用性能更好的 api1-api4）
	SpotAPIBaseURLs = []string{
		"https://api1.binance.com",
		"https://api2.binance.com",
		"https://api3.binance.com",
		"https://api4.binance.com",
		"https://api.binance.com",     // 最稳定但性能略低
		"https://api-gcp.binance.com", // GCP 节点
	}

	// 合约 API URLs
	FuturesAPIBaseURLs = []string{
		"https://fapi.binance.com",
	}

	// 代理配置
	proxyURL    string
	proxyConfig sync.Mutex
)

// SetProxyURL 设置代理 URL（需要在创建客户端前调用）
func SetProxyURL(url string) {
	proxyConfig.Lock()
	defer proxyConfig.Unlock()
	proxyURL = url
	if url != "" {
		log.Printf("[Binance] Proxy enabled: %s", url)
	} else {
		log.Println("[Binance] Proxy disabled")
	}
}

// RestClient Binance REST API 客户端（可扩展）
type RestClient struct {
	spotClients    []*binance_connector.Client
	futuresClients []*binance_connector.Client
	currentSpotIdx int
	currentFutIdx  int
	mu             sync.Mutex
}

func newHTTPClient() *http.Client {
	// 获取代理配置
	proxyConfig.Lock()
	currentProxyURL := proxyURL
	proxyConfig.Unlock()

	// 创建 Transport
	// ！Warning: 超时配置，本地需要调整
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   60 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,

		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			MaxVersion: tls.VersionTLS12,
		},

		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,

		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,

		ForceAttemptHTTP2: false, // 🔥 关键
	}

	// 根据配置决定是否使用代理
	if currentProxyURL != "" {
		// 使用配置的代理
		proxyURLParsed, err := parseProxyURL(currentProxyURL)
		if err != nil {
			log.Printf("[Binance] Invalid proxy URL %s: %v, using direct connection", currentProxyURL, err)
			transport.Proxy = nil
		} else {
			transport.Proxy = http.ProxyURL(proxyURLParsed)
		}
	} else {
		// 不使用代理（直连）
		transport.Proxy = nil
	}

	return &http.Client{
		Timeout:   15 * time.Second,
		Transport: transport,
	}
}

// parseProxyURL 解析代理 URL
func parseProxyURL(urlStr string) (*url.URL, error) {
	return url.Parse(urlStr)
}

// NewRestClient 创建新的 REST 客户端
func NewRestClient() *RestClient {
	httpClient := newHTTPClient() // 🔥 只创建一次

	client := &RestClient{
		spotClients:    make([]*binance_connector.Client, 0, len(SpotAPIBaseURLs)),
		futuresClients: make([]*binance_connector.Client, 0, len(FuturesAPIBaseURLs)),
	}

	// 初始化现货客户端
	for _, baseURL := range SpotAPIBaseURLs {
		c := binance_connector.NewClient("", "")
		c.BaseURL = baseURL
		c.HTTPClient = httpClient // 🔥 关键注入
		client.spotClients = append(client.spotClients, c)
	}

	// 初始化合约客户端
	for _, baseURL := range FuturesAPIBaseURLs {
		c := binance_connector.NewClient("", "")
		c.BaseURL = baseURL
		c.HTTPClient = httpClient // 🔥 同样注入
		client.futuresClients = append(client.futuresClients, c)
	}

	return client
}

// 全局客户端实例
var (
	globalClient     *RestClient
	globalClientOnce sync.Once
)

// GetRestClient 获取全局 REST 客户端实例
func GetRestClient() *RestClient {
	globalClientOnce.Do(func() {
		globalClient = NewRestClient()
	})
	return globalClient
}

// FetchSpotPrices 获取现货市场所有价格（带重试和备用 URL）
func FetchSpotPrices() ([]*common.Price, error) {
	client := GetRestClient()
	return client.fetchSpotPricesWithRetry(3)
}

// FetchFuturesPrices 获取合约市场所有价格（带重试和备用 URL）
func FetchFuturesPrices() ([]*common.Price, error) {
	client := GetRestClient()
	return client.fetchFuturesPricesWithRetry(3)
}

// fetchSpotPricesWithRetry 获取现货价格（带重试）
func (c *RestClient) fetchSpotPricesWithRetry(maxRetries int) ([]*common.Price, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			log.Printf("[Binance API] Retry attempt %d/%d for SPOT", attempt, maxRetries)
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		prices, err := c.fetchSpotPrices()
		if err == nil {
			return prices, nil
		}

		lastErr = err
		log.Printf("[Binance API] Attempt %d/%d failed for SPOT: %v", attempt, maxRetries, err)

		// 尝试下一个 URL
		c.rotateSpotURL()
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}

// fetchFuturesPricesWithRetry 获取合约价格（带重试）
func (c *RestClient) fetchFuturesPricesWithRetry(maxRetries int) ([]*common.Price, error) {
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			log.Printf("[Binance API] Retry attempt %d/%d for FUTURE", attempt, maxRetries)
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		prices, err := c.fetchFuturesPrices()
		if err == nil {
			return prices, nil
		}

		lastErr = err
		log.Printf("[Binance API] Attempt %d/%d failed for FUTURE: %v", attempt, maxRetries, err)

		// 尝试下一个 URL
		c.rotateFuturesURL()
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}

// rotateSpotURL 轮换现货 API URL
func (c *RestClient) rotateSpotURL() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentSpotIdx = (c.currentSpotIdx + 1) % len(c.spotClients)
	log.Printf("[Binance API] Switched to spot URL: %s", SpotAPIBaseURLs[c.currentSpotIdx])
}

// rotateFuturesURL 轮换合约 API URL
func (c *RestClient) rotateFuturesURL() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.currentFutIdx = (c.currentFutIdx + 1) % len(c.futuresClients)
	log.Printf("[Binance API] Switched to futures URL: %s", FuturesAPIBaseURLs[c.currentFutIdx])
}

// fetchSpotPrices 获取现货价格（单次请求）- 使用 TickerPrice API（轻量级）
func (c *RestClient) fetchSpotPrices() ([]*common.Price, error) {
	c.mu.Lock()
	client := c.spotClients[c.currentSpotIdx]
	currentURL := SpotAPIBaseURLs[c.currentSpotIdx]
	c.mu.Unlock()

	log.Printf("[Binance API] Fetching SPOT prices from %s", currentURL)
	startTime := time.Now()

	// 使用 SDK 获取 TickerPrice（轻量级，只有 symbol 和 price）
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tickers, err := client.NewTickerPriceService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch spot tickers: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("[Binance API] Fetched %d SPOT tickers in %.2fs", len(tickers), duration.Seconds())

	// 转换为通用 Price 格式
	prices := make([]*common.Price, 0, len(tickers))
	for _, ticker := range tickers {
		price := convertTickerPriceToPrice(*ticker, common.MarketTypeSpot)
		if price != nil {
			prices = append(prices, price)
		}
	}

	log.Printf("[Binance API] ✓ Successfully processed %d SPOT prices", len(prices))
	return prices, nil
}

// fetchFuturesPrices 获取合约价格（单次请求）- 使用 TickerPrice API（轻量级）
func (c *RestClient) fetchFuturesPrices() ([]*common.Price, error) {
	c.mu.Lock()
	client := c.futuresClients[c.currentFutIdx]
	currentURL := FuturesAPIBaseURLs[c.currentFutIdx]
	c.mu.Unlock()

	log.Printf("[Binance API] Fetching FUTURE prices from %s", currentURL)
	startTime := time.Now()

	// 使用 SDK 获取 TickerPrice（轻量级，只有 symbol 和 price）
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	tickers, err := client.NewTickerPriceService().Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch futures tickers: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("[Binance API] Fetched %d FUTURE tickers in %.2fs", len(tickers), duration.Seconds())

	// 转换为通用 Price 格式
	prices := make([]*common.Price, 0, len(tickers))
	for _, ticker := range tickers {
		price := convertTickerPriceToPrice(*ticker, common.MarketTypeFuture)
		if price != nil {
			prices = append(prices, price)
		}
	}

	log.Printf("[Binance API] ✓ Successfully processed %d FUTURE prices", len(prices))
	return prices, nil
}

// convertTickerPriceToPrice 将 SDK 返回的 TickerPrice 转换为通用 Price
func convertTickerPriceToPrice(ticker binance_connector.TickerPriceResponse, marketType common.MarketType) *common.Price {
	// 转换价格（SDK 返回的都是字符串）
	price := parseFloat(ticker.Price)

	// 如果价格为 0，跳过
	if price == 0 {
		return nil
	}

	return &common.Price{
		Symbol:      ticker.Symbol,
		Exchange:    common.ExchangeBinance,
		MarketType:  marketType,
		Price:       price,
		BidPrice:    price, // TickerPrice 没有 bid/ask，使用 price
		AskPrice:    price,
		BidQty:      0, // TickerPrice 没有数量信息
		AskQty:      0,
		Volume24h:   0, // TickerPrice 没有成交量信息
		Timestamp:   time.Now(),
		LastUpdated: time.Now(),
	}
}
