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

// FuturesClient Aster 合约API客户端
type FuturesClient struct {
	BaseURL    string
	Auth       *Auth
	HTTPClient *http.Client
}

// NewFuturesClient 创建合约客户端
func NewFuturesClient(baseURL, apiKey, secretKey string) *FuturesClient {
	return &FuturesClient{
		BaseURL: baseURL,
		Auth:    NewAuth(apiKey, secretKey),
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// FuturesExchangeInfo 合约交易所信息
type FuturesExchangeInfo struct {
	Timezone   string          `json:"timezone"`
	ServerTime int64           `json:"serverTime"`
	Symbols    []FuturesSymbol `json:"symbols"`
}

// FuturesSymbol 合约交易对信息
type FuturesSymbol struct {
	Symbol                string `json:"symbol"`
	Status                string `json:"status"`
	BaseAsset             string `json:"baseAsset"`
	QuoteAsset            string `json:"quoteAsset"`
	ContractType          string `json:"contractType"`
	DeliveryDate          int64  `json:"deliveryDate"`
	OnboardDate           int64  `json:"onboardDate"`
	ContractStatus        string `json:"contractStatus"`
	ContractSize          int    `json:"contractSize"`
	MarginAsset           string `json:"marginAsset"`
	MaintMarginPercent    string `json:"maintMarginPercent"`
	RequiredMarginPercent string `json:"requiredMarginPercent"`
}

// FuturesTickerPrice 合约最新价格
type FuturesTickerPrice struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
	Time   int64  `json:"time"`
}

// FuturesBookTicker 合约最优挂单
type FuturesBookTicker struct {
	Symbol   string `json:"symbol"`
	BidPrice string `json:"bidPrice"`
	BidQty   string `json:"bidQty"`
	AskPrice string `json:"askPrice"`
	AskQty   string `json:"askQty"`
	Time     int64  `json:"time"`
}

// FuturesTicker24hr 合约24小时价格变动
type FuturesTicker24hr struct {
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
	Count              int64  `json:"count"`
}

// MarkPrice 标记价格
type MarkPrice struct {
	Symbol          string `json:"symbol"`
	MarkPrice       string `json:"markPrice"`
	IndexPrice      string `json:"indexPrice"`
	LastFundingRate string `json:"lastFundingRate"`
	NextFundingTime int64  `json:"nextFundingTime"`
	Time            int64  `json:"time"`
}

// GetExchangeInfo 获取合约交易所信息
func (c *FuturesClient) GetExchangeInfo() (*FuturesExchangeInfo, error) {
	endpoint := "/fapi/v1/exchangeInfo"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var info FuturesExchangeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("failed to unmarshal exchange info: %w", err)
	}

	return &info, nil
}

// GetTickerPrice 获取最新价格
func (c *FuturesClient) GetTickerPrice(symbol string) (*FuturesTickerPrice, error) {
	endpoint := "/fapi/v1/ticker/price"
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.doRequest("GET", endpoint, params, false)
	if err != nil {
		return nil, err
	}

	var ticker FuturesTickerPrice
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ticker price: %w", err)
	}

	return &ticker, nil
}

// GetAllTickerPrices 获取所有合约最新价格
func (c *FuturesClient) GetAllTickerPrices() ([]FuturesTickerPrice, error) {
	endpoint := "/fapi/v1/ticker/price"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var tickers []FuturesTickerPrice
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal ticker prices: %w", err)
	}

	return tickers, nil
}

// GetBookTicker 获取最优挂单
func (c *FuturesClient) GetBookTicker(symbol string) (*FuturesBookTicker, error) {
	endpoint := "/fapi/v1/ticker/bookTicker"
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.doRequest("GET", endpoint, params, false)
	if err != nil {
		return nil, err
	}

	var ticker FuturesBookTicker
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal book ticker: %w", err)
	}

	return &ticker, nil
}

// GetAllBookTickers 获取所有合约最优挂单
func (c *FuturesClient) GetAllBookTickers() ([]FuturesBookTicker, error) {
	endpoint := "/fapi/v1/ticker/bookTicker"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var tickers []FuturesBookTicker
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal book tickers: %w", err)
	}

	return tickers, nil
}

// Get24hrTicker 获取24小时价格变动
func (c *FuturesClient) Get24hrTicker(symbol string) (*FuturesTicker24hr, error) {
	endpoint := "/fapi/v1/ticker/24hr"
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.doRequest("GET", endpoint, params, false)
	if err != nil {
		return nil, err
	}

	var ticker FuturesTicker24hr
	if err := json.Unmarshal(data, &ticker); err != nil {
		return nil, fmt.Errorf("failed to unmarshal 24hr ticker: %w", err)
	}

	return &ticker, nil
}

// GetAll24hrTickers 获取所有合约24小时价格变动
func (c *FuturesClient) GetAll24hrTickers() ([]FuturesTicker24hr, error) {
	endpoint := "/fapi/v1/ticker/24hr"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var tickers []FuturesTicker24hr
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal 24hr tickers: %w", err)
	}

	return tickers, nil
}

// GetMarkPrice 获取标记价格
func (c *FuturesClient) GetMarkPrice(symbol string) (*MarkPrice, error) {
	endpoint := "/fapi/v1/premiumIndex"
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.doRequest("GET", endpoint, params, false)
	if err != nil {
		return nil, err
	}

	var markPrice MarkPrice
	if err := json.Unmarshal(data, &markPrice); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mark price: %w", err)
	}

	return &markPrice, nil
}

// GetAllMarkPrices 获取所有标记价格
func (c *FuturesClient) GetAllMarkPrices() ([]MarkPrice, error) {
	endpoint := "/fapi/v1/premiumIndex"
	data, err := c.doRequest("GET", endpoint, nil, false)
	if err != nil {
		return nil, err
	}

	var markPrices []MarkPrice
	if err := json.Unmarshal(data, &markPrices); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mark prices: %w", err)
	}

	return markPrices, nil
}

// ConvertToCommonPrice 转换为通用价格格式
func (c *FuturesClient) ConvertToCommonPrice(ticker *FuturesBookTicker, volume24h float64) *common.Price {
	bidPrice := parseFloat(ticker.BidPrice)
	askPrice := parseFloat(ticker.AskPrice)

	return &common.Price{
		Symbol:      ticker.Symbol,
		Exchange:    common.ExchangeAster,
		MarketType:  common.MarketTypeFuture,
		Price:       (bidPrice + askPrice) / 2,
		BidPrice:    bidPrice,
		AskPrice:    askPrice,
		BidQty:      parseFloat(ticker.BidQty),
		AskQty:      parseFloat(ticker.AskQty),
		Volume24h:   volume24h,
		Timestamp:   time.UnixMilli(ticker.Time),
		LastUpdated: time.Now(),
	}
}

// doRequest 执行HTTP请求
func (c *FuturesClient) doRequest(method, endpoint string, params map[string]string, signed bool) ([]byte, error) {
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
