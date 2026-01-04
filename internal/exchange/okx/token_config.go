package okx

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
)

// TokenConfig 代币配置
type TokenConfig struct {
	Symbol       string         // 代币符号 (如 "USDC")
	ChainIndex   string         // 链ID (如 "1" 为 Ethereum)
	Address      string         // 合约地址
	Decimals     int            // 精度
	defaultPrice atomic.Uint64  // 默认价格（用atomic存储float64的位模式，避免数据竞争）
}

// GetDefaultPrice 原子读取DefaultPrice
func (tc *TokenConfig) GetDefaultPrice() float64 {
	bits := tc.defaultPrice.Load()
	return math.Float64frombits(bits)
}

// SetDefaultPrice 原子写入DefaultPrice
func (tc *TokenConfig) SetDefaultPrice(price float64) {
	bits := math.Float64bits(price)
	tc.defaultPrice.Store(bits)
}

// LoadTokenConfigs 从文件加载代币配置
// 文件格式：每行为 "Symbol,ChainIndex,Address,Decimals[,DefaultPrice]"
// 例如：USDC,1,0xa0b86991c6218b36c1d19d4a2e9eb0ce3606eb48,6,1.0
func LoadTokenConfigs(filePath string) ([]*TokenConfig, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open token config file failed: %w", err)
	}
	defer file.Close()

	configs := make([]*TokenConfig, 0)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// 解析：Symbol,ChainIndex,Address,Decimals[,DefaultPrice]
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			log.Printf("[OKX] Warning: line %d invalid format (expected 4-5 fields): %s", lineNum, line)
			continue
		}

		decimals, err := strconv.Atoi(strings.TrimSpace(parts[3]))
		if err != nil {
			log.Printf("[OKX] Warning: line %d invalid decimals: %s", lineNum, parts[3])
			continue
		}

		// 默认价格（可选）
		var defaultPrice float64 = 0
		if len(parts) >= 5 {
			if price, err := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64); err == nil {
				defaultPrice = price
			}
		}

		// 如果没有配置默认价格，使用启发式估算
		if defaultPrice == 0 {
			defaultPrice = estimateDefaultPrice(strings.TrimSpace(parts[0]))
		}

		config := &TokenConfig{
			Symbol:     strings.TrimSpace(parts[0]),
			ChainIndex: strings.TrimSpace(parts[1]),
			Address:    strings.ToLower(strings.TrimSpace(parts[2])), // 地址转小写
			Decimals:   decimals,
		}
		config.SetDefaultPrice(defaultPrice)

		configs = append(configs, config)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read token config file failed: %w", err)
	}

	log.Printf("[OKX] Loaded %d token configs from %s", len(configs), filePath)
	return configs, nil
}

// estimateDefaultPrice 估算代币的默认价格（用于计算询价数量）
func estimateDefaultPrice(symbol string) float64 {
	// 常见代币的大致价格
	priceEstimates := map[string]float64{
		"USDC":  1.0,
		"USDT":  1.0,
		"DAI":   1.0,
		"USDE":  1.0,
		"FDUSD": 1.0,
		"BUSD":  1.0,
		"BTC":   90000.0,
		"WBTC":  90000.0,
		"ETH":   3000.0,
		"WETH":  3000.0,
		"BNB":   600.0,
		"SOL":   130.0,
		"MATIC": 0.8,
		"AVAX":  35.0,
		"LINK":  15.0,
		"UNI":   8.0,
		"AAVE":  200.0,
	}

	if price, ok := priceEstimates[symbol]; ok {
		return price
	}

	// 默认假设为中等价格代币（约10 USDT）
	return 10.0
}

// Calculate200USDTAmount 计算价值约200 USDT的代币数量
// tokenPriceUSD: 代币的USD价格
// decimals: 代币精度
// 返回：包含精度的数量字符串
func Calculate200USDTAmount(tokenPriceUSD float64, decimals int) string {
	if tokenPriceUSD <= 0 {
		return "0"
	}

	// 200 USDT 能买多少代币
	tokenAmount := 200.0 / tokenPriceUSD

	// 转换为包含精度的整数
	// 例如：1.5 USDC (decimals=6) -> 1500000
	multiplier := new(big.Float).SetFloat64(tokenAmount)
	decimalsMultiplier := new(big.Float).SetInt(new(big.Int).Exp(
		big.NewInt(10),
		big.NewInt(int64(decimals)),
		nil,
	))

	result := new(big.Float).Mul(multiplier, decimalsMultiplier)

	// 转换为整数字符串
	intResult, _ := result.Int(nil)
	return intResult.String()
}

// GetUSDTAddress 获取USDT在指定链上的合约地址
func GetUSDTAddress(chainIndex string) string {
	// 常见链的USDT地址
	usdtAddresses := map[string]string{
		"1":     "0xdac17f958d2ee523a2206206994597c13d831ec7", // Ethereum
		"56":    "0x55d398326f99059ff775485246999027b3197955", // BSC
		"137":   "0xc2132d05d31c914a87c6611c10748aeb04b58e8f", // Polygon
		"42161": "0xfd086bc7cd5c481dcc9c85ebe478a1c0b69fcbb9", // Arbitrum
		"10":    "0x94b008aa00579c1307b0ef2c499ad98a8ce58e58", // Optimism
		"8453":  "0xfde4c96c8593536e31f229ea8f37b2ada2699bb2", // Base
	}

	if addr, ok := usdtAddresses[chainIndex]; ok {
		return strings.ToLower(addr)
	}

	// 默认返回Ethereum USDT
	return "0xdac17f958d2ee523a2206206994597c13d831ec7"
}
