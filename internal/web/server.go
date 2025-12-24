package web

import (
	"crypto-arbitrage-monitor/internal/pricestore"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"sort"
	"strconv"
	"time"
)

//go:embed static
var staticFS embed.FS

// Server Web服务器
type Server struct {
	store *pricestore.PriceStore
	addr  string
}

// NewServer 创建新的Web服务器
func NewServer(store *pricestore.PriceStore, addr string) *Server {
	return &Server{
		store: store,
		addr:  addr,
	}
}

// Start 启动服务器
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/spreads", s.handleSpreads)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/custom-strategies", s.handleCustomStrategies)
	mux.HandleFunc("/api/arbitrage-opportunities", s.handleArbitrageOpportunities)

	// Static files - 使用子文件系统来正确访问 static 目录
	staticDir, err := fs.Sub(staticFS, "static")
	if err != nil {
		log.Fatal(err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticDir)))

	log.Printf("[Web Server] Starting on %s", s.addr)
	return http.ListenAndServe(s.addr, s.corsMiddleware(mux))
}

// corsMiddleware 添加CORS支持
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleSpreads 处理价差查询请求
// 支持参数:
// - sort: spread|volume|symbol (默认spread)
// - order: asc|desc (默认desc)
// - min_volume: 最小volume过滤
// - min_spread: 最小价差百分比过滤
// - limit: 限制返回数量
func (s *Server) handleSpreads(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 解析查询参数
	query := r.URL.Query()
	sortBy := query.Get("sort")
	if sortBy == "" {
		sortBy = "spread"
	}

	order := query.Get("order")
	if order == "" {
		order = "desc"
	}

	minVolume := parseFloat(query.Get("min_volume"), 0)
	minSpread := parseFloat(query.Get("min_spread"), -999999)
	limit := parseInt(query.Get("limit"), 0)

	// 计算价差
	spreads := s.store.CalculateSpreads()

	// 过滤
	filtered := make([]*pricestore.Spread, 0)
	for _, spread := range spreads {
		// 过滤掉价差大于100%的无效币对
		if spread.Volume24h >= minVolume && spread.SpreadPercent >= minSpread && spread.SpreadPercent <= 100.0 {
			filtered = append(filtered, spread)
		}
	}

	// 排序
	s.sortSpreads(filtered, sortBy, order)

	// 限制数量
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	// 返回JSON
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"count":   len(filtered),
		"data":    filtered,
	})
}

// handleStats 处理统计信息请求
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.store.GetStats()
	activePrices := len(s.store.GetActivePrices(60 * time.Second))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{
			"total_prices":    stats.TotalPrices,
			"active_prices":   activePrices,
			"total_symbols":   stats.TotalSymbols,
			"total_exchanges": stats.TotalExchanges,
			"by_exchange":     stats.ByExchange,
		},
	})
}

// handleCustomStrategies 处理自定义策略请求
func (s *Server) handleCustomStrategies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	strategies := s.store.CalculateCustomStrategies()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"count":   len(strategies),
		"data":    strategies,
	})
}

// handleArbitrageOpportunities 处理套利机会请求
func (s *Server) handleArbitrageOpportunities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	opportunities := s.store.GetArbitrageOpportunities()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"count":   len(opportunities),
		"data":    opportunities,
	})
}

// sortSpreads 排序价差列表
func (s *Server) sortSpreads(spreads []*pricestore.Spread, sortBy, order string) {
	sort.Slice(spreads, func(i, j int) bool {
		var less bool
		switch sortBy {
		case "volume":
			less = spreads[i].Volume24h < spreads[j].Volume24h
		case "symbol":
			less = spreads[i].Symbol < spreads[j].Symbol
		case "spread":
			fallthrough
		default:
			less = spreads[i].SpreadPercent < spreads[j].SpreadPercent
		}

		if order == "asc" {
			return less
		}
		return !less
	})
}

// parseFloat 解析浮点数，失败返回默认值
func parseFloat(s string, defaultValue float64) float64 {
	if s == "" {
		return defaultValue
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return defaultValue
	}
	return f
}

// parseInt 解析整数，失败返回默认值
func parseInt(s string, defaultValue int) int {
	if s == "" {
		return defaultValue
	}
	i, err := strconv.Atoi(s)
	if err != nil {
		return defaultValue
	}
	return i
}
