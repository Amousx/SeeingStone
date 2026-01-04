package okx

import (
	"bufio"
	"bytes"
	"crypto-arbitrage-monitor/pkg/common"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	BaseURL = "https://web3.okx.com"
)

// APIConfig OKX API配置
type APIConfig struct {
	APIKey     string
	SecretKey  string
	Passphrase string
	LastUsed   time.Time
}

// Client OKX DEX客户端
type Client struct {
	baseURL    string
	httpClient *http.Client

	// API密钥池（用于轮询，规避限速）
	apiConfigs []*APIConfig
	apiMu      sync.Mutex
	apiIndex   int

	// 限速控制：每秒1次请求
	rateLimiter *time.Ticker
}

// LoadAPIConfigs 从文件加载API配置
// 文件格式：每行为 "APIKey,SecretKey,Passphrase"
func LoadAPIConfigs(filePath string) ([]*APIConfig, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open API config file failed: %w", err)
	}
	defer file.Close()

	configs := make([]*APIConfig, 0)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析：APIKey,SecretKey,Passphrase
		parts := strings.Split(line, ",")
		if len(parts) != 3 {
			log.Printf("[OKX] Warning: line %d invalid format (expected 3 fields): %s", lineNum, line)
			continue
		}

		config := &APIConfig{
			APIKey:     strings.TrimSpace(parts[0]),
			SecretKey:  strings.TrimSpace(parts[1]),
			Passphrase: strings.TrimSpace(parts[2]),
		}

		configs = append(configs, config)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read API config file failed: %w", err)
	}

	log.Printf("[OKX] Loaded %d API configs from %s", len(configs), filePath)
	return configs, nil
}

// NewClient 创建OKX DEX客户端
func NewClient(apiConfigs []*APIConfig) *Client {
	if len(apiConfigs) == 0 {
		log.Println("[OKX] Warning: No API configs provided")
		return nil
	}

	return &Client{
		baseURL:     BaseURL,
		httpClient:  &http.Client{Timeout: 10 * time.Second},
		apiConfigs:  apiConfigs,
		apiIndex:    0,
		rateLimiter: time.NewTicker(time.Second), // 每秒1次
	}
}

// getNextAPIConfig 获取下一个可用的API配置（轮询）
func (c *Client) getNextAPIConfig() *APIConfig {
	c.apiMu.Lock()
	defer c.apiMu.Unlock()

	config := c.apiConfigs[c.apiIndex]
	c.apiIndex = (c.apiIndex + 1) % len(c.apiConfigs)

	return config
}

// generateSignature 生成签名
// signature = Base64(HMAC-SHA256(timestamp + method + requestPath + body, secretKey))
func (c *Client) generateSignature(timestamp, method, requestPath, body, secretKey string) string {
	message := timestamp + method + requestPath + body

	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(message))

	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// doRequest 执行HTTP请求（带签名认证）
func (c *Client) doRequest(method, path string, body string) ([]byte, error) {
	// 限速：等待下一个时间槽
	<-c.rateLimiter.C

	// 获取API配置
	config := c.getNextAPIConfig()

	// 生成时间戳 (ISO 8601 UTC)
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// 生成签名
	signature := c.generateSignature(timestamp, method, path, body, config.SecretKey)

	// 构建完整URL
	url := c.baseURL + path

	// 创建请求
	var req *http.Request
	var err error

	if method == "GET" {
		req, err = http.NewRequest(method, url, nil)
	} else {
		req, err = http.NewRequest(method, url, bytes.NewBufferString(body))
	}

	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	// 设置认证头
	req.Header.Set("OK-ACCESS-KEY", config.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", config.Passphrase)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	// 检查HTTP状态码
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %d: %s", resp.StatusCode, string(data))
	}

	// 更新最后使用时间
	config.LastUsed = time.Now()

	return data, nil
}

// QuoteRequest 询价请求参数
type QuoteRequest struct {
	ChainIndex               string // 链ID (如 "1" 为 Ethereum)
	Amount                   string // 交易数量（包含精度）
	FromTokenAddress         string // 卖出币种合约地址
	ToTokenAddress           string // 买入币种合约地址
	SwapMode                 string // 交易模式: "exactIn" 或 "exactOut"
	PriceImpactProtectionPct string // 价格影响保护百分比 (默认90)
}

// QuoteResponse 询价响应
type QuoteResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		ChainIndex      string `json:"chainIndex"`
		FromTokenAmount string `json:"fromTokenAmount"`
		ToTokenAmount   string `json:"toTokenAmount"`
		TradeFee        string `json:"tradeFee"`
		EstimateGasFee  string `json:"estimateGasFee"`
		PriceImpactPercent string `json:"priceImpactPercent"`
		Router          string `json:"router"` // 路由路径字符串
		SwapMode        string `json:"swapMode"`
		FromToken       struct {
			TokenContractAddress string `json:"tokenContractAddress"`
			TokenSymbol          string `json:"tokenSymbol"`
			TokenUnitPrice       string `json:"tokenUnitPrice"`
			Decimal              string `json:"decimal"`
			IsHoneyPot           bool   `json:"isHoneyPot"`
			TaxRate              string `json:"taxRate"`
		} `json:"fromToken"`
		ToToken struct {
			TokenContractAddress string `json:"tokenContractAddress"`
			TokenSymbol          string `json:"tokenSymbol"`
			TokenUnitPrice       string `json:"tokenUnitPrice"`
			Decimal              string `json:"decimal"`
			IsHoneyPot           bool   `json:"isHoneyPot"`
			TaxRate              string `json:"taxRate"`
		} `json:"toToken"`
		DexRouterList []struct {
			DexProtocol struct {
				DexName string `json:"dexName"`
				Percent string `json:"percent"`
			} `json:"dexProtocol"`
			FromToken struct {
				TokenContractAddress string `json:"tokenContractAddress"`
				TokenSymbol          string `json:"tokenSymbol"`
				TokenUnitPrice       string `json:"tokenUnitPrice"`
				Decimal              string `json:"decimal"`
				IsHoneyPot           bool   `json:"isHoneyPot"`
				TaxRate              string `json:"taxRate"`
			} `json:"fromToken"`
			ToToken struct {
				TokenContractAddress string `json:"tokenContractAddress"`
				TokenSymbol          string `json:"tokenSymbol"`
				TokenUnitPrice       string `json:"tokenUnitPrice"`
				Decimal              string `json:"decimal"`
				IsHoneyPot           bool   `json:"isHoneyPot"`
				TaxRate              string `json:"taxRate"`
			} `json:"toToken"`
			FromTokenIndex string `json:"fromTokenIndex"`
			ToTokenIndex   string `json:"toTokenIndex"`
		} `json:"dexRouterList"`
	} `json:"data"`
}

