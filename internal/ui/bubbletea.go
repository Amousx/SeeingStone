package ui

import (
	"crypto-arbitrage-monitor/internal/arbitrage"
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
}

// OpportunityGetter 获取套利机会的接口
type OpportunityGetter interface {
	GetOpportunities() []*common.ArbitrageOpportunity
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
		{Title: "Buy From (LOW)", Width: 35},
		{Title: "Sell To (HIGH)", Width: 35},
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

		case "r":
			// 刷新
			m.lastUpdate = time.Now()

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
		m.lastUpdate = time.Now()
		// 从calculator获取最新的套利机会
		if m.calculator != nil {
			m.opportunities = m.calculator.GetOpportunities()
			m.updateTable()
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

	filtered := arbitrage.FilterOpportunities(m.opportunities, m.filterType)
	stats := fmt.Sprintf(
		"Total: %d | Showing: %d | Sort: %s %s | Filter: %s | Last Update: %s",
		len(m.opportunities),
		len(filtered),
		m.sortBy,
		m.getSortDirectionSymbol(),
		m.filterType,
		m.lastUpdate.Format("15:04:05"),
	)
	b.WriteString(statsStyle.Render(stats))
	b.WriteString("\n\n")

	// 表格
	b.WriteString(m.table.View())
	b.WriteString("\n\n")

	// 帮助信息
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))
	help := "Keys: [s] Sort Field | [d] Sort Direction | [f] Filter | [r] Refresh | [q] Quit"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

// updateTable 更新表格
func (m *Model) updateTable() {
	// 过滤
	filtered := arbitrage.FilterOpportunities(m.opportunities, m.filterType)

	// 排序
	sorted := arbitrage.SortOpportunities(filtered, m.sortBy, m.sortDesc)

	// 转换为表格行
	rows := make([]table.Row, 0, len(sorted))
	for _, opp := range sorted {
		// 添加价格和方向提示 (LOW表示买入低价, HIGH表示卖出高价)
		buyFrom := fmt.Sprintf("LOW %s %s @%.4f", opp.Exchange1, opp.Market1Type, opp.Price1)
		sellTo := fmt.Sprintf("HIGH %s %s @%.4f", opp.Exchange2, opp.Market2Type, opp.Price2)

		row := table.Row{
			opp.Symbol,
			opp.Type,
			buyFrom,
			sellTo,
			fmt.Sprintf("%.2f%%", opp.SpreadPercent),
			fmt.Sprintf("$%.2f", opp.ProfitPotential),
			fmt.Sprintf("%.0f", opp.Volume24h),
		}
		rows = append(rows, row)
	}

	m.table.SetRows(rows)
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
