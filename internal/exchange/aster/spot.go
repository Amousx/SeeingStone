package aster

import (
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// SpotClient Aster 现货API客户端
type SpotClient struct {
	BaseURL    string
	Auth       *Auth
	HTTPClient *http.Client
}

// NewSpotClient 创建现货客户端
func NewSpotClient(baseURL, apiKey, secretKey string) *SpotClient {
	return &SpotClient{
		BaseURL: baseURL,
		Auth:    NewAuth(apiKey, secretKey),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// ExchangeInfo 交易所信息响应
type ExchangeInfo struct {
	Timezone   string   `json:"timezone"`
	ServerTime int64    `json:"serverTime"`
	Symbols    []Symbol `json:"symbols"`
}

// Symbol 交易对信息
type Symbol struct {
	Symbol     string `json:"symbol"`
	Status     string `json:"status"`
	BaseAsset  string `json:"baseAsset"`
	QuoteAsset string `json:"quoteAsset"`
}

// TickerPrice 最新价格
type TickerPrice struct {
	Symbol string  `json:"symbol"`
	Price  string  `json:"price"`
	Time   int64   `json:"time"`
}

// BookTicker 最优挂单
type BookTicker struct {
	Symbol   string `json:"symbol"`
	BidPrice string `json:"bidPrice"`
	BidQty   string `json:"bidQty"`
	AskPrice string `json:"askPrice"`
	AskQty   string `json:"askQty"`
	Time     int64  `json:"time"`
}

// Ticker24hr 24小时价格变动
type Ticker24hr struct {
	Symbol             string `json:"symbol"`
	PriceChange        string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	LastPrice          string `json:"lastPrice"`
	BidPrice           string `json:"bidPrice"`
	AskPrice           string `json:"askPrice"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
	OpenTime           int64  `json:"openTime"`
	CloseTime          int64  `json:"closeTime"`
}

// GetExchangeInfo 获取交易所信息
func (c *SpotClient) GetExchangeInfo() (*ExchangeInfo, error) {
	endpoint := "/api/v1/exchangeInfo"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var info ExchangeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal exchange info: %w", err)
	}

	return &info, nil
}

// GetTickerPrice 获取最新价格
func (c *SpotClient) GetTickerPrice(symbol string) (*TickerPrice, error) {
	endpoint := "/api/v1/ticker/price"
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.doRequest("GET", endpoint, params, false)
	if err != nil {
		return nil, err
	}

	var ticker TickerPrice
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ticker price: %w", err)
	}

	return &ticker, nil
}

// GetAllTickerPrices 获取所有交易对最新价格
func (c *SpotClient) GetAllTickerPrices() ([]TickerPrice, error) {
	endpoint := "/api/v1/ticker/price"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var tickers []TickerPrice
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ticker prices: %w", err)
	}

	return tickers, nil
}

// GetBookTicker 获取最优挂单
func (c *SpotClient) GetBookTicker(symbol string) (*BookTicker, error) {
	endpoint := "/api/v1/ticker/bookTicker"
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.doRequest("GET", endpoint, params, false)
	if err != nil {
		return nil, err
	}

	var ticker BookTicker
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal book ticker: %w", err)
	}

	return &ticker, nil
}

// GetAllBookTickers 获取所有交易对最优挂单
func (c *SpotClient) GetAllBookTickers() ([]BookTicker, error) {
	endpoint := "/api/v1/ticker/bookTicker"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var tickers []BookTicker
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal book tickers: %w", err)
	}

	return tickers, nil
}

// Get24hrTicker 获取24小时价格变动
func (c *SpotClient) Get24hrTicker(symbol string) (*Ticker24hr, error) {
	endpoint := "/api/v1/ticker/24hr"
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.doRequest("GET", endpoint, params, false)
	if err != nil {
		return nil, err
	}

	var ticker Ticker24hr
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal 24hr ticker: %w", err)
	}

	return &ticker, nil
}

// GetAll24hrTickers 获取所有交易对24小时价格变动
func (c *SpotClient) GetAll24hrTickers() ([]Ticker24hr, error) {
	endpoint := "/api/v1/ticker/24hr"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var tickers []Ticker24hr
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal 24hr tickers: %w", err)
	}

	return tickers, nil
}

// ConvertToCommonPrice 转换为通用价格格式（REST API）
func (c *SpotClient) ConvertToCommonPrice(ticker *BookTicker, volume24h float64) *common.Price {
	bidPrice := parseFloat(ticker.BidPrice)
	askPrice := parseFloat(ticker.AskPrice)

	return &common.Price{
		Symbol:      ticker.Symbol,
		Exchange:    common.ExchangeAster,
		MarketType:  common.MarketTypeSpot,
		Price:       (bidPrice + askPrice) / 2,
		BidPrice:    bidPrice,
		AskPrice:    askPrice,
		BidQty:      parseFloat(ticker.BidQty),
		AskQty:      parseFloat(ticker.AskQty),
		Volume24h:   volume24h,
		Timestamp:   time.UnixMilli(ticker.Time), // 使用交易所时间
		LastUpdated: time.Now(),                  // 本地接收时间
		Source:      common.PriceSourceREST,      // 标记为REST数据源
	}
}

// doRequest 执行HTTP请求
func (c *SpotClient) doRequest(method, endpoint string, params map[string]string, signed bool) ([]byte, error) {
	// 构建URL
	reqURL := c.BaseURL + endpoint

	// 如果需要签名
	if signed {
		params = c.Auth.SignedParams(params)
	}

	// 添加查询参数
	if len(params) > 0 && method == "GET" {
		values := url.Values{}
		for k, v := range params {
			values.Add(k, v)
		}
		reqURL += "?" + values.Encode()
	}

	// 创建请求
	req, err := http.NewRequest(method, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// 添加认证头
	headers := make(map[string]string)
	c.Auth.AddAuthHeaders(headers)
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	// 发送请求
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error: status=%d, body=%s", resp.StatusCode, string(body))
	}

	return body, nil
}