// GetQuote 获取兑换价格（详细询价）
func (c *Client) GetQuote(req *QuoteRequest) (*QuoteResponse, error) {
	// 构建请求路径
	path := fmt.Sprintf("/api/v6/dex/aggregator/quote?chainIndex=%s&amount=%s&fromTokenAddress=%s&toTokenAddress=%s",
		req.ChainIndex, req.Amount, req.FromTokenAddress, req.ToTokenAddress)

	if req.SwapMode != "" {
		path += "&swapMode=" + req.SwapMode
	}

	if req.PriceImpactProtectionPct != "" {
		path += "&priceImpactProtectionPercent=" + req.PriceImpactProtectionPct
	}

	// 发送请求
	data, err := c.doRequest("GET", path, "")
	if err != nil {
		return nil, err
	}

	// 解析响应
	var resp QuoteResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse response failed: %w", err)
	}

	if resp.Code != "0" {
		return nil, fmt.Errorf("API error: %s - %s", resp.Code, resp.Msg)
	}

	return &resp, nil
}

// MarketPriceRequest 市场价格请求
type MarketPriceRequest struct {
	ChainIndex           string `json:"chainIndex"`
	TokenContractAddress string `json:"tokenContractAddress"`
}

// MarketPriceResponse 市场价格响应
type MarketPriceResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
	Data []struct {
		ChainIndex           string `json:"chainIndex"`
		TokenContractAddress string `json:"tokenContractAddress"`
		Time                 string `json:"time"`
		Price                string `json:"price"`
	} `json:"data"`
}

// GetMarketPrice 获取单个代币粗略价格（更快速）
func (c *Client) GetMarketPrice(req *MarketPriceRequest) (*MarketPriceResponse, error) {
	return c.GetMarketPriceBatch([]*MarketPriceRequest{req})
}

// GetMarketPriceBatch 批量获取代币粗略价格（支持批量请求）
func (c *Client) GetMarketPriceBatch(requests []*MarketPriceRequest) (*MarketPriceResponse, error) {
	path := "/api/v6/dex/market/price"

	// 构建请求体（数组格式）
	bodyBytes, err := json.Marshal(requests)
	if err != nil {
		return nil, err
	}
	body := string(bodyBytes)

	// 发送请求
	data, err := c.doRequest("POST", path, body)
	if err != nil {
		return nil, err
	}

	// 解析响应
	var resp MarketPriceResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parse response failed: %w", err)
	}

	if resp.Code != "0" {
		return nil, fmt.Errorf("API error: %s - %s", resp.Code, resp.Msg)
	}

	return &resp, nil
}

// ConvertToCommonPrice 将OKX价格转换为通用价格格式
// direction: 交易方向，决定bid/ask价格设置
//   - DirectionTokenToUSDT: priceUSD是bid价格（卖出价）
//   - DirectionUSDTToToken: priceUSD是ask价格（买入价）
func ConvertToCommonPrice(tokenConfig *TokenConfig, priceUSD float64, direction QuoteDirection) *common.Price {
	now := time.Now()

	var bidPrice, askPrice float64
	if direction == DirectionTokenToUSDT {
		// Token→USDT：得到的是卖出价格（bid）
		bidPrice = priceUSD
		askPrice = 0 // 稍后从另一个方向获取
	} else {
		// USDT→Token：得到的是买入价格（ask）
		bidPrice = 0 // 稍后从另一个方向获取
		askPrice = priceUSD
	}

	// OKX返回的是USD价格，我们作为SPOT市场类型
	return &common.Price{
		Symbol:             tokenConfig.Symbol,
		Exchange:           common.ExchangeOKX,
		MarketType:         common.MarketTypeSpot,
		Price:              priceUSD, // 使用当前方向的价格
		BidPrice:           bidPrice,
		AskPrice:           askPrice,
		Volume24h:          0, // OKX DEX不提供24h交易量
		Timestamp:          now,
		LastUpdated:        now,
		Source:             common.PriceSourceREST,
		QuoteCurrency:      common.QuoteCurrencyUSDT,
		IsNormalized:       true,
		ExchangeRate:       1.0,
		ExchangeRateSource: "IDENTITY",
	}
}

// Close 关闭客户端
func (c *Client) Close() {
	if c.rateLimiter != nil {
		c.rateLimiter.Stop()
	}
}
