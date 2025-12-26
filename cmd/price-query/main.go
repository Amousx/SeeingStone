package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// APIPrice API 返回的价格结构
type APIPrice struct {
	Symbol      string    `json:"symbol"`
	Exchange    string    `json:"exchange"`
	MarketType  string    `json:"market_type"`
	Price       float64   `json:"price"`
	BidPrice    float64   `json:"bid_price"`
	AskPrice    float64   `json:"ask_price"`
	BidQty      float64   `json:"bid_qty"`
	AskQty      float64   `json:"ask_qty"`
	Volume24h   float64   `json:"volume_24h"`
	Timestamp   time.Time `json:"timestamp"`
	LastUpdated time.Time `json:"last_updated"`
	Source      string    `json:"source"`
}

// PriceDisplay 价格显示
type PriceDisplay struct {
	Exchange   string
	MarketType string
	BidPrice   float64
	AskPrice   float64
	BidQty     float64
	AskQty     float64
	Spread     float64
	Volume24h  float64
	Age        time.Duration
	Available  bool
}

func clearScreen() {
	switch runtime.GOOS {
	case "windows":
		cmd := exec.Command("cmd", "/c", "cls")
		cmd.Stdout = os.Stdout
		cmd.Run()
	default:
		cmd := exec.Command("clear")
		cmd.Stdout = os.Stdout
		cmd.Run()
	}
}

func formatPrice(num float64) string {
	if num == 0 {
		return "-"
	}
	// 价格统一显示 8 位小数，确保能看出差异
	return fmt.Sprintf("%.8f", num)
}

func formatQty(num float64) string {
	if num == 0 {
		return "-"
	}
	if num < 0.01 {
		return fmt.Sprintf("%.8f", num)
	} else if num < 1 {
		return fmt.Sprintf("%.6f", num)
	} else if num < 100 {
		return fmt.Sprintf("%.4f", num)
	} else {
		return fmt.Sprintf("%.2f", num)
	}
}

// fetchPricesFromAPI 从 HTTP API 获取价格数据
func fetchPricesFromAPI(symbol, apiURL string) (map[string]*APIPrice, error) {
	url := fmt.Sprintf("%s/api/prices/%s", apiURL, symbol)

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// 调试：显示原始响应
	if len(body) == 0 || string(body) == "null" || string(body) == "[]" {
		return nil, fmt.Errorf("API 返回空数据，主程序可能刚启动或未订阅此币种")
	}

	var prices []APIPrice
	if err := json.Unmarshal(body, &prices); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w (响应: %s)", err, string(body))
	}

	if len(prices) == 0 {
		return nil, fmt.Errorf("未找到价格数据，请等待 10-30 秒让主程序收集数据")
	}

	// 转换为 map，key 为 "exchange-marketType" (小写)
	result := make(map[string]*APIPrice)
	for i := range prices {
		key := fmt.Sprintf("%s-%s",
			strings.ToLower(prices[i].Exchange),
			strings.ToLower(prices[i].MarketType))
		result[key] = &prices[i]
	}

	return result, nil
}

