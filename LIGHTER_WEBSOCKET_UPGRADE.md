# Lighter WebSocket 升级方案

## 概述

Lighter 现在也采用了与 Aster 相同的**混合架构**，使用 WebSocket 实时推送 + REST API 智能补充，确保数据的实时性和完整性。

## 核心改进

### 1. WebSocket 订阅方式

#### 新增 `order_book/all` 订阅

添加了 `SubscribeAll()` 方法，支持一次性订阅所有市场：

```go
// 订阅所有市场
func (c *WSClient) SubscribeAll() error {
    orderBookSub := SubscribeMessage{
        Type:    "subscribe",
        Channel: "order_book/all",
    }

    marketStatsSub := SubscribeMessage{
        Type:    "subscribe",
        Channel: "market_stats/all",
    }
    // ...
}
```

**优势：**
- 一次订阅获取所有市场数据
- 减少订阅消息数量
- 简化代码逻辑

#### 订阅策略

```
启动时尝试:
├─ 1. SubscribeAll() - 订阅 order_book/all
│   └─ 如果成功：使用全市场订阅
│   └─ 如果失败：回退到逐个订阅
└─ 2. Subscribe(marketIDs) - 逐个订阅每个市场
```

### 2. 数据解析增强

#### 支持 market_id 字段

```go
type OrderBookData struct {
    Code      int           `json:"code"`
    MarketID  int           `json:"market_id,omitempty"` // 新增：用于 order_book/all
    Asks      []PriceLevel  `json:"asks"`
    Bids      []PriceLevel  `json:"bids"`
    Nonce     int64         `json:"nonce"`
    Timestamp int64         `json:"timestamp"`
}
```

#### 智能 Market ID 解析

```go
func (c *WSClient) handleOrderBookUpdate(update *OrderBookUpdate) {
    var marketID int

    // 1. 优先从 OrderBook.MarketID 字段获取（order_book/all）
    if update.OrderBook.MarketID > 0 {
        marketID = update.OrderBook.MarketID
    } else {
        // 2. 从 channel 解析（order_book:123 或 order_book/123）
        // ...
    }
}
```

**兼容性：**
- 支持 `order_book/all` 返回的数据（包含 market_id）
- 支持逐个订阅返回的数据（从 channel 解析）
- 向后兼容旧版本

### 3. 混合架构实现

#### 启动流程

```
1. 连接 Lighter WebSocket (wss://api.lighter.xyz/v1/ws)
2. 尝试订阅 order_book/all
3. 如果失败，回退到逐个订阅
4. 拉取 REST API 快照（保证完整性）
5. 启动智能 REST 补充协程
```

#### 智能刷新策略

**冷启动阶段（前 60 秒）：**
```
每 2 秒全量刷新
└─ 快速建立完整数据集
```

**正常运行阶段（60 秒后）：**
```
每 10 秒检查一次
└─ 如果发现过期 symbol（>10秒未更新）
    └─ 触发 REST API 全量刷新
    └─ 否则跳过（节省 API 调用）
```

## 架构对比

| 维度 | 旧架构 | 新架构 |
|------|--------|--------|
| **数据源** | REST API 每 5 秒轮询 | WebSocket 实时 + REST 智能补充 |
| **订阅方式** | 逐个订阅每个市场 | `order_book/all` 一次性订阅 |
| **延迟** | 5 秒 | 毫秒级（WebSocket） |
| **完整性** | 依赖轮询频率 | REST 快照 + WS 实时 + 智能补充 |
| **API 调用** | 固定高频 | 冷启动高频，正常阶段按需 |
| **降级策略** | 无 | WebSocket 失败自动回退 |

## 技术细节

### WebSocket URL
```
wss://api.lighter.xyz/v1/ws
```

### 订阅消息格式
```json
{
  "type": "subscribe",
  "channel": "order_book/all"
}

{
  "type": "subscribe",
  "channel": "market_stats/all"
}
```

### 数据格式（order_book/all）
```json
{
  "type": "update/order_book",
  "channel": "order_book/all",
  "order_book": {
    "code": 0,
    "market_id": 123,  // ← 关键字段
    "asks": [...],
    "bids": [...],
    "nonce": 12345,
    "timestamp": 1234567890
  }
}
```

## 降级与容错

### WebSocket 连接失败
```
如果 WebSocket 连接失败：
├─ 记录警告日志
├─ 设置 lighterWS = nil
└─ 继续使用 REST API（每 2 秒刷新）
```

### order_book/all 订阅失败
```
如果 order_book/all 订阅失败：
├─ 记录警告日志
├─ 自动回退到逐个订阅
└─ 订阅 order_book/1, order_book/2, ...
```

