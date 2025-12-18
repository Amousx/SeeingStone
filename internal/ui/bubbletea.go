package ui

import (
	"crypto-arbitrage-monitor/pkg/common"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model Bubbletea模型
type Model struct {
	table         table.Model
	opportunities []*common.ArbitrageOpportunity
	sortBy        string // "spread", "profit", "volume", "time"
	sortDesc      bool
	filterType    string // "all", "spot-spot", "spot-future", "future-spot", "future-future"
	lastUpdate    time.Time
	width         int
	height        int
	calculator    OpportunityGetter // 添加calculator引用
	paused        bool              // 暂停刷新
	knownPairs    map[string]bool   // 记录曾经有过数据的币对组合
}

// OpportunityGetter 获取套利机会的接口
type OpportunityGetter interface {
	GetOpportunities() []*common.ArbitrageOpportunity
	GetAllPrices() []*common.Price
	GetAllSymbols() []string
}

// TickMsg 定时更新消息
type TickMsg time.Time

// UpdateOpportunitiesMsg 更新套利机会消息
type UpdateOpportunitiesMsg []*common.ArbitrageOpportunity

// NewModel 创建新模型
func NewModel(calc OpportunityGetter) Model {
	columns := []table.Column{
		{Title: "Symbol", Width: 15},
		{Title: "Type", Width: 16},
		{Title: "Buy From", Width: 35},
		{Title: "Sell To", Width: 35},
		{Title: "Spread %", Width: 12},
		{Title: "Profit $", Width: 14},
		{Title: "Volume", Width: 14},
	}

	t := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
		table.WithHeight(20),
	)

	s := table.DefaultStyles()
	s.Header = s.Header.
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Bold(true)
	s.Selected = s.Selected.
		Foreground(lipgloss.Color("229")).
		Background(lipgloss.Color("57")).
		Bold(false)
	t.SetStyles(s)

	return Model{
		table:         t,
		opportunities: make([]*common.ArbitrageOpportunity, 0),
		sortBy:        "spread",
		sortDesc:      true,
		filterType:    "all",
		lastUpdate:    time.Now(),
		calculator:    calc,
		paused:        false,
		knownPairs:    make(map[string]bool),
	}
}

// Init 初始化
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
	)
}

// Update 更新
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.table.SetHeight(msg.Height - 10)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case " ", "p":
			// 暂停/恢复刷新
			m.paused = !m.paused

		case "r":
			// 刷新
			m.lastUpdate = time.Now()
			if !m.paused {
				if m.calculator != nil {
					m.opportunities = m.calculator.GetOpportunities()
					m.updateTable()
				}
			}

		case "s":
			// 切换排序字段
			switch m.sortBy {
			case "spread":
				m.sortBy = "profit"
			case "profit":
				m.sortBy = "volume"
			case "volume":
				m.sortBy = "time"
			case "time":
				m.sortBy = "spread"
			}
			m.updateTable()

		case "d":
			// 切换排序方向
			m.sortDesc = !m.sortDesc
			m.updateTable()

		case "f":
			// 切换过滤类型
			switch m.filterType {
			case "all":
				m.filterType = "spot-spot"
			case "spot-spot":
				m.filterType = "spot-future"
			case "spot-future":
				m.filterType = "future-spot"
			case "future-spot":
				m.filterType = "future-future"
			case "future-future":
				m.filterType = "all"
			}
			m.updateTable()
		}

	case TickMsg:
		// 只在未暂停时刷新
		if !m.paused {
			m.lastUpdate = time.Now()
			// 从calculator获取最新的套利机会
			if m.calculator != nil {
				m.opportunities = m.calculator.GetOpportunities()
				m.updateTable()
			}
		}
		return m, tickCmd()

	case UpdateOpportunitiesMsg:
		m.opportunities = msg
		m.lastUpdate = time.Now()
		m.updateTable()
		return m, nil
	}

	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View 视图
