# 混合架构优化方案（Aster & Lighter）

> **更新：** Lighter 也已升级为混合架构！详见 [LIGHTER_WEBSOCKET_UPGRADE.md](LIGHTER_WEBSOCKET_UPGRADE.md)

# Aster 混合架构优化方案

## 问题背景

Aster 的 `!miniTicker@arr` WebSocket 订阅是**增量推送模型**，不是全量推送：
- 只推送有交易活动的交易对
- 可能遗漏低流动性或暂时无交易的币种
- 需要 REST API 补充以保证数据完整性

## 架构设计

### 核心原则

1. **WebSocket 负责实时性** - 提供活跃交易对的低延迟更新
2. **REST API 负责完整性** - 保证所有交易对都有价格数据
3. **智能补充策略** - 根据数据新鲜度动态调整刷新频率

### 实现方案

#### Step 1: 冷启动快照
```
启动时拉取 REST API 全市场快照
├─ Aster Spot: GetAllBookTickers() + GetAll24hrTickers()
└─ Aster Futures: GetAllBookTickers() + GetAll24hrTickers()
```

#### Step 2: 时间戳追踪
每个 `common.Price` 自动维护：
- `Timestamp`: 价格数据的时间戳
- `LastUpdated`: 最后更新时间

Calculator 提供方法：
- `GetStaleSymbols(threshold)` - 获取超过阈值未更新的币种
- `GetCoverageStats()` - 统计覆盖率

#### Step 3: WebSocket 实时刷新
```
连接 wss://fstream.asterdex.com/ws
订阅 !miniTicker@arr
└─ 收到数据时更新 price.LastUpdated
```

## 刷新策略

### 冷启动阶段（前 60 秒）
```
协程1: Aster 智能 REST 补充刷新
├─ 频率: 每 2 秒
├─ 操作: 全量拉取所有 Aster 价格
└─ 目的: 快速建立完整的价格数据集
```

### 正常运行阶段（60 秒后）
```
协程1: Aster 智能 REST 补充刷新
├─ 频率: 每 10 秒检查一次
├─ 检查: GetStaleSymbols(10秒)
├─ 操作: 仅当发现过期 symbol 时，才拉取全量
└─ 优化: 活跃币种由 WS 更新，REST 只补充不活跃币种
```

### Lighter（保持原策略）
```
协程2: Lighter 价格刷新
├─ 频率: 每 5 秒
└─ 操作: 全量拉取（Lighter 没有 WebSocket）
```

## 监控机制

### 覆盖率监控
```
协程5: Symbol 覆盖率监控
├─ 频率: 每 30 秒
├─ 统计:
│   ├─ TotalSymbols: 总交易对数量
│   ├─ ActiveSymbols: 60秒内有更新的数量
│   ├─ AsterSymbols: Aster 活跃交易对
│   └─ LighterSymbols: Lighter 活跃交易对
└─ 告警: 如果 ActiveSymbols < TotalSymbols/2，发出警告
```

日志输出示例：
```
[Coverage] Total: 150, Active: 145, Aster: 120, Lighter: 25
[Warning] Low coverage: only 72/150 symbols are active
```

## 协程架构

```
main()
├─ 协程1: Aster 智能 REST 补充刷新
│   ├─ 冷启动模式（0-60s）: 每 2 秒全量刷新
│   └─ 正常模式（60s+）: 每 10 秒检查，按需刷新
│
├─ 协程2: Lighter REST 刷新（每 5 秒）
│
├─ 协程3: 价差计算（按配置间隔）
│
├─ 协程4: Telegram 通知
│
└─ 协程5: 覆盖率监控（每 30 秒）
```

## 优势分析

### 1. 实时性
- WebSocket 提供毫秒级延迟
- 活跃交易对实时更新

### 2. 完整性
- REST API 保证所有币种都有数据
- 智能检测并补充过期数据

### 3. 性能优化
- 冷启动阶段快速建立数据集（前 60 秒高频）
- 正常阶段按需刷新，降低 API 调用频率
- WebSocket 接管活跃币种，REST 只补充不活跃币种

### 4. 可观测性
- 覆盖率统计实时监控数据质量
- 日志清晰标识 [Cold Start] / [Normal] / [Coverage]
- 告警机制及时发现数据问题

## 时间窗口统一

所有交易所的价格数据使用统一的时间窗口判断：
- 60 秒内有更新算"活跃"（用于套利计算）
- 10 秒内无更新触发 REST 补充（Aster）
- `price.LastUpdated` 记录最后更新时间

## 测试建议

1. **冷启动测试**
   ```bash
   # 查看日志，确认前 60 秒每 2 秒刷新一次
   [Cold Start] Aster REST refresh (2s elapsed)
   [Cold Start] Aster REST refresh (4s elapsed)
   ...
   ```

2. **覆盖率测试**
   ```bash
   # 查看日志，确认覆盖率统计正常
   [Coverage] Total: 150, Active: 145, Aster: 120, Lighter: 25
   ```

3. **WebSocket 测试**
   ```bash
   # 监控 WebSocket 是否正常接收 MiniTicker
   # 查看 price 更新频率
   ```

4. **智能刷新测试**
   ```bash
   # 60 秒后查看日志，确认只在发现 stale symbols 时刷新
   [Normal] Found 5 stale Aster symbols, refreshing via REST...
   ```

## 配置建议

- `UpdateInterval`: 建议 1-3 秒（套利计算频率）
- `MinSpreadPercent`: 根据实际需求调整
- WebSocket 自动处理 ping/pong 和 24 小时重连

## 注意事项

1. Aster WebSocket 连接最长 24 小时，会自动重连
2. 服务端 5 分钟 ping，客户端自动 pong
3. Combined Stream 格式已正确处理
4. 价格数据带有时间戳，用于覆盖率统计
