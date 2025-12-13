package config

import (
	"os"
	"strconv"
	"strings"
)

// Config 应用配置
type Config struct {
	// Aster API配置
	AsterAPIKey        string
	AsterSecretKey     string
	AsterSpotBaseURL   string
	AsterFutureBaseURL string
	AsterWSSpotURL     string
	AsterWSFutureURL   string

	// Telegram配置
	TelegramBotToken string
	TelegramChatID   string

	// 监控配置
	MinSpreadPercent   float64  // 最小价差百分比，低于此值不通知
	UpdateInterval     int      // 更新间隔(秒)
	MonitorSymbols     []string // 监控的交易对
	EnableNotification bool     // 是否启用Telegram通知

	// 性能配置
	MaxGoroutines int // 最大并发数
}

// LoadConfig 加载配置
func LoadConfig() *Config {
	cfg := &Config{
		// Aster 默认配置
		AsterSpotBaseURL:   getEnv("ASTER_SPOT_BASE_URL", "https://sapi.asterdex.com"),
		AsterFutureBaseURL: getEnv("ASTER_FUTURE_BASE_URL", "https://fapi.asterdex.com"),
		AsterWSSpotURL:     getEnv("ASTER_WS_SPOT_URL", "wss://sstream.asterdex.com"),
		AsterWSFutureURL:   getEnv("ASTER_WS_FUTURE_URL", "wss://fstream.asterdex.com"),
		AsterAPIKey:        getEnv("ASTER_API_KEY", ""),
		AsterSecretKey:     getEnv("ASTER_SECRET_KEY", ""),

		// Telegram 配置
		TelegramBotToken: getEnv("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:   getEnv("TELEGRAM_CHAT_ID", ""),

		// 监控配置
		MinSpreadPercent:   getEnvFloat("MIN_SPREAD_PERCENT", 0.1), // 降低最小价差到0.1%以显示更多机会
		UpdateInterval:     getEnvInt("UPDATE_INTERVAL", 1),
		MonitorSymbols:     getEnvArray("MONITOR_SYMBOLS", []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}),
		EnableNotification: getEnvBool("ENABLE_NOTIFICATION", false), // 默认关闭通知避免误发

		// 性能配置
		MaxGoroutines: getEnvInt("MAX_GOROUTINES", 100),
	}

	return cfg
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvFloat(key string, defaultValue float64) float64 {
	if value := os.Getenv(key); value != "" {
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			return floatVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolVal, err := strconv.ParseBool(value); err == nil {
			return boolVal
		}
	}
	return defaultValue
}

func getEnvArray(key string, defaultValue []string) []string {
	if value := os.Getenv(key); value != "" {
		return strings.Split(value, ",")
	}
	return defaultValue
}
