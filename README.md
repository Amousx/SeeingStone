# SeeingStone (视石)

> 🤖 **本项目完全由 [Claude Code](https://claude.com/claude-code) 开发**
>
> SeeingStone是一个高性能加密货币套利监控系统，使用Go语言编写，支持实时监控多个交易所的价差机会。项目的每一行代码、架构设计、调试修复都由Claude Code (Sonnet 4.5) 自主完成。

---

## ✨ 功能特性

- ✅ **实时监控**: 显示所有价差机会（包括负价差），按绝对值排序
- ✅ **4种套利类型**: spot-spot、spot-future、future-spot、future-future
- ✅ **智能显示**: 清晰标注买入价（LOW）和卖出价（HIGH），显示具体价格
- ✅ **多数据源**: WebSocket实时流 + REST API备份，双通道保障
- ✅ **精美TUI**: 基于Bubbletea的终端界面，支持实时排序和过滤
- ✅ **即时通知**: Telegram机器人推送高价差机会
- ✅ **高性能**: 并发处理，自动重连，过期数据过滤
- ✅ **多交易所支持**: Aster（现货+合约，196个交易对）、Lighter（永续合约，**117个活跃市场**，自动同步）
- ✅ **跨交易所套利**: 实时监控Aster与Lighter之间的价差机会
- ✅ **自动市场发现**: Lighter市场通过官方API自动获取，无需手动配置

> 📝 **关于Lighter市场**: 通过官方API自动获取所有117个活跃市场，包括加密货币、外汇、美股等。详见 [LIGHTER_MARKETS.md](./LIGHTER_MARKETS.md)

## 🎯 监控的套利类型

SeeingStone支持4种套利类型，**左边为买入市场，右边为卖出市场**：

| 类型 | 描述 | 示例 |
|------|------|------|
| **spot-spot** | 现货买入 → 现货卖出 | 币安现货买入BTC，OKX现货卖出BTC |
| **spot-future** | 现货买入 → 合约卖出 | 现货市场买入，合约市场做空对冲 |
| **future-spot** | 合约买入 → 现货卖出 | 合约市场做多，现货市场卖出对冲 |
| **future-future** | 合约买入 → 合约卖出 | 币安合约买入，Bybit合约卖出 |

## 🚀 快速开始

### 前置要求

- Go 1.21 或更高版本
- Git

### 安装步骤

1. **克隆仓库**
```bash
git clone <repository-url>
cd crypto_arbitrage_golang
```

2. **安装依赖**
```bash
go mod download
```

3. **配置环境变量**
```bash
cp .env.example .env
# 编辑 .env 文件，填入你的 API 密钥
```

4. **编译运行**
```bash
go build -o seeing-stone.exe ./cmd/monitor
./seeing-stone.exe
```

或直接运行：
```bash
go run ./cmd/monitor/main.go
```

## ⚙️ 配置说明

### 环境变量

创建 `.env` 文件并配置以下参数：

```bash
# Aster API配置
ASTER_API_KEY=your_api_key
ASTER_SECRET_KEY=your_secret_key

# Telegram通知（可选）
TELEGRAM_BOT_TOKEN=your_bot_token
TELEGRAM_CHAT_ID=your_chat_id
ENABLE_NOTIFICATION=false

# 监控参数
MIN_SPREAD_PERCENT=0.1        # 最小价差阈值（仅影响Telegram通知）
UPDATE_INTERVAL=1             # UI刷新间隔（秒）

# Lighter配置
LIGHTER_MARKET_REFRESH_INTERVAL=10  # Lighter市场刷新间隔（分钟），0表示禁用自动刷新
```

### Aster交易所配置

在 [Aster Exchange](https://www.asterdex.com) 创建API密钥：
- **现货API**: 普通API密钥即可
- **合约API**: 需要创建专业API (AGENT)

### Telegram通知配置（可选）

1. **创建Telegram Bot**:
   - 在Telegram中搜索 `@BotFather`
   - 发送 `/newbot` 创建新机器人
   - 复制Bot Token

2. **获取Chat ID**:
   - 在Telegram中搜索 `@userinfobot`
   - 发送任意消息获取你的Chat ID

## 🎮 使用方法

### 键盘快捷键

| 按键 | 功能 |
|------|------|
| `s` | 切换排序字段（价差 → 利润 → 交易量 → 时间） |
| `d` | 切换排序方向（升序 ↔ 降序） |
| `f` | 切换过滤器（全部 → spot-spot → spot-future → future-spot → future-future → 全部） |
| `r` | 手动刷新时间戳 |
| `q` / `Ctrl+C` | 退出程序 |

### 界面示例

```
Crypto Arbitrage Monitor

Total: 18 | Showing: 18 | Sort: spread ↓ | Filter: all | Last Update: 17:20:45

┌────────────┬──────────────┬────────────────────────────────┬────────────────────────────────┬──────────┬──────────┬──────────┐
│ Symbol     │ Type         │ Buy From (LOW)                 │ Sell To (HIGH)                 │ Spread % │ Profit $ │ Volume   │
├────────────┼──────────────┼────────────────────────────────┼────────────────────────────────┼──────────┼──────────┼──────────┤
│ GUAUSDT    │ future-spot  │ LOW ASTER FUTURE @0.1128       │ HIGH ASTER SPOT @0.1143        │ -1.28%   │ $-12.50  │ 125000   │
│ GIGGLEUSDT │ spot-future  │ LOW ASTER SPOT @69.0900        │ HIGH ASTER FUTURE @68.3300     │ -1.10%   │ $-45.30  │ 85000    │
│ TAGUSDT    │ future-spot  │ LOW ASTER FUTURE @0.0005       │ HIGH ASTER SPOT @0.0005        │ -0.85%   │ $-3.20   │ 45000    │
└────────────┴──────────────┴────────────────────────────────┴────────────────────────────────┴──────────┴──────────┴──────────┘

Keys: [s] Sort Field | [d] Sort Direction | [f] Filter | [r] Refresh | [q] Quit
```

### 数据说明

- **Spread %**: 价差百分比（正数=套利机会，负数=价格倒挂）
- **按绝对值排序**: 无论正负，都显示最大的价差机会
- **LOW/HIGH标记**:
  - `LOW` = 买入价格（低价端）
  - `HIGH` = 卖出价格（高价端）
- **价格显示**: 显示具体的买入价和卖出价（4位小数）

## 📁 项目结构

```
crypto_arbitrage_golang/
├── cmd/
│   └── monitor/              # 主程序入口
│       └── main.go           # 应用启动、协程管理、数据流编排
├── internal/
│   ├── exchange/
│   │   ├── aster/           # Aster交易所集成
│   │   │   ├── auth.go      # API认证（HMAC SHA256签名）
│   │   │   ├── spot.go      # 现货市场REST API
│   │   │   ├── futures.go   # 合约市场REST API
│   │   │   ├── websocket.go # WebSocket实时数据流
│   │   │   └── utils.go     # 工具函数
│   │   └── lighter/         # Lighter交易所集成
│   │       ├── types.go     # WebSocket消息结构
│   │       ├── websocket.go # WebSocket客户端
│   │       └── markets.go   # 市场配置
│   ├── arbitrage/
│   │   └── calculator.go    # 套利机会计算引擎
│   ├── notification/
│   │   └── telegram.go      # Telegram推送通知
│   └── ui/
│       └── bubbletea.go     # Bubbletea TUI实现
├── pkg/
│   └── common/
│       └── types.go         # 共享数据结构（Price, ArbitrageOpportunity）
└── config/
    └── config.go            # 环境配置管理
```

## 🏗️ 架构设计

### 数据流

```
┌─────────────────────────────────────────────────────────────┐
│                      数据流向                                 │
└─────────────────────────────────────────────────────────────┘

1. 价格采集
   ├─ Aster WebSocket (实时) ─→ Calculator.UpdatePrice()
   ├─ Lighter WebSocket (实时) ─→ Calculator.UpdatePrice()
   └─ Aster REST API (每30s)  ─→ Calculator.UpdatePrice()

2. 套利计算（每1秒）
   └─ Calculator.CalculateArbitrage()
      ├─ 过滤过期数据（>60秒）
      ├─ 按symbol分组
      ├─ 两两比较价格
      ├─ 创建套利机会
      └─ 存储到opportunities数组

3. UI展示（每1秒）
   └─ TickMsg触发
      ├─ 从Calculator获取最新数据
      ├─ 按类型过滤
      ├─ 按字段排序（绝对值）
      └─ 渲染表格

4. Telegram通知
   └─ 价差 >= 阈值时推送
```

### 并发设计

- **协程1**: REST API定时刷新（每30秒）
- **协程2**: 套利机会计算（每1秒）
- **协程3**: Telegram通知监听
- **WebSocket协程**: 实时价格更新（自动重连）
- **UI主循环**: Bubbletea事件循环

## 🔧 技术栈

| 组件 | 技术 | 用途 |
|------|------|------|
| **语言** | Go 1.21+ | 高性能并发处理 |
| **TUI框架** | Bubbletea | 终端用户界面 |
| **UI组件** | Bubbles | 表格、样式组件 |
| **样式** | Lipgloss | 终端样式渲染 |
| **WebSocket** | gorilla/websocket | 实时数据流 |
| **ID生成** | google/uuid | 套利机会唯一标识 |

## 🛡️ 安全建议

- ⚠️ **不要提交API密钥**: `.env` 文件已在 `.gitignore` 中
- ⚠️ **使用只读API**: 本程序仅需读取行情数据权限
- ⚠️ **IP白名单**: 在交易所后台设置API密钥的IP访问限制
- ⚠️ **定期轮换密钥**: 建议每月更换API密钥
- ⚠️ **Telegram Token保护**: 避免Bot Token泄露

## 🚧 开发路线图

- [x] Aster交易所集成（现货+合约）
- [x] WebSocket实时数据流
- [x] 4种套利类型识别
- [x] Bubbletea TUI界面
- [x] Telegram即时通知
- [x] 绝对价差排序
- [x] 价格高低可视化
- [ ] 多交易所支持（Binance, Bybit, OKX等）
- [ ] 历史数据记录与分析
- [ ] 价差趋势图表
- [ ] 套利机会回测
- [ ] Web Dashboard
- [ ] RESTful API服务
- [ ] Docker容器化部署
- [ ] 自动交易执行（谨慎使用）

## 🤖 关于Claude Code

本项目是展示AI辅助编程能力的典范案例。从架构设计、代码实现、问题调试到文档编写，**100%由Claude Code (Sonnet 4.5)** 完成。

### 开发过程亮点

- ✅ **完整架构设计**: 从零开始设计项目结构和数据流
- ✅ **多轮问题修复**: 诊断并修复UI数据显示、时序、过滤等复杂问题
- ✅ **性能优化**: WebSocket自动重连、过期数据过滤、并发安全
- ✅ **用户体验**: 直观的价格显示、灵活的过滤排序、实时数据刷新
- ✅ **代码质量**: 模块化设计、接口抽象、错误处理完善

### 技术挑战解决

1. **UI数据不显示**: 发现并修复Update方法缺少return、启动时序问题、价格过期时间过短
2. **绝对价差排序**: 实现math.Abs()排序，同时显示正负价差
3. **4种套利类型**: 扩展类型系统，区分spot-future和future-spot
4. **数据流架构**: 从消息传递改为主动获取，彻底解决时序问题

## 📜 许可证

MIT License

## ⚠️ 免责声明

本软件仅用于**教育和研究目的**。加密货币交易具有高风险，使用本软件进行实际交易需自行承担全部风险。

**作者不对以下情况负责**：
- 交易损失
- API限制或封禁
- 数据延迟或错误
- 第三方服务中断

请在充分了解风险后谨慎使用。

## 🤝 贡献

欢迎提交Issue和Pull Request！

如果你对AI辅助编程感兴趣，也欢迎分享你使用Claude Code的体验。

## 📧 联系方式

如有问题或建议，请创建Issue。

---

**Built with ❤️ by Claude Code**
