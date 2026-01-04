package okx

import (
	"log"
	"sync"
	"time"
)

// TokenUpdateStats 代币更新统计
type TokenUpdateStats struct {
	Symbol           string
	TotalUpdates     int64              // 总更新次数
	SuccessUpdates   int64              // 成功次数
	FailedUpdates    int64              // 失败次数
	PartialUpdates   int64              // 部分成功次数（只有bid或ask）
	LastUpdateTime   time.Time          // 最后更新时间
	AvgTimeDiff      time.Duration      // 平均bid-ask时间差
	MaxTimeDiff      time.Duration      // 最大bid-ask时间差
	MinTimeDiff      time.Duration      // 最小bid-ask时间差
	ValidationErrors map[string]int64   // 验证错误统计（按类型）
}

// StatsManager 统计管理器
type StatsManager struct {
	mu    sync.RWMutex
	stats map[string]*TokenUpdateStats // key: symbol
}

// NewStatsManager 创建统计管理器
func NewStatsManager() *StatsManager {
	return &StatsManager{
		stats: make(map[string]*TokenUpdateStats),
	}
}

// RecordUpdate 记录一次更新
// symbol: 代币符号
// success: 是否成功
// partial: 是否部分成功（只有bid或ask）
// timeDiff: bid和ask的时间差
func (sm *StatsManager) RecordUpdate(
	symbol string,
	success bool,
	partial bool,
	timeDiff time.Duration,
) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stats, exists := sm.stats[symbol]
	if !exists {
		stats = &TokenUpdateStats{
			Symbol:           symbol,
			ValidationErrors: make(map[string]int64),
		}
		sm.stats[symbol] = stats
	}

	// 更新计数
	stats.TotalUpdates++
	if success {
		stats.SuccessUpdates++
	} else {
		stats.FailedUpdates++
	}
	if partial {
		stats.PartialUpdates++
	}

	stats.LastUpdateTime = time.Now()

	// 更新时间差统计（仅在有效时间差时）
	if timeDiff > 0 {
		if stats.AvgTimeDiff == 0 {
			// 首次记录，直接使用当前值
			stats.AvgTimeDiff = timeDiff
		} else {
			// 使用移动平均（权重9:1，平滑历史数据）
			// newAvg = (oldAvg * 9 + newValue * 1) / 10
			stats.AvgTimeDiff = (stats.AvgTimeDiff*9 + timeDiff) / 10
		}

		// 更新最大值
		if timeDiff > stats.MaxTimeDiff {
			stats.MaxTimeDiff = timeDiff
		}

		// 更新最小值
		if stats.MinTimeDiff == 0 || timeDiff < stats.MinTimeDiff {
			stats.MinTimeDiff = timeDiff
		}
	}
}

// RecordValidationError 记录验证错误
// symbol: 代币符号
// errorType: 错误类型（如 "spread", "price_change", "timestamp"）
// err: 错误对象（用于日志）
func (sm *StatsManager) RecordValidationError(symbol, errorType string, err error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	stats, exists := sm.stats[symbol]
	if !exists {
		stats = &TokenUpdateStats{
			Symbol:           symbol,
			ValidationErrors: make(map[string]int64),
		}
		sm.stats[symbol] = stats
	}

	// 初始化验证错误map（如果需要）
	if stats.ValidationErrors == nil {
		stats.ValidationErrors = make(map[string]int64)
	}

	// 增加错误计数
	stats.ValidationErrors[errorType]++

	log.Printf("[OKX Stats] Validation error for %s (%s): %v", symbol, errorType, err)
}

// GetStats 获取指定代币的统计信息
// 返回nil如果该代币没有统计数据
func (sm *StatsManager) GetStats(symbol string) *TokenUpdateStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats, exists := sm.stats[symbol]
	if !exists {
		return nil
	}

	// 返回副本，避免外部修改
	statsCopy := *stats
	// 深拷贝ValidationErrors
	statsCopy.ValidationErrors = make(map[string]int64)
	for k, v := range stats.ValidationErrors {
		statsCopy.ValidationErrors[k] = v
	}

	return &statsCopy
}

// GetAllStats 获取所有代币的统计信息
func (sm *StatsManager) GetAllStats() map[string]*TokenUpdateStats {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	result := make(map[string]*TokenUpdateStats, len(sm.stats))
	for symbol, stats := range sm.stats {
		// 返回副本，避免外部修改
		statsCopy := *stats
		// 深拷贝ValidationErrors
		statsCopy.ValidationErrors = make(map[string]int64)
		for k, v := range stats.ValidationErrors {
			statsCopy.ValidationErrors[k] = v
		}
		result[symbol] = &statsCopy
	}

	return result
}

// ResetStats 重置指定代币的统计数据（可选，用于定期清理）
func (sm *StatsManager) ResetStats(symbol string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.stats, symbol)
	log.Printf("[OKX Stats] Reset statistics for %s", symbol)
}

// ResetAllStats 重置所有统计数据
func (sm *StatsManager) ResetAllStats() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.stats = make(map[string]*TokenUpdateStats)
	log.Println("[OKX Stats] Reset all statistics")
}

// PrintSummary 打印统计摘要（用于日志）
func (sm *StatsManager) PrintSummary() {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	if len(sm.stats) == 0 {
		log.Println("[OKX Stats] No statistics available")
		return
	}

	log.Println("[OKX Stats] Update Statistics Summary:")
	for symbol, stats := range sm.stats {
		successRate := float64(0)
		if stats.TotalUpdates > 0 {
			successRate = float64(stats.SuccessUpdates) / float64(stats.TotalUpdates) * 100
		}

		log.Printf("  %s: updates=%d, success=%d (%.1f%%), failed=%d, partial=%d, avg_diff=%.0fms, max_diff=%.0fms, min_diff=%.0fms",
			symbol,
			stats.TotalUpdates,
			stats.SuccessUpdates,
			successRate,
			stats.FailedUpdates,
			stats.PartialUpdates,
			stats.AvgTimeDiff.Seconds()*1000,
			stats.MaxTimeDiff.Seconds()*1000,
			stats.MinTimeDiff.Seconds()*1000,
		)

		// 打印验证错误（如果有）
		if len(stats.ValidationErrors) > 0 {
			log.Printf("    Validation errors:")
			for errorType, count := range stats.ValidationErrors {
				log.Printf("      %s: %d", errorType, count)
			}
		}
	}
}

// GetSuccessRate 获取指定代币的成功率
func (sm *StatsManager) GetSuccessRate(symbol string) float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	stats, exists := sm.stats[symbol]
	if !exists || stats.TotalUpdates == 0 {
		return 0
	}

	return float64(stats.SuccessUpdates) / float64(stats.TotalUpdates) * 100
}

// GetOverallSuccessRate 获取所有代币的整体成功率
func (sm *StatsManager) GetOverallSuccessRate() float64 {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	totalUpdates := int64(0)
	totalSuccess := int64(0)

	for _, stats := range sm.stats {
		totalUpdates += stats.TotalUpdates
		totalSuccess += stats.SuccessUpdates
	}

	if totalUpdates == 0 {
		return 0
	}

	return float64(totalSuccess) / float64(totalUpdates) * 100
}
