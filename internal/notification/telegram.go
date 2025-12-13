package notification

import (
	"bytes"
	"crypto-arbitrage-monitor/pkg/common"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// TelegramNotifier Telegram通知器
type TelegramNotifier struct {
	BotToken   string
	ChatID     string
	HTTPClient *http.Client
	enabled    bool
}

// NewTelegramNotifier 创建Telegram通知器
func NewTelegramNotifier(botToken, chatID string, enabled bool) *TelegramNotifier {
	return &TelegramNotifier{
		BotToken: botToken,
		ChatID:   chatID,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		enabled: enabled,
	}
}

// SendOpportunity 发送套利机会通知
func (t *TelegramNotifier) SendOpportunity(opp *common.ArbitrageOpportunity) error {
	if !t.enabled {
		return nil
	}

	if t.BotToken == "" || t.ChatID == "" {
		return fmt.Errorf("telegram bot token or chat id not configured")
	}

	message := t.formatOpportunity(opp)
	return t.sendMessage(message)
}

// SendMessage 发送消息
func (t *TelegramNotifier) SendMessage(message string) error {
	if !t.enabled {
		return nil
	}

	if t.BotToken == "" || t.ChatID == "" {
		return fmt.Errorf("telegram bot token or chat id not configured")
	}

	return t.sendMessage(message)
}

// formatOpportunity 格式化套利机会消息
func (t *TelegramNotifier) formatOpportunity(opp *common.ArbitrageOpportunity) string {
	emoji := "🚀"
	if opp.SpreadPercent > 2.0 {
		emoji = "🔥🔥🔥"
	} else if opp.SpreadPercent > 1.0 {
		emoji = "🔥"
	}

	msg := fmt.Sprintf(`%s <b>套利机会</b>

<b>交易对:</b> %s
<b>类型:</b> %s
<b>价差:</b> %.2f%% (%.4f)

<b>买入:</b> %s %s @ %.4f
<b>卖出:</b> %s %s @ %.4f

<b>24h交易量:</b> %.2f
<b>预估利润:</b> $%.2f

<b>时间:</b> %s`,
		emoji,
		opp.Symbol,
		opp.Type,
		opp.SpreadPercent,
		opp.SpreadAbsolute,
		opp.Exchange1,
		opp.Market1Type,
		opp.Price1,
		opp.Exchange2,
		opp.Market2Type,
		opp.Price2,
		opp.Volume24h,
		opp.ProfitPotential,
		opp.Timestamp.Format("15:04:05"),
	)

	return msg
}

// sendMessage 发送消息到Telegram
func (t *TelegramNotifier) sendMessage(message string) error {
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.BotToken)

	payload := map[string]interface{}{
		"chat_id":    t.ChatID,
		"text":       message,
		"parse_mode": "HTML",
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	resp, err := t.HTTPClient.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("telegram API error: status=%d", resp.StatusCode)
	}

	return nil
}

// IsEnabled 检查是否启用
func (t *TelegramNotifier) IsEnabled() bool {
	return t.enabled
}

// Enable 启用通知
func (t *TelegramNotifier) Enable() {
	t.enabled = true
}

// Disable 禁用通知
func (t *TelegramNotifier) Disable() {
	t.enabled = false
}