func displayPrices(symbol, apiURL string) {
	clearScreen()

	fmt.Printf("\n")
	fmt.Printf("═══════════════════════════════════════════════════════════════════════════════════════════════════════\n")
	fmt.Printf("                              实时价格监控（本地缓存） - %s\n", symbol)
	fmt.Printf("═══════════════════════════════════════════════════════════════════════════════════════════════════════\n")
	fmt.Printf("\n")

	// 从 API 获取数据
	pricesMap, err := fetchPricesFromAPI(symbol, apiURL)
	if err != nil {
		fmt.Printf("  ⚠️  无法获取价格数据: %v\n", err)
		fmt.Printf("\n")
		fmt.Printf("  提示：请确保主监控程序正在运行并监听 %s\n", apiURL)
		fmt.Printf("\n")
		fmt.Printf("═══════════════════════════════════════════════════════════════════════════════════════════════════════\n")
		return
	}

	// 定义要显示的交易所和市场类型
	displayConfigs := []struct {
		key     string
		name    string
		typeStr string
	}{
		{"binance-spot", "Binance", "现货"},
		{"binance-future", "Binance", "合约"},
		{"aster-future", "Aster", "合约"},
		{"lighter-future", "Lighter", "合约"},
	}

	var displays []*PriceDisplay

	for _, cfg := range displayConfigs {
		price, exists := pricesMap[cfg.key]

		if !exists || price == nil {
			displays = append(displays, &PriceDisplay{
				Exchange:   cfg.name,
				MarketType: cfg.typeStr,
				Available:  false,
			})
			continue
		}

		spread := 0.0
		if price.AskPrice > 0 && price.BidPrice > 0 {
			spread = ((price.AskPrice - price.BidPrice) / price.BidPrice) * 100
		}

		age := time.Since(price.LastUpdated)

		displays = append(displays, &PriceDisplay{
			Exchange:   cfg.name,
			MarketType: cfg.typeStr,
			BidPrice:   price.BidPrice,
			AskPrice:   price.AskPrice,
			BidQty:     price.BidQty,
			AskQty:     price.AskQty,
			Spread:     spread,
			Volume24h:  price.Volume24h,
			Age:        age,
			Available:  true,
		})
	}

	// 检查是否有任何数据
	hasData := false
	for _, d := range displays {
		if d.Available {
			hasData = true
			break
		}
	}

	if !hasData {
		fmt.Printf("  ⚠️  本地缓存中未找到 %s 的价格数据\n", symbol)
		fmt.Printf("\n")
		fmt.Printf("  提示：请确保主监控程序正在运行 (run_with_proxy.bat)\n")
		fmt.Printf("\n")
		fmt.Printf("═══════════════════════════════════════════════════════════════════════════════════════════════════════\n")
		return
	}

	// 表头
	fmt.Printf("%-15s %-10s %20s %20s %13s %13s %10s %10s\n",
		"交易所", "市场", "买价(Bid)", "卖价(Ask)", "买量", "卖量", "价差%", "更新")
	fmt.Printf("───────────────────────────────────────────────────────────────────────────────────────────────────────\n")

	// 显示数据
	for _, d := range displays {
		if !d.Available {
			fmt.Printf("%-15s %-10s %20s %20s %13s %13s %10s %10s\n",
				d.Exchange, d.MarketType, "-", "-", "-", "-", "-", "-")
			continue
		}

		// 数据新鲜度指示器
		ageIndicator := "●" // 新鲜
		if d.Age > 10*time.Second {
			ageIndicator = "◐" // 一般
		}
		if d.Age > 30*time.Second {
			ageIndicator = "○" // 陈旧
		}

		ageStr := fmt.Sprintf("%s %.0fs", ageIndicator, d.Age.Seconds())

		fmt.Printf("%-15s %-10s %20s %20s %13s %13s %9.3f%% %10s\n",
			d.Exchange,
			d.MarketType,
			formatPrice(d.BidPrice),
			formatPrice(d.AskPrice),
			formatQty(d.BidQty),
			formatQty(d.AskQty),
			d.Spread,
			ageStr,
		)
	}

	// 计算套利机会
	fmt.Printf("\n")
	fmt.Printf("─────────────────────── 套利机会分析 ───────────────────────────────────\n")

	var validPrices []*PriceDisplay
	for _, d := range displays {
		if d.Available && d.BidPrice > 0 && d.AskPrice > 0 {
			validPrices = append(validPrices, d)
		}
	}

	if len(validPrices) >= 2 {
		// 找出最高 bid 和最低 ask
		var maxBid, minAsk *PriceDisplay
		for _, p := range validPrices {
			if maxBid == nil || p.BidPrice > maxBid.BidPrice {
				maxBid = p
			}
			if minAsk == nil || p.AskPrice < minAsk.AskPrice {
				minAsk = p
			}
		}

		if maxBid != nil && minAsk != nil && maxBid.BidPrice > minAsk.AskPrice {
			profit := ((maxBid.BidPrice - minAsk.AskPrice) / minAsk.AskPrice) * 100
			priceDiff := maxBid.BidPrice - minAsk.AskPrice
			fmt.Printf("\n")
			fmt.Printf("  🔥 发现套利机会！\n")
			fmt.Printf("     在 %s %s 买入: %s\n", minAsk.Exchange, minAsk.MarketType, formatPrice(minAsk.AskPrice))
			fmt.Printf("     在 %s %s 卖出: %s\n", maxBid.Exchange, maxBid.MarketType, formatPrice(maxBid.BidPrice))
			fmt.Printf("     价格差: %s (%.6f%%)\n", formatPrice(priceDiff), profit)
			fmt.Printf("\n")
		} else {
			fmt.Printf("\n  暂无明显套利机会\n\n")
		}
	} else {
		fmt.Printf("\n  数据不足，无法计算套利机会\n\n")
	}

	// 统计信息
	fmt.Printf("═══════════════════════════════════════════════════════════════════════════════════════════════════════\n")
	fmt.Printf("数据新鲜度: ● <10s  ◐ 10-30s  ○ >30s  |  刷新时间: %s\n",
		time.Now().Format("2006-01-02 15:04:05"))
	fmt.Printf("═══════════════════════════════════════════════════════════════════════════════════════════════════════\n")
	fmt.Printf("按 Ctrl+C 退出\n")
	fmt.Printf("═══════════════════════════════════════════════════════════════════════════════════════════════════════\n")
}

