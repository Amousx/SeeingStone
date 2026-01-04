package okx

import (
	"context"
	"crypto-arbitrage-monitor/internal/pricestore"
	"crypto-arbitrage-monitor/pkg/common"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// QuoteDirection 询价方向
type QuoteDirection string

const (
	// DirectionTokenToUSDT 代币→USDT（卖出代币，得到bid价格）
	DirectionTokenToUSDT QuoteDirection = "TOKEN_TO_USDT"
	// DirectionUSDTToToken USDT→代币（买入代币，得到ask价格）
	DirectionUSDTToToken QuoteDirection = "USDT_TO_TOKEN"
)

// KeyWorker 单个API Key的工作器（每个Worker独立运行，1 req/s）
type KeyWorker struct {
	ID                  int
	APIConfig           *APIConfig
	httpClient          *http.Client
	RateLimiter         *time.Ticker
	DirectionalTaskChan chan *DirectionalTask         // 单向任务通道
	coordinator         *BidirectionalTaskCoordinator // 协调器引用
	Store               *pricestore.PriceStore
	ResultChan          chan *FetchResult
	lastAssignedTime    time.Time  // 最后分配任务的时间（用于负载均衡）
	assignMu            sync.Mutex // 保护lastAssignedTime的互斥锁
}

// FetchResult 价格获取结果
type FetchResult struct {
	TokenConfig *TokenConfig
	Direction   QuoteDirection
	Price       *common.Price
	Error       error
}

// NewKeyWorker 创建新的Key Worker
func NewKeyWorker(id int, apiConfig *APIConfig, store *pricestore.PriceStore) *KeyWorker {
	return &KeyWorker{
		ID:                  id,
		APIConfig:           apiConfig,
		httpClient:          &http.Client{Timeout: 10 * time.Second},
		RateLimiter:         time.NewTicker(time.Second),     // 每秒1次
		DirectionalTaskChan: make(chan *DirectionalTask, 20), // 单向任务通道，容量更大
		coordinator:         nil,                             // 稍后由外部设置
		Store:               store,
		ResultChan:          make(chan *FetchResult, 10),
	}
}

// TaskWithDirection Worker任务（带方向）
type TaskWithDirection struct {
	TokenConfig *TokenConfig
	Direction   QuoteDirection
}

// Run 运行Worker（支持双向任务和单向任务）
func (w *KeyWorker) Run(ctx context.Context) {
	defer w.RateLimiter.Stop()

	for {
		select {
		case dt := <-w.DirectionalTaskChan:
			// 处理单向任务（新增逻辑，用于并行模式）
			w.handleDirectionalTask(dt)

		case <-ctx.Done():
			log.Printf("[OKX Worker %d] Stopping...", w.ID)
			return
		}
	}
}

// handleBidirectionalTask 处理双向任务（原有逻辑）
// 串行执行bid和ask两个方向的查询，合并结果后更新PriceStore
func (w *KeyWorker) handleBidirectionalTask(tc *TokenConfig) {
	// 等待限速器
	<-w.RateLimiter.C

	// 获取双向价格（bid和ask）
	// 1. Token→USDT获取bid价格
	bidPrice, bidErr := w.fetchTokenPrice(tc, DirectionTokenToUSDT)

	// 再次等待限速器（避免API调用过快）
	<-w.RateLimiter.C

	// 2. USDT→Token获取ask价格
	askPrice, askErr := w.fetchTokenPrice(tc, DirectionUSDTToToken)

	// 合并bid和ask价格
	var mergedPrice *common.Price
	var mergedErr error

	if bidErr == nil && askErr == nil && bidPrice != nil && askPrice != nil {
		// 两个方向都成功，合并价格
		mergedPrice = bidPrice
		mergedPrice.AskPrice = askPrice.AskPrice
		mergedPrice.Price = (mergedPrice.BidPrice + mergedPrice.AskPrice) / 2 // 中间价
		w.Store.UpdatePrice(mergedPrice)
	} else if bidErr == nil && bidPrice != nil {
		// 只有bid成功
		w.Store.UpdatePrice(bidPrice)
		mergedPrice = bidPrice
		mergedErr = askErr
	} else if askErr == nil && askPrice != nil {
		// 只有ask成功
		w.Store.UpdatePrice(askPrice)
		mergedPrice = askPrice
		mergedErr = bidErr
	} else {
		// 都失败了
		mergedErr = bidErr
	}

	// 发送结果
	w.ResultChan <- &FetchResult{
		TokenConfig: tc,
		Direction:   DirectionTokenToUSDT,
		Price:       mergedPrice,
		Error:       mergedErr,
	}
}

// handleDirectionalTask 处理单向任务（新增逻辑）
// 只执行一个方向的查询，将结果通知协调器
func (w *KeyWorker) handleDirectionalTask(dt *DirectionalTask) {
	// 等待限速器
	<-w.RateLimiter.C

	// 执行单向查询
	price, err := w.fetchTokenPrice(dt.TokenConfig, dt.Direction)

	// 创建结果
	result := &FetchResult{
		TokenConfig: dt.TokenConfig,
		Direction:   dt.Direction,
		Price:       price,
		Error:       err,
	}

	// 通知协调器（如果已设置）
	if w.coordinator != nil {
		w.coordinator.OnDirectionalResult(dt.TaskID, dt.Direction, result)
	} else {
		// 协调器未设置，记录警告
		log.Printf("[OKX Worker %d] Warning: coordinator not set for directional task %s", w.ID, dt.TaskID)
	}
}

// fetchTokenPrice 获取单个代币价格（使用Quote API）
// direction: 交易方向
//   - DirectionTokenToUSDT: Token→USDT（卖出代币，获取bid价格）
//   - DirectionUSDTToToken: USDT→Token（买入代币，获取ask价格）
func (w *KeyWorker) fetchTokenPrice(tc *TokenConfig, direction QuoteDirection) (*common.Price, error) {
	// 获取USDT地址
	usdtAddress := GetUSDTAddress(tc.ChainIndex)

	var path string
	var fromAddress, toAddress string
	var fromDecimals, toDecimals int

	// 根据方向确定交易对和数量
	if direction == DirectionTokenToUSDT {
		// Token→USDT：卖出代币，得到bid价格
		fromAddress = tc.Address
		toAddress = usdtAddress
		fromDecimals = tc.Decimals
		toDecimals = 6 // USDT精度

		// 计算询价数量：价值约200 USDT的代币数量
		amount := Calculate200USDTAmount(tc.GetDefaultPrice(), tc.Decimals)
		path = fmt.Sprintf("/api/v6/dex/aggregator/quote?chainIndex=%s&amount=%s&fromTokenAddress=%s&toTokenAddress=%s&swapMode=exactIn&priceImpactProtectionPercent=90",
			tc.ChainIndex, amount, fromAddress, toAddress)
	} else {
		// USDT→Token：买入代币，得到ask价格
		fromAddress = usdtAddress
		toAddress = tc.Address
		fromDecimals = 6 // USDT精度
		toDecimals = tc.Decimals

		// 询价数量：200 USDT（固定数量）
		amount := "200000000" // 200 USDT with 6 decimals = 200 * 10^6
		path = fmt.Sprintf("/api/v6/dex/aggregator/quote?chainIndex=%s&amount=%s&fromTokenAddress=%s&toTokenAddress=%s&swapMode=exactIn&priceImpactProtectionPercent=90",
			tc.ChainIndex, amount, fromAddress, toAddress)
	}

	// 执行HTTP请求
	data, err := w.doRequest("GET", path, "")
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	// 解析响应
	var quoteResp QuoteResponse
	if err := json.Unmarshal(data, &quoteResp); err != nil {
		return nil, fmt.Errorf("parse response failed: %w", err)
	}

	if quoteResp.Code != "0" {
		return nil, fmt.Errorf("API error: %s - %s", quoteResp.Code, quoteResp.Msg)
	}

	if len(quoteResp.Data) == 0 {
		return nil, fmt.Errorf("no quote data in response")
	}

	// 从Quote结果计算价格
	quoteData := quoteResp.Data[0]

	// 检查是否有路由（可选，因为即使没有路由，fromTokenAmount和toTokenAmount也应该存在）
	if len(quoteData.DexRouterList) == 0 {
		log.Printf("[OKX Worker %d] Warning: no route found for %s, but will try to use amounts", w.ID, tc.Symbol)
	}

	fromAmount, err := strconv.ParseFloat(quoteData.FromTokenAmount, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid fromTokenAmount: %s", quoteData.FromTokenAmount)
	}

	toAmount, err := strconv.ParseFloat(quoteData.ToTokenAmount, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid toTokenAmount: %s", quoteData.ToTokenAmount)
	}

	if fromAmount == 0 || toAmount == 0 {
		return nil, fmt.Errorf("invalid amounts in quote response")
	}

	// 计算价格（以USDT计价）
	var priceUSD float64
	if direction == DirectionTokenToUSDT {
		// Token→USDT：卖出代币得到USDT
		// 价格 = toAmount USDT / fromAmount Token
		actualFromAmount := fromAmount / pow10(fromDecimals)
		actualToAmount := toAmount / pow10(toDecimals)
		priceUSD = actualToAmount / actualFromAmount

		log.Printf("[OKX Worker %d] %s bid: %s tokens (%.6f) -> %s USDT (%.6f) = $%.6f",
			w.ID, tc.Symbol, quoteData.FromTokenAmount, actualFromAmount,
			quoteData.ToTokenAmount, actualToAmount, priceUSD)
	} else {
		// USDT→Token：花费USDT买入代币
		// 价格 = fromAmount USDT / toAmount Token
		actualFromAmount := fromAmount / pow10(fromDecimals)
		actualToAmount := toAmount / pow10(toDecimals)
		priceUSD = actualFromAmount / actualToAmount

		log.Printf("[OKX Worker %d] %s ask: %s USDT (%.6f) -> %s tokens (%.6f) = $%.6f",
			w.ID, tc.Symbol, quoteData.FromTokenAmount, actualFromAmount,
			quoteData.ToTokenAmount, actualToAmount, priceUSD)
	}

	// 转换为通用价格格式
	price := ConvertToCommonPrice(tc, priceUSD, direction)

	return price, nil
}

// pow10 计算10的n次方
func pow10(n int) float64 {
	result := 1.0
	for i := 0; i < n; i++ {
		result *= 10
	}
	return result
}

// doRequest 执行HTTP请求（带签名认证）
func (w *KeyWorker) doRequest(method, path string, body string) ([]byte, error) {
	log.Printf("[OKX WorkerRequest] %s path: %s body (%s) api %s",
		method, path, body, w.APIConfig.APIKey)
	// 生成时间戳 (ISO 8601 UTC)
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// 生成签名
	message := timestamp + method + path + body
	h := hmac.New(sha256.New, []byte(w.APIConfig.SecretKey))
	h.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// 构建完整URL
	url := BaseURL + path

	// 创建请求
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request failed: %w", err)
	}

	// 设置认证头
	req.Header.Set("OK-ACCESS-KEY", w.APIConfig.APIKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", w.APIConfig.Passphrase)
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	resp, err := w.httpClient.Do(req)
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
	w.APIConfig.LastUsed = time.Now()

	return data, nil
}

// Close 关闭Worker
func (w *KeyWorker) Close() {
	close(w.ResultChan)
	if w.RateLimiter != nil {
		w.RateLimiter.Stop()
	}
}
