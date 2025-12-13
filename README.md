# Crypto Arbitrage Monitor

高性能加密货币套利监控系统，支持实时监控多个交易所的价差机会。

## 功能特性

- ✅ 实时监控现货-现货、现货-合约、合约-合约价差
- ✅ 支持多个交易所（当前实现：Aster）
- ✅ WebSocket 实时价格更新
- ✅ Bubbletea 精美 TUI 界面
- ✅ 实时排序和过滤
- ✅ Telegram 即时通知
- ✅ 高性能并发处理

## 支持的交易所

- [x] Aster (现货 + 合约)
- [ ] Binance (计划中)
- [ ] Bitget (计划中)
- [ ] Bybit (计划中)
- [ ] Gate (计划中)
- [ ] Hyperliquid (计划中)
- [ ] Lighter (计划中)

## 监控的价差类型

1. **Spot-Spot**: 现货与现货之间的价差（跨交易所）
2. **Spot-Future**: 现货与合约之间的价差
3. **Future-Future**: 合约与合约之间的价差（跨交易所）

## 安装

### 前置要求

- Go 1.21 或更高版本
- Git

### 步骤

1. 克隆仓库
```bash
git clone <repository-url>
cd crypto_arbitrage_golang
```

2. 安装依赖
```bash
go mod download
```

3. 配置环境变量
```bash
cp .env.example .env
# 编辑 .env 文件，填入你的 API 密钥和配置
```

4. 编译并运行
```bash
go build -o arbitrage-monitor ./cmd/monitor
./arbitrage-monitor
```

或直接运行：
```bash
go run ./cmd/monitor/main.go
```

## 配置说明

### Aster 交易所配置

在 [Aster Exchange](https://www.asterdex.com) 创建 API 密钥：
- 现货 API：普通 API 密钥
- 合约 API：需要创建专业 API (AGENT)

### Telegram 通知配置

1. 创建 Telegram Bot：
   - 在 Telegram 中搜索 `@BotFather`
   - 发送 `/newbot` 创建新机器人
   - 获取 Bot Token

2. 获取 Chat ID：
   - 在 Telegram 中搜索 `@userinfobot`
   - 发送任意消息获取你的 Chat ID

### 监控配置

- `MIN_SPREAD_PERCENT`: 最小价差百分比阈值（默认 0.5%）
- `UPDATE_INTERVAL`: 刷新间隔（秒，默认 1）
- `MONITOR_SYMBOLS`: 监控的交易对，逗号分隔
- `ENABLE_NOTIFICATION`: 是否启用 Telegram 通知

## 使用方法

### 键盘快捷键

- `s` - 切换排序字段（价差 → 利润 → 交易量 → 时间）
- `d` - 切换排序方向（升序/降序）
- `f` - 切换过滤器（全部 → 现货-现货 → 现货-合约 → 合约-合约）
- `r` - 手动刷新
- `q` / `Ctrl+C` - 退出

### 界面说明

```
🚀 Crypto Arbitrage Monitor

Total: 45 | Showing: 15 | Sort: spread ↓ | Filter: all | Last Update: 14:23:45

┌──────────┬───────────────┬────────────────────┬────────────────────┬──────────┬──────────┬──────────┐
│ Symbol   │ Type          │ Buy From           │ Sell To            │ Spread % │ Profit $ │ Volume   │
├──────────┼───────────────┼────────────────────┼────────────────────┼──────────┼──────────┼──────────┤
│ BTCUSDT  │ spot-future   │ ASTER SPOT         │ ASTER FUTURE       │ 1.25%    │ $450.50  │ 1250000  │
│ ETHUSDT  │ future-future │ ASTER FUTURE       │ BINANCE FUTURE     │ 0.85%    │ $125.30  │ 850000   │
└──────────┴───────────────┴────────────────────┴────────────────────┴──────────┴──────────┴──────────┘

Keys: [s] Sort Field | [d] Sort Direction | [f] Filter | [r] Refresh | [q] Quit
```

## 项目结构

```
crypto_arbitrage_golang/
├── cmd/
│   └── monitor/           # 主程序入口
├── internal/
│   ├── exchange/
│   │   └── aster/        # Aster 交易所实现
│   ├── arbitrage/        # 价差计算引擎
│   ├── notification/     # Telegram 通知
│   └── ui/               # Bubbletea TUI
├── pkg/
│   └── common/           # 通用类型定义
└── config/               # 配置管理
```

## 性能优化

- 使用 WebSocket 实时接收价格更新，减少 API 调用
- 并发处理多个交易所数据
- 高效的价差计算算法
- 合理的内存管理和缓存策略

## 安全建议

- ⚠️ 不要将 API 密钥提交到版本控制系统
- ⚠️ 建议设置 API 密钥的 IP 白名单
- ⚠️ 使用只读权限的 API 密钥（仅需行情数据）
- ⚠️ 定期轮换 API 密钥

## 开发路线图

- [x] Aster 交易所集成
- [x] 基础价差监控
- [x] Bubbletea TUI
- [x] Telegram 通知
- [ ] 多交易所支持（Binance, Bitget, Bybit 等）
- [ ] 历史数据分析
- [ ] 回测功能
- [ ] Web Dashboard
- [ ] 自动交易（谨慎使用）

## 许可证

MIT

## 免责声明

本软件仅用于教育和研究目的。使用本软件进行实际交易需自行承担风险。作者不对任何交易损失负责。

## 贡献

欢迎提交 Issue 和 Pull Request！

## 联系方式

如有问题或建议，请创建 Issue。