func main() {
	// 解析命令行参数
	symbol := flag.String("symbol", "ETHUSDT", "要查询的币种符号，如 BTCUSDT, ETHUSDT")
	refresh := flag.Int("refresh", 500, "刷新间隔(毫秒)")
	apiURL := flag.String("api", "http://localhost:8080", "API 服务器地址")
	flag.Parse()

	// 标准化符号（转大写）
	*symbol = strings.ToUpper(*symbol)

	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("   实时价格监控工具（本地缓存查询）\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("\n")
	fmt.Printf("  查询币种: %s\n", *symbol)
	fmt.Printf("  刷新间隔: %d ms\n", *refresh)
	fmt.Printf("  API 地址: %s\n", *apiURL)
	fmt.Printf("\n")
	fmt.Printf("  💡 提示：请确保主监控程序正在运行\n")
	fmt.Printf("     运行: run_with_proxy.bat\n")
	fmt.Printf("\n")
	fmt.Printf("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	fmt.Printf("\n")
	fmt.Printf("正在连接 API 服务器...\n")

	// 测试 API 连接
	testURL := fmt.Sprintf("%s/api/prices/%s", *apiURL, *symbol)
	_, err := http.Get(testURL)
	if err != nil {
		fmt.Printf("\n")
		fmt.Printf("⚠️  无法连接到 API 服务器: %v\n", err)
		fmt.Printf("\n")
		fmt.Printf("请检查：\n")
		fmt.Printf("  1. 主监控程序是否正在运行\n")
		fmt.Printf("  2. API 地址是否正确: %s\n", *apiURL)
		fmt.Printf("\n")
		os.Exit(1)
	}

	fmt.Printf("✓ API 连接成功\n")
	fmt.Printf("\n")
	time.Sleep(1 * time.Second)

	// 启动定期刷新显示
	ticker := time.NewTicker(time.Duration(*refresh) * time.Millisecond)
	defer ticker.Stop()

	// 监听退出信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// 先显示一次
	displayPrices(*symbol, *apiURL)

	// 主循环
	for {
		select {
		case <-sigChan:
			fmt.Printf("\n正在退出...\n")
			return
		case <-ticker.C:
			displayPrices(*symbol, *apiURL)
		}
	}
}
