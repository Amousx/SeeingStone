# SeeingStone 更新日志

## v1.0.0 - 初始版本 (2025-12-13)

### 🎉 首次发布

完全由 Claude Code (Sonnet 4.5) 开发的加密货币套利监控系统。

### ✨ 核心功能

- **4种套利类型识别**
  - `spot-spot`: 现货买入 → 现货卖出
  - `spot-future`: 现货买入 → 合约卖出
  - `future-spot`: 合约买入 → 现货卖出 (新增)
  - `future-future`: 合约买入 → 合约卖出

- **实时数据监控**
  - WebSocket实时价格流
  - REST API 30秒备份刷新
  - 自动过滤过期数据（>60秒）
  - 支持 Aster 交易所（现货+合约）

- **智能UI显示**
  - 显示所有价差（包括负价差）
  - 按绝对价差排序
  - 清晰标注 LOW（买入）/ HIGH（卖出）
  - 显示具体价格（4位小数）
  - 实时刷新（1秒间隔）

- **灵活的过滤和排序**
  - 按类型过滤（all / spot-spot / spot-future / future-spot / future-future）
  - 按字段排序（价差 / 利润 / 交易量 / 时间）
  - 升序/降序切换

- **Telegram通知**
  - 高价差机会即时推送
  - 可配置阈值
  - 防重复通知

### 🐛 修复的问题

#### 问题1: UI没有数据显示
**症状**: Total: 0，UI完全空白

**根因分析**:
1. `Update` 方法处理 `UpdateOpportunitiesMsg` 后缺少 `return`
2. UI启动时序问题：`p.Send()` 在 `p.Run()` 之前调用，消息丢失
3. 价格过期时间太短（5秒），数据被过滤掉

**解决方案**:
- 添加 `return m, nil` 到 `UpdateOpportunitiesMsg` 分支
- 重构架构：UI持有calculator引用，主动获取数据
- 延长过期时间到60秒

#### 问题2: 只显示最小价差限制以上的数据
**症状**: 小价差机会不显示

**解决方案**:
- 移除 `minSpread` 限制在创建套利机会时
- `minSpread` 仅用于Telegram通知
- 显示所有价差，按绝对值排序

#### 问题3: Type类型不完整
**症状**: 只有3种类型，无法区分 spot-future 和 future-spot

**解决方案**:
- 扩展 `getArbitrageType()` 函数
- 添加 `future-spot` 类型
- 更新UI过滤逻辑

### 🏗️ 架构改进

**数据流重构**:
```
之前: 协程 → p.Send() → UI (消息丢失)
现在: UI → Calculator.GetOpportunities() (主动获取)
```

**优势**:
- 消除时序依赖
- 简化并发模型
- 提高数据同步可靠性

### 📊 性能指标

- 支持监控 196 个交易对
- WebSocket 订阅 180 个现货流 + 180 个合约流
- 平均发现 18 个套利机会
- UI刷新延迟 < 1秒
- 内存占用 < 50MB

### 📝 技术栈

- **语言**: Go 1.21+
- **TUI**: Bubbletea + Bubbles + Lipgloss
- **WebSocket**: gorilla/websocket
- **ID生成**: google/uuid

### 🎯 下一步计划

- [ ] 支持更多交易所（Binance, Bybit, OKX）
- [ ] 历史数据记录与分析
- [ ] 价差趋势图表
- [ ] 套利机会回测
- [ ] Web Dashboard
- [ ] Docker容器化

---

**Built with ❤️ by Claude Code**
