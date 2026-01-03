package common

// QuoteCurrency 报价货币类型
type QuoteCurrency string

const (
	QuoteCurrencyUSDT  QuoteCurrency = "USDT"
	QuoteCurrencyUSDC  QuoteCurrency = "USDC"
	QuoteCurrencyUSDE  QuoteCurrency = "USDE"
	QuoteCurrencyFDUSD QuoteCurrency = "FDUSD"
)

var freeStableCoin = []QuoteCurrency{
	QuoteCurrencyUSDT,
	QuoteCurrencyUSDC,
}

// IsStablecoin 判断是否为稳定币
func (qc QuoteCurrency) IsStablecoin() bool {
	switch qc {
	case QuoteCurrencyUSDT, QuoteCurrencyUSDC, QuoteCurrencyUSDE, QuoteCurrencyFDUSD:
		return true
	default:
		return false
	}
}

func IsFreeStablecoin(symbol QuoteCurrency) bool {
	for _, coin := range freeStableCoin {
		if coin == symbol {
			return true
		}
	}
	return false
}

// ToUSDTPair 生成到USDT的交易对symbol
// 例如: USDC -> "USDCUSDT", USDE -> "USDEUSDT"
// USDT返回空字符串（不需要转换）
func (qc QuoteCurrency) ToUSDTPair() string {
	if qc == QuoteCurrencyUSDT {
		return "" // USDT不需要转换
	}
	return string(qc) + "USDT"
}

// String 实现Stringer接口
func (qc QuoteCurrency) String() string {
	return string(qc)
}
