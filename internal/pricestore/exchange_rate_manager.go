package pricestore

import (
	"crypto-arbitrage-monitor/pkg/common"
	"fmt"
	"sync"
	"time"
)

// ExchangeRate 汇率信息
type ExchangeRate struct {
	FromCurrency   common.QuoteCurrency
	ToCurrency     common.QuoteCurrency // 总是USDT
	Rate           float64              // 汇率 (如 USDC->USDT = 0.9998)
	Source         string               // 来源 (如 "BINANCE_USDCUSDT_ASK")
	LastUpdated    time.Time
	IsDefaultRate  bool // 是否为默认汇率1.0
}

// ExchangeRateManager 汇率管理器
type ExchangeRateManager struct {
	mu    sync.RWMutex
	rates map[common.QuoteCurrency]*ExchangeRate

	// 依赖的PriceStore (用于订阅币安汇率价格)
	priceStore *PriceStore

	// 防抖：避免过多更新goroutine
	updating sync.Mutex
}

// NewExchangeRateManager 创建汇率管理器
func NewExchangeRateManager(priceStore *PriceStore) *ExchangeRateManager {
	erm := &ExchangeRateManager{
		rates:      make(map[common.QuoteCurrency]*ExchangeRate),
		priceStore: priceStore,
	}

	// 初始化默认汇率
	erm.initDefaultRates()

	return erm
}

// initDefaultRates 初始化默认汇率 (全部为1.0)
func (erm *ExchangeRateManager) initDefaultRates() {
	quoteCurrencies := []common.QuoteCurrency{
		common.QuoteCurrencyUSDT,
		common.QuoteCurrencyUSDC,
		common.QuoteCurrencyUSDE,
		common.QuoteCurrencyFDUSD,
	}

	for _, qc := range quoteCurrencies {
		if qc == common.QuoteCurrencyUSDT {
			continue // USDT不需要汇率
		}

		erm.rates[qc] = &ExchangeRate{
			FromCurrency:  qc,
			ToCurrency:    common.QuoteCurrencyUSDT,
			Rate:          1.0,
			Source:        "DEFAULT",
			LastUpdated:   time.Now(),
			IsDefaultRate: true,
		}
	}
}

// UpdateFromBinance 从Binance价格更新汇率
// 在PriceStore更新价格后调用
func (erm *ExchangeRateManager) UpdateFromBinance() {
	// 防抖：如果已经有更新在进行，跳过本次更新
	if !erm.updating.TryLock() {
		return // 已经有goroutine在更新，跳过
	}
	defer erm.updating.Unlock()

	quoteCurrencies := []common.QuoteCurrency{
		common.QuoteCurrencyUSDC,
		common.QuoteCurrencyUSDE,
		common.QuoteCurrencyFDUSD,
	}

	// 先从PriceStore获取所有价格（避免在持有erm.mu锁时调用GetPrice，防止死锁）
	prices := make(map[common.QuoteCurrency]*common.Price)
	for _, qc := range quoteCurrencies {
		pairSymbol := qc.ToUSDTPair()
		if pairSymbol == "" {
			continue
		}

		// 从PriceStore获取币安现货价格
		price := erm.priceStore.GetPrice(
			common.ExchangeBinance,
			common.MarketTypeSpot,
			pairSymbol,
		)

		if price != nil && price.AskPrice > 0 {
			prices[qc] = price
		}
	}

	// 然后再加锁更新汇率
	erm.mu.Lock()
	defer erm.mu.Unlock()

	for qc, price := range prices {
		// 使用Ask价格作为汇率 (买入USDT的价格,即用X币换USDT的成本)
		erm.rates[qc] = &ExchangeRate{
			FromCurrency:  qc,
			ToCurrency:    common.QuoteCurrencyUSDT,
			Rate:          price.AskPrice,
			Source:        fmt.Sprintf("BINANCE_%s_ASK", price.Symbol),
			LastUpdated:   price.LastUpdated,
			IsDefaultRate: false,
		}
	}
}

// GetRate 获取汇率 (线程安全)
func (erm *ExchangeRateManager) GetRate(from common.QuoteCurrency) *ExchangeRate {
	erm.mu.RLock()
	defer erm.mu.RUnlock()

	if from == common.QuoteCurrencyUSDT {
		return &ExchangeRate{
			FromCurrency:  common.QuoteCurrencyUSDT,
			ToCurrency:    common.QuoteCurrencyUSDT,
			Rate:          1.0,
			Source:        "IDENTITY",
			LastUpdated:   time.Now(),
			IsDefaultRate: false,
		}
	}

	if rate, exists := erm.rates[from]; exists {
		// 返回副本,避免外部修改
		rateCopy := *rate
		return &rateCopy
	}

	// 回退到默认汇率1.0
	return &ExchangeRate{
		FromCurrency:  from,
		ToCurrency:    common.QuoteCurrencyUSDT,
		Rate:          1.0,
		Source:        "FALLBACK",
		LastUpdated:   time.Now(),
		IsDefaultRate: true,
	}
}

// GetAllRates 获取所有汇率快照
func (erm *ExchangeRateManager) GetAllRates() map[common.QuoteCurrency]*ExchangeRate {
	erm.mu.RLock()
	defer erm.mu.RUnlock()

	snapshot := make(map[common.QuoteCurrency]*ExchangeRate)
	for k, v := range erm.rates {
		// 复制值,避免外部修改
		rateCopy := *v
		snapshot[k] = &rateCopy
	}
	return snapshot
}
