# 快速启动指南

## 1. 配置环境

### 1.1 复制配置文件
```bash
cp .env.example .env
```

### 1.2 编辑 .env 文件

打开 `.env` 文件，填入以下必要配置：

#### Aster 交易所 API 配置
```bash
ASTER_API_KEY=your_api_key_here
ASTER_SECRET_KEY=your_secret_key_here
```

**如何获取 Aster API 密钥：**
1. 访问 [Aster Exchange](https://www.asterdex.com)
2. 登录账户
3. 进入 API 管理页面
4. 创建新的 API 密钥
   - 现货交易：创建普通 API 密钥
   - 合约交易：需要切换到"专业API"并创建 AGENT
5. 复制 API Key 和 Secret Key

#### Telegram 通知配置（可选）
```bash
TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrsTUVwxyz
TELEGRAM_CHAT_ID=123456789
```

**如何配置 Telegram：**

1. 创建 Telegram Bot
   - 在 Telegram 中搜索 `@BotFather`
   - 发送 `/newbot` 命令
   - 按提示设置 Bot 名称
   - 获取 Bot Token（格式：`123456789:ABCdefGHIjklMNOpqrsTUVwxyz`）

2. 获取 Chat ID
   - 在 Telegram 中搜索 `@userinfobot`
   - 发送任意消息
   - Bot 会返回你的 Chat ID

3. 激活 Bot
   - 在 Telegram 中搜索你的 Bot 名称
   - 点击 "Start" 按钮

#### 监控配置
```bash
# 最小价差百分比（只通知大于此值的机会）
MIN_SPREAD_PERCENT=0.5

# 刷新间隔（秒）
UPDATE_INTERVAL=1

# 监控的交易对（逗号分隔）
MONITOR_SYMBOLS=BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,ADAUSDT

# 是否启用 Telegram 通知
ENABLE_NOTIFICATION=true
```

## 2. 安装依赖

```bash
go mod download
```

## 3. 运行程序

### 方式 1: 直接运行（开发模式）
```bash
go run ./cmd/monitor/main.go
```

### 方式 2: 编译后运行
```bash
# Windows
go build -o arbitrage-monitor.exe ./cmd/monitor
./arbitrage-monitor.exe

# Linux/Mac
go build -o arbitrage-monitor ./cmd/monitor
./arbitrage-monitor
```

## 4. 使用界面

程序启动后，你会看到一个交互式终端界面：

```
🚀 Crypto Arbitrage Monitor

Total: 45 | Showing: 15 | Sort: spread ↓ | Filter: all | Last Update: 14:23:45

┌──────────┬───────────────┬────────────────────┬────────────────────┬──────────┬──────────┬──────────┐
│ Symbol   │ Type          │ Buy From           │ Sell To            │ Spread % │ Profit $ │ Volume   │
├──────────┼───────────────┼────────────────────┼────────────────────┼──────────┼──────────┼──────────┤
│ BTCUSDT  │ spot-future   │ ASTER SPOT         │ ASTER FUTURE       │ 1.25%    │ $450.50  │ 1250000  │
└──────────┴───────────────┴────────────────────┴────────────────────┴──────────┴──────────┴──────────┘
```

### 键盘操作

- **`s`** - 切换排序字段
  - spread → profit → volume → time → spread ...

- **`d`** - 切换排序方向
  - ↓ 降序 ↔ ↑ 升序

- **`f`** - 切换过滤器
  - all → spot-spot → spot-future → future-future → all ...

- **`r`** - 手动刷新数据

- **`q`** 或 **`Ctrl+C`** - 退出程序

## 5. 查看日志

程序运行时会生成 `arbitrage.log` 文件，记录详细的运行日志：

```bash
# Windows
type arbitrage.log

# Linux/Mac
tail -f arbitrage.log
```

## 6. 常见问题

### Q1: 无法连接到 Aster API
**解决方案：**
- 检查网络连接
- 确认 API 密钥是否正确
- 检查 API 密钥是否设置了 IP 白名单

### Q2: Telegram 通知不工作
**解决方案：**
- 确认 Bot Token 和 Chat ID 正确
- 确认已在 Telegram 中启动了 Bot（点击 Start）
- 检查 `ENABLE_NOTIFICATION` 是否设置为 `true`

### Q3: 界面显示异常
**解决方案：**
- 确保终端窗口足够大（建议至少 120x30）
- 使用支持 UTF-8 的现代终端（Windows Terminal、iTerm2 等）

### Q4: 价差数据为空
**解决方案：**
- 等待几秒让 WebSocket 连接建立
- 检查监控的交易对在交易所是否存在
- 查看日志文件确认是否有错误

## 7. 性能调优

### 增加监控的交易对
编辑 `.env`：
```bash
MONITOR_SYMBOLS=BTCUSDT,ETHUSDT,SOLUSDT,BNBUSDT,ADAUSDT,DOGEUSDT,XRPUSDT
```

### 调整刷新频率
```bash
# 更快的刷新（1秒）
UPDATE_INTERVAL=1

# 较慢的刷新（5秒，节省资源）
UPDATE_INTERVAL=5
```

### 调整价差阈值
```bash
# 只显示价差 > 1% 的机会
MIN_SPREAD_PERCENT=1.0

# 显示所有价差 > 0.3% 的机会
MIN_SPREAD_PERCENT=0.3
```

## 8. 下一步

- 查看 [README.md](README.md) 了解完整功能
- 添加更多交易所支持（Binance、Bitget 等）
- 配置自动化交易（谨慎使用）

## 9. 安全提醒

⚠️ **重要安全建议：**

1. 不要分享你的 API 密钥和 Secret Key
2. 建议为 API 密钥设置 IP 白名单
3. 使用只读权限的 API 密钥（本程序只需读取行情）
4. 定期更换 API 密钥
5. 不要将 `.env` 文件提交到版本控制系统

## 10. 获取帮助

如有问题，请：
1. 查看 `arbitrage.log` 日志文件
2. 在 GitHub 创建 Issue
3. 查阅 Aster API 文档：`D:\api-docs\`
