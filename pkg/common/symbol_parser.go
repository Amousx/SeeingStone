package common

import "strings"

// SymbolInfo 解析后的symbol信息
type SymbolInfo struct {
	OriginalSymbol string        // 原始symbol (如 ETHUSDC, LITUSDT)
	BaseAsset      string        // 基础资产 (如 ETH, LIT)
	QuoteAsset     QuoteCurrency // 报价货币 (如 USDC, USDT)
}

// ParseSymbol 解析symbol,提取base asset和quote currency
// 按后缀长度从长到短匹配,避免FDUSD被误识别为USDT
func ParseSymbol(symbol string) *SymbolInfo {
	symbol = strings.ToUpper(symbol)

	// 按长度从长到短尝试匹配quote currencies
	// 顺序重要: FDUSD(5字符) > USDT/USDC/USDE(4字符)
	quoteCurrencies := []QuoteCurrency{
		QuoteCurrencyFDUSD, // 5字符,先匹配
		QuoteCurrencyUSDT,  // 4字符
		QuoteCurrencyUSDC,  // 4字符
		QuoteCurrencyUSDE,  // 4字符
	}

	for _, qc := range quoteCurrencies {
		quoteSuffix := string(qc)
		if strings.HasSuffix(symbol, quoteSuffix) {
			baseAsset := symbol[:len(symbol)-len(quoteSuffix)]
			if baseAsset != "" {
				return &SymbolInfo{
					OriginalSymbol: symbol,
					BaseAsset:      baseAsset,
					QuoteAsset:     qc,
				}
			}
		}
	}

	// 默认假设是USDT (兼容现有逻辑,大部分交易对都是USDT)
	// 如果symbol长度>4,尝试去掉USDT后缀
	baseAsset := symbol
	if len(symbol) > 4 {
		baseAsset = strings.TrimSuffix(symbol, "USDT")
	}

	return &SymbolInfo{
		OriginalSymbol: symbol,
		BaseAsset:      baseAsset,
		QuoteAsset:     QuoteCurrencyUSDT,
	}
}

// ToStandardSymbol 转换为标准symbol (总是使用USDT后缀)
func (si *SymbolInfo) ToStandardSymbol() string {
	return si.BaseAsset + "USDT"
}

// IsQuoteCurrencySupported 检查quote currency是否被支持
func (si *SymbolInfo) IsQuoteCurrencySupported() bool {
	return si.QuoteAsset.IsStablecoin()
}