func (m Model) View() string {
	var b strings.Builder

	// 标题
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		MarginBottom(1)
	b.WriteString(titleStyle.Render("Crypto Arbitrage Monitor"))
	b.WriteString("\n\n")

	// 统计信息
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	totalRows := len(m.table.Rows())
	pausedIndicator := ""
	if m.paused {
		pausedIndicator = " | ⏸ PAUSED"
	}
	stats := fmt.Sprintf(
		"Total Pairs: %d | Arbitrage Opportunities: %d | Sort: %s %s | Filter: %s | Last Update: %s%s",
		totalRows,
		len(m.opportunities),
		m.sortBy,
		m.getSortDirectionSymbol(),
		m.filterType,
		m.lastUpdate.Format("15:04:05"),
		pausedIndicator,
	)
	b.WriteString(statsStyle.Render(stats))
	b.WriteString("\n\n")

	// 表格
	b.WriteString(m.table.View())
	b.WriteString("\n\n")

	// 帮助信息
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
	help := "Keys: [Space/p] Pause | [s] Sort Field | [d] Sort Direction | [f] Filter | [r] Refresh | [q] Quit"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// updateTable 更新表格
func (m *Model) updateTable() {
	// 获取所有价格数据
	var allPrices []*common.Price
	var allSymbols []string
	if m.calculator != nil {
		allPrices = m.calculator.GetAllPrices()
		allSymbols = m.calculator.GetAllSymbols()
	}

	// 按 symbol 分组价格
	pricesBySymbol := make(map[string]map[string]*common.Price) // symbol -> key -> price
	for _, price := range allPrices {
		if _, exists := pricesBySymbol[price.Symbol]; !exists {
			pricesBySymbol[price.Symbol] = make(map[string]*common.Price)
		}
		key := fmt.Sprintf("%s_%s", price.Exchange, price.MarketType)
		pricesBySymbol[price.Symbol][key] = price
	}

	// 按 symbol 分组套利机会，便于快速查找
	oppsByKey := make(map[string]*common.ArbitrageOpportunity)
	for _, opp := range m.opportunities {
		key1 := fmt.Sprintf("%s_%s_%s_%s_%s", opp.Symbol, opp.Exchange1, opp.Market1Type, opp.Exchange2, opp.Market2Type)
		key2 := fmt.Sprintf("%s_%s_%s_%s_%s", opp.Symbol, opp.Exchange2, opp.Market2Type, opp.Exchange1, opp.Market1Type)
		oppsByKey[key1] = opp
		oppsByKey[key2] = opp
	}

	// 定义应该监控的交易所/市场组合
	monitoredSources := []struct {
		exchange   common.Exchange
		marketType common.MarketType
		key        string
	}{
		{common.ExchangeAster, common.MarketTypeSpot, "ASTER_SPOT"},
		{common.ExchangeAster, common.MarketTypeFuture, "ASTER_FUTURE"},
		{common.ExchangeLighter, common.MarketTypeFuture, "LIGHTER_FUTURE"},
	}

	// 生成所有可能的币对组合行
	rows := make([]table.Row, 0)

	// 对于每个 symbol，生成所有可能的交易所/市场组合
	for _, symbol := range allSymbols {
		prices := pricesBySymbol[symbol]
		if prices == nil {
			prices = make(map[string]*common.Price)
		}

		// 两两组合所有监控源
		for i := 0; i < len(monitoredSources); i++ {
			for j := i + 1; j < len(monitoredSources); j++ {
				src1 := monitoredSources[i]
				src2 := monitoredSources[j]

				key1 := fmt.Sprintf("%s_%s", src1.exchange, src1.marketType)
				key2 := fmt.Sprintf("%s_%s", src2.exchange, src2.marketType)

				price1 := prices[key1]
				price2 := prices[key2]

				// 确定市场类型字符串
				marketType := m.getMarketTypeString(src1.marketType, src2.marketType)

				// 应用过滤器
				if !m.shouldShowMarketType(marketType) {
					continue
				}

				// 生成 pair key，用于记录是否曾经有过数据
				pairKey := fmt.Sprintf("%s_%s_%s_%s_%s", symbol, src1.exchange, src1.marketType, src2.exchange, src2.marketType)

				// 如果两边都有价格数据，记录这个 pair
				if price1 != nil && price2 != nil {
					m.knownPairs[pairKey] = true

					// 生成套利机会的 key
					oppKey := fmt.Sprintf("%s_%s_%s_%s_%s", symbol, src1.exchange, src1.marketType, src2.exchange, src2.marketType)

					if opp, exists := oppsByKey[oppKey]; exists {
						// 有套利机会，显示套利数据
						row := m.createOpportunityRow(opp, false)
						rows = append(rows, row)
					} else {
						// 没有套利机会，但有价格数据，显示价格，价差为 0
						row := m.createNoPriceSpreadRow(symbol, price1, price2, marketType, false)
						rows = append(rows, row)
					}
				} else if m.knownPairs[pairKey] {
					// 之前有过数据，但现在缺失了，显示空价格并标记为淡红色
					row := m.createEmptyRow(symbol, src1, src2, price1, price2, marketType)
					rows = append(rows, row)
				}
				// 如果从未有过数据，则不创建这个 pair
			}
		}
	}

	// 排序行
	rows = m.sortRows(rows)

	m.table.SetRows(rows)
}

// createOpportunityRow 创建套利机会行
func (m *Model) createOpportunityRow(opp *common.ArbitrageOpportunity, isMissing bool) table.Row {
	// 根据实际价格判断哪个是LOW，哪个是HIGH
	var buyFrom, sellTo string
	if opp.Price1 < opp.Price2 {
		buyFrom = fmt.Sprintf("LOW %s %s @%g", opp.Exchange1, opp.Market1Type, opp.Price1)
		sellTo = fmt.Sprintf("HIGH %s %s @%g", opp.Exchange2, opp.Market2Type, opp.Price2)
	} else {
		buyFrom = fmt.Sprintf("HIGH %s %s @%g", opp.Exchange1, opp.Market1Type, opp.Price1)
		sellTo = fmt.Sprintf("LOW %s %s @%g", opp.Exchange2, opp.Market2Type, opp.Price2)
	}

	return table.Row{
		opp.Symbol,
		opp.Type,
		buyFrom,
		sellTo,
		fmt.Sprintf("%.2f%%", opp.SpreadPercent),
		fmt.Sprintf("$%.2f", opp.ProfitPotential),
		fmt.Sprintf("%.0f", opp.Volume24h),
	}
}

// createNoPriceSpreadRow 创建无价差行（有价格但无套利机会）
func (m *Model) createNoPriceSpreadRow(symbol string, price1, price2 *common.Price, marketType string, isMissing bool) table.Row {
	// 根据价格确定买卖方向，从而确定正确的类型
	var from, to, actualType string

	if price1.AskPrice <= price2.BidPrice {
		// 买 price1，卖 price2
		from = fmt.Sprintf("BUY %s %s @%g", price1.Exchange, price1.MarketType, price1.AskPrice)
		to = fmt.Sprintf("SELL %s %s @%g", price2.Exchange, price2.MarketType, price2.BidPrice)
		actualType = fmt.Sprintf("%s-%s", strings.ToLower(string(price1.MarketType)), strings.ToLower(string(price2.MarketType)))
	} else {
		// 买 price2，卖 price1
		from = fmt.Sprintf("BUY %s %s @%g", price2.Exchange, price2.MarketType, price2.AskPrice)
		to = fmt.Sprintf("SELL %s %s @%g", price1.Exchange, price1.MarketType, price1.BidPrice)
		actualType = fmt.Sprintf("%s-%s", strings.ToLower(string(price2.MarketType)), strings.ToLower(string(price1.MarketType)))
	}

	return table.Row{
		symbol,
		actualType,
		from,
		to,
		"0.00%",
		"$0.00",
		fmt.Sprintf("%.0f", (price1.Volume24h + price2.Volume24h) / 2),
	}
}

// createEmptyRow 创建空行（无价格数据，使用淡红色标记）
func (m *Model) createEmptyRow(symbol string, src1, src2 struct {
	exchange   common.Exchange
	marketType common.MarketType
	key        string
}, price1, price2 *common.Price, marketType string) table.Row {
	// 淡红色样式
	missingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("203"))

	// 确定实际的类型（根据价格方向）
	var actualType string
	var from, to string

	if price1 != nil && price2 != nil {
		// 两边都有价格，根据价格方向确定类型
		if price1.AskPrice <= price2.BidPrice {
			from = fmt.Sprintf("BUY %s %s @%g", price1.Exchange, price1.MarketType, price1.AskPrice)
			to = fmt.Sprintf("SELL %s %s @%g", price2.Exchange, price2.MarketType, price2.BidPrice)
			actualType = fmt.Sprintf("%s-%s", strings.ToLower(string(price1.MarketType)), strings.ToLower(string(price2.MarketType)))
		} else {
			from = fmt.Sprintf("BUY %s %s @%g", price2.Exchange, price2.MarketType, price2.AskPrice)
			to = fmt.Sprintf("SELL %s %s @%g", price1.Exchange, price1.MarketType, price1.BidPrice)
			actualType = fmt.Sprintf("%s-%s", strings.ToLower(string(price2.MarketType)), strings.ToLower(string(price1.MarketType)))
		}
	} else {
		// 至少有一边缺失，无法确定方向，使用传入的 marketType
		actualType = marketType

		if price1 != nil {
			from = fmt.Sprintf("%s %s @%g", price1.Exchange, price1.MarketType, price1.Price)
		} else {
			from = missingStyle.Render(fmt.Sprintf("%s %s @N/A", src1.exchange, src1.marketType))
		}

		if price2 != nil {
			to = fmt.Sprintf("%s %s @%g", price2.Exchange, price2.MarketType, price2.Price)
		} else {
			to = missingStyle.Render(fmt.Sprintf("%s %s @N/A", src2.exchange, src2.marketType))
		}
	}

	return table.Row{
		missingStyle.Render(symbol),
		missingStyle.Render(actualType),
		from,
		to,
		missingStyle.Render("0.00%"),
		missingStyle.Render("$0.00"),
		missingStyle.Render("N/A"),
	}
}