### WebSocket 断线重连
```
WebSocket 客户端自带重连机制：
├─ 检测到断线后等待 5 秒
├─ 自动重新连接
└─ 重新订阅所有市场
```

## 日志输出示例

### 启动日志
```
Connecting to Lighter WebSocket...
Subscribing to Lighter order_book/all...
Lighter WebSocket subscribed to order_book/all successfully
Fetching initial Lighter prices via REST API...
Loaded 25 Lighter prices from REST API
```

### 冷启动阶段
```
[Cold Start] Lighter REST refresh (2s elapsed)
[Cold Start] Lighter REST refresh (4s elapsed)
...
[Cold Start] Lighter REST refresh (58s elapsed)
```

### 正常运行阶段
```
[Normal] Found 3 stale Lighter symbols, refreshing via REST...
[Coverage] Total: 150, Active: 145, Aster: 120, Lighter: 25
```

### 降级场景
```
Warning: Failed to connect to Lighter WebSocket: connection refused
Will continue using REST API only for Lighter
[Cold Start] Lighter REST refresh (2s elapsed)
...
```

## 测试建议

### 1. 测试 order_book/all 订阅
```bash
# 启动程序，查看日志
./monitor.exe

# 预期输出：
# Lighter WebSocket subscribed to order_book/all successfully

# 观察是否收到所有市场的数据
```

### 2. 测试降级机制
```bash
# 断开网络后启动程序
# 预期：自动使用 REST API

# 启动后断开 WebSocket
# 预期：REST API 自动补充数据
```

### 3. 测试覆盖率
```bash
# 运行 30 秒后查看覆盖率日志
# 预期：ActiveSymbols 接近 TotalSymbols
[Coverage] Total: 150, Active: 145, Aster: 120, Lighter: 25
```

### 4. 测试智能刷新
```bash
# 60 秒后观察日志
# 如果所有 symbol 都活跃，应该不触发 REST 刷新
# 如果有 stale symbols，应该触发刷新
[Normal] Found 0 stale Lighter symbols, no refresh needed
```

## 性能指标

### 冷启动阶段（前 60 秒）
- REST API 调用：30 次（每 2 秒）
- WebSocket 消息：实时推送
- 数据覆盖率：100%

### 正常运行阶段（60 秒后）
- REST API 调用：按需触发（理想情况 0 次/分钟）
- WebSocket 消息：实时推送
- 数据新鲜度：<10 秒

## 配置参数

```go
// WebSocket URL
wsURL := "wss://api.lighter.xyz/v1/ws"

// 市场刷新间隔（分钟）
refreshInterval := 60

// 冷启动持续时间
coldStartDuration := 60 * time.Second

// REST 刷新检查间隔
checkInterval := 10 * time.Second

// 数据过期阈值
staleThreshold := 10 * time.Second
```

## 注意事项

1. **order_book/all 兼容性**
   - 如果 Lighter API 不支持 `order_book/all`，会自动回退
   - 回退后的行为与旧版本一致

2. **Market ID 来源**
   - 优先使用 `order_book.market_id` 字段
   - 如果没有，从 `channel` 解析
   - 确保向后兼容

3. **WebSocket 连接管理**
   - 使用 `defer lighterWS.Close()` 确保连接正确关闭
   - 内置重连机制处理断线
   - 支持优雅关闭

4. **REST API 作为兜底**
   - WebSocket 失败不影响程序运行
   - REST API 始终作为数据完整性保障
   - 智能刷新减少不必要的 API 调用

## 未来优化方向

1. **精细化 REST 刷新**
   - 只刷新 stale 的特定 symbol（而不是全量）
   - 需要 Lighter API 支持单个 symbol 查询

2. **WebSocket 健康检查**
   - 定期检查 WebSocket 连接状态
   - 主动检测 "僵尸连接"

3. **动态订阅管理**
   - 检测新上市的交易对
   - 动态添加订阅

4. **性能监控**
   - 统计 WebSocket 消息频率
   - 监控 REST API 调用次数
   - 数据延迟分析

## 总结

Lighter 现在拥有与 Aster 相同的混合架构：

✅ **实时性** - WebSocket 毫秒级推送
✅ **完整性** - REST API 快照 + 智能补充
✅ **可靠性** - 自动降级 + 重连机制
✅ **效率** - 智能刷新减少 API 调用
✅ **可观测性** - 覆盖率监控 + 详细日志

这套架构确保了套利监控系统的**高性能**、**高可靠性**和**低延迟**。
