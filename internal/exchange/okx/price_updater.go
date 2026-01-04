package okx

import (
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TokenPriceUpdater 负责自动更新代币的DefaultPrice
// 从OKX Market Price API获取代币价格，更新TokenConfig.DefaultPrice
type TokenPriceUpdater struct {
	mu          sync.RWMutex
	client      *Client
	tokens      []*TokenConfig
	updateTimer *time.Ticker
	stopChan    chan struct{}
}

// NewTokenPriceUpdater 创建价格更新器
func NewTokenPriceUpdater(client *Client, tokens []*TokenConfig) *TokenPriceUpdater {
	if client == nil {
		log.Println("[OKX PriceUpdater] Warning: client is nil, price updater will not work")
		return nil
	}

	if len(tokens) == 0 {
		log.Println("[OKX PriceUpdater] Warning: no tokens provided")
		return nil
	}

	return &TokenPriceUpdater{
		client:   client,
		tokens:   tokens,
		stopChan: make(chan struct{}),
	}
}

// Start 启动自动更新（启动时立即更新一次，然后每4小时更新一次）
func (u *TokenPriceUpdater) Start() {
	if u == nil {
		log.Println("[OKX PriceUpdater] Cannot start: updater is nil")
		return
	}

	// 立即更新一次
	log.Println("[OKX PriceUpdater] Starting initial price update...")
	u.updateAllPrices()

	// 启动定时更新（每4小时）
	u.updateTimer = time.NewTicker(4 * time.Hour)
	go u.updateLoop()

	log.Println("[OKX PriceUpdater] Started with 4-hour update interval")
}

// Stop 停止自动更新
func (u *TokenPriceUpdater) Stop() {
	if u == nil {
		return
	}

	if u.updateTimer != nil {
		u.updateTimer.Stop()
		log.Println("[OKX PriceUpdater] Stopped")
	}

	if u.stopChan != nil {
		close(u.stopChan)
	}
}

// updateLoop 更新循环
func (u *TokenPriceUpdater) updateLoop() {
	for {
		select {
		case <-u.updateTimer.C:
			log.Println("[OKX PriceUpdater] Running scheduled price update...")
			u.updateAllPrices()
		case <-u.stopChan:
			return
		}
	}
}

// updateAllPrices 更新所有代币的DefaultPrice（使用批量请求）
func (u *TokenPriceUpdater) updateAllPrices() {
	// 添加recover防止panic导致程序崩溃
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[OKX PriceUpdater] Recovered from panic: %v", r)
		}
	}()

	if u == nil {
		log.Println("[OKX PriceUpdater] Cannot update: updater is nil")
		return
	}

	if u.client == nil {
		log.Println("[OKX PriceUpdater] Cannot update: client is nil")
		return
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	// 批量大小（每次请求的代币数量）
	const batchSize = 50

	updated := 0
	failed := 0
	totalTokens := len(u.tokens)

	if totalTokens == 0 {
		log.Println("[OKX PriceUpdater] No tokens to update")
		return
	}

	// 分批处理
	for i := 0; i < totalTokens; i += batchSize {
		end := i + batchSize
		if end > totalTokens {
			end = totalTokens
		}

		batch := u.tokens[i:end]

		// 构建批量请求
		requests := make([]*MarketPriceRequest, len(batch))
		for j, token := range batch {
			if token == nil {
				log.Printf("[OKX PriceUpdater] Warning: token at index %d is nil", i+j)
				continue
			}
			requests[j] = &MarketPriceRequest{
				ChainIndex:           token.ChainIndex,
				TokenContractAddress: token.Address,
			}
		}

		// 发送批量请求
		resp, err := u.client.GetMarketPriceBatch(requests)
		if err != nil {
			log.Printf("[OKX PriceUpdater] Batch request failed: %v", err)
			failed += len(batch)
			continue
		}

		if resp == nil {
			log.Printf("[OKX PriceUpdater] Batch response is nil")
			failed += len(batch)
			continue
		}

		// 创建地址到价格的映射（方便查找）
		priceMap := make(map[string]string)
		for _, data := range resp.Data {
			key := data.ChainIndex + ":" + strings.ToLower(data.TokenContractAddress)
			priceMap[key] = data.Price
		}

		// 更新代币价格
		for _, token := range batch {
			key := token.ChainIndex + ":" + strings.ToLower(token.Address)
			priceStr, exists := priceMap[key]

			if exists && priceStr != "" {
				price, err := strconv.ParseFloat(priceStr, 64)
				if err == nil && price > 0 {
					oldPrice := token.GetDefaultPrice()
					token.SetDefaultPrice(price)
					log.Printf("[OKX PriceUpdater] Updated %s: %.4f -> %.4f", token.Symbol, oldPrice, price)
					updated++
					continue
				}
			}

			// 更新失败
			log.Printf("[OKX PriceUpdater] Failed to update %s, keeping default: %.4f", token.Symbol, token.GetDefaultPrice())
			failed++
		}

		// 限速：避免API调用过快（OKX限制每秒1次请求）
		if end < totalTokens {
			time.Sleep(1100 * time.Millisecond)
		}
	}

	log.Printf("[OKX PriceUpdater] Update completed: %d updated, %d failed (total: %d)", updated, failed, totalTokens)
}

// GetDefaultPrice 获取指定代币的DefaultPrice（线程安全）
func (u *TokenPriceUpdater) GetDefaultPrice(symbol string) float64 {
	if u == nil {
		log.Printf("[OKX PriceUpdater] Cannot get price: updater is nil, using estimate for %s", symbol)
		return estimateDefaultPrice(symbol)
	}

	u.mu.RLock()
	defer u.mu.RUnlock()

	for _, token := range u.tokens {
		if token != nil && token.Symbol == symbol {
			return token.GetDefaultPrice()
		}
	}

	// 如果没找到，使用估算价格
	return estimateDefaultPrice(symbol)
}