// getMarketTypeString 获取市场类型字符串
func (m *Model) getMarketTypeString(mt1, mt2 common.MarketType) string {
	return fmt.Sprintf("%s-%s", strings.ToLower(string(mt1)), strings.ToLower(string(mt2)))
}

// shouldShowOpportunity 判断是否应该显示该套利机会（根据过滤器）
func (m *Model) shouldShowOpportunity(opp *common.ArbitrageOpportunity) bool {
	if m.filterType == "all" {
		return true
	}
	return opp.Type == m.filterType
}

// shouldShowMarketType 判断是否应该显示该市场类型组合（根据过滤器）
func (m *Model) shouldShowMarketType(marketType string) bool {
	if m.filterType == "all" {
		return true
	}
	return marketType == m.filterType
}

// sortRows 排序行
func (m *Model) sortRows(rows []table.Row) []table.Row {
	if m.sortBy != "spread" {
		// 暂时只支持按 spread 排序
		return rows
	}

	// 按价差排序
	sortedRows := make([]table.Row, len(rows))
	copy(sortedRows, rows)

	// 简单的冒泡排序（对于小数据集足够了）
	for i := 0; i < len(sortedRows); i++ {
		for j := i + 1; j < len(sortedRows); j++ {
			// 提取价差值（去掉 % 符号）
			spread1 := m.extractSpreadValue(sortedRows[i][4])
			spread2 := m.extractSpreadValue(sortedRows[j][4])

			// 根据排序方向比较
			shouldSwap := false
			if m.sortDesc {
				shouldSwap = spread1 < spread2
			} else {
				shouldSwap = spread1 > spread2
			}

			if shouldSwap {
				sortedRows[i], sortedRows[j] = sortedRows[j], sortedRows[i]
			}
		}
	}

	return sortedRows
}

// extractSpreadValue 从字符串中提取价差值
func (m *Model) extractSpreadValue(spreadStr string) float64 {
	var value float64
	fmt.Sscanf(spreadStr, "%f%%", &value)
	return value
}

// getSortDirectionSymbol 获取排序方向符号
func (m Model) getSortDirectionSymbol() string {
	if m.sortDesc {
		return "↓"
	}
	return "↑"
}

// tickCmd 定时器命令
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// UpdateOpportunitiesCmd 更新套利机会命令
func UpdateOpportunitiesCmd(opportunities []*common.ArbitrageOpportunity) tea.Cmd {
	return func() tea.Msg {
		return UpdateOpportunitiesMsg(opportunities)
	}
}
