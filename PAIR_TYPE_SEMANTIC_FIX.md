# Pair Type 语义修复：类型与买卖方向一致

## 问题描述

**用户反馈**:
```
SPELLUSDT   spot-future   LOW BINANCE FUTURE @0.000248   HIGH BINANCE SPOT @0.0002498   0.73%
```

**问题**:
- Pair Type 显示 "spot-future"（现货-合约）
- 但 Buy From 是 "BINANCE FUTURE"（买合约）
- Sell To 是 "BINANCE SPOT"（卖现货）

**矛盾**: "spot-future" 应该表示买现货卖合约，但实际显示的是买合约卖现货 ❌

## 根本原因

### 之前的错误逻辑

```go
// 固定 marketType（基于数据源顺序）
marketType := getMarketTypeString(src1.marketType, src2.marketType)
// 例如：src1=SPOT, src2=FUTURE → marketType = "spot-future"

// 应用过滤器
if !shouldShowMarketType(marketType) {
    continue
}

// 但是，Buy/Sell 是根据价格动态决定的：
if price1.AskPrice <= price2.BidPrice {
    buy = src1  // SPOT
    sell = src2 // FUTURE
} else {
    buy = src2  // FUTURE ← 实际是买这个
    sell = src1 // SPOT   ← 实际是卖这个
}

// 显示：
// Type: "spot-future"（固定）
// Buy: FUTURE（动态）← 不一致！
// Sell: SPOT（动态）
```

**结果**: Pair Type 和 Buy/Sell 列语义不一致。

## 语义重新定义

### Pair Type 的正确含义

**Pair Type 应该表示实际的买卖方向，而不仅仅是数据源组合。**

| Pair Type | 含义 | Buy From | Sell To |
|-----------|------|----------|---------|
| **spot-spot** | 买入现货，卖出现货 | SPOT 市场 | SPOT 市场 |
| **spot-future** | 买入现货，卖出合约 | SPOT 市场 | FUTURE 市场 |
| **future-spot** | 买入合约，卖出现货 | FUTURE 市场 | SPOT 市场 |
| **future-future** | 买入合约，卖出合约 | FUTURE 市场 | FUTURE 市场 |

**关键**:
- Pair Type 的第一个部分 = 买入的市场类型
- Pair Type 的第二个部分 = 卖出的市场类型

### 过滤器的含义

| 过滤器 | 含义 | 适用场景 |
|--------|------|---------|
| **spot-future** | 只显示买现货卖合约的机会 | 现货溢价时（合约贴水） |
| **future-spot** | 只显示买合约卖现货的机会 | 合约溢价时（合约升水） |
| **spot-spot** | 只显示现货间套利 | 跨交易所现货价差 |
| **future-future** | 只显示合约间套利 | 跨交易所合约价差 |

## 新的实现逻辑

### 步骤 1: 根据价格确定实际买卖方向

```go
// 第 310-318 行
if price1 != nil && price2 != nil {
    // 根据价格确定实际的买卖方向和类型
    var actualType string
    if price1.AskPrice <= price2.BidPrice {
        // 买 price1（src1），卖 price2（src2）
        actualType = getMarketTypeString(src1.marketType, src2.marketType)
        // 例如：src1=SPOT, src2=FUTURE → actualType = "spot-future"
    } else {
        // 买 price2（src2），卖 price1（src1）
        actualType = getMarketTypeString(src2.marketType, src1.marketType)
        // 例如：src2=FUTURE, src1=SPOT → actualType = "future-spot"
    }
    ...
}
```

**关键**: actualType 由实际的买卖方向决定，不是固定的数据源顺序。

### 步骤 2: 应用过滤器

```go
// 第 320-323 行
// 应用过滤器（基于实际的买卖方向）
if !m.shouldShowMarketType(actualType) {
    continue
}
```

**效果**: 过滤器筛选的是实际的买卖方向，不是数据源组合。

### 步骤 3: 显示时使用 actualType

```go
// 第 332, 336 行
row := m.createOpportunityRow(opp, actualType, false)
row := m.createNoPriceSpreadRow(symbol, price1, price2, actualType, false)
```

**效果**: Pair Type 列显示的是实际的买卖方向。

## 修复效果对比

### 修复前（错误）

```
SPELLUSDT   spot-future   LOW BINANCE FUTURE @0.000248   HIGH BINANCE SPOT @0.0002498
            ↑ 类型说 spot-future            ↑ 实际买 FUTURE      ↑ 实际卖 SPOT
                                            ← 矛盾！应该是 future-spot ❌
```

**问题**:
- Pair Type 说的是 "spot-future"（买现货卖合约）
- 但实际操作是买合约卖现货
- 用户困惑 ❌

### 修复后（正确）

```
SPELLUSDT   future-spot   LOW BINANCE FUTURE @0.000248   HIGH BINANCE SPOT @0.0002498
            ↑ 类型说 future-spot            ↑ 买 FUTURE         ↑ 卖 SPOT
                                            ← 完全一致！✅
```

**改进**:
- Pair Type 是 "future-spot"（买合约卖现货）
- 实际操作确实是买 FUTURE 卖 SPOT
- 语义一致 ✅

## 完整示例

### 示例 1: 现货便宜（正常期货升水）

```
价格:
  BINANCE SPOT Ask: $100（便宜）
  BINANCE FUTURE Bid: $102（贵）

显示:
  Pair Type: spot-future
  Buy From: BINANCE SPOT @100（买便宜的现货）
  Sell To: BINANCE FUTURE @102（卖贵的合约）
  Spread: 2.0%

语义: 买入现货，卖出合约 ✅
策略: 现货套保（现货低于合约，做正向套利）
```

### 示例 2: 合约便宜（期货贴水，少见）

```
价格:
  BINANCE FUTURE Ask: $100（便宜）
  BINANCE SPOT Bid: $102（贵）

显示:
  Pair Type: future-spot
  Buy From: BINANCE FUTURE @100（买便宜的合约）
  Sell To: BINANCE SPOT @102（卖贵的现货）
  Spread: 2.0%

语义: 买入合约，卖出现货 ✅
策略: 反向套利（合约低于现货，做反向套利）
```

### 示例 3: 价格波动导致方向变化

```
时刻 T1:
  SPOT Ask: $100, FUTURE Bid: $102
  → Pair Type: spot-future（买 SPOT 卖 FUTURE）

时刻 T2（价格反转）:
  SPOT Ask: $102, FUTURE Bid: $100
  → Pair Type: future-spot（买 FUTURE 卖 SPOT）

结果:
  - 同一个币对组合（SPOT-FUTURE）
  - Pair Type 从 "spot-future" 变成 "future-spot"
  - 这是合理的，因为套利方向确实变了 ✅
```

**重要**: Pair Type 会随价格波动改变，这反映了真实的套利方向变化。

## 过滤器行为变化

### 新的过滤逻辑

**filter = "spot-future"**:
- 只显示当前价格下买现货卖合约的机会
- 不显示买合约卖现货的机会
- 即使是同一个币对组合（如 BTCUSDT SPOT-FUTURE），如果价格反转导致方向变化，会从这个过滤器消失

**filter = "future-spot"**:
- 只显示当前价格下买合约卖现货的机会
- 不显示买现货卖合约的机会

**filter = "all"**:
- 显示所有方向的机会
- 包括 spot-future 和 future-spot

### 过滤器的动态性

**重要特性**: 过滤器现在是动态的，随价格变化而变化。

```
示例: BTCUSDT (BINANCE SPOT vs FUTURE)

时刻 T1（现货便宜）:
  - actualType = "spot-future"
  - filter="spot-future": 显示 ✅
  - filter="future-spot": 不显示

时刻 T2（合约便宜，价格反转）:
  - actualType = "future-spot"
  - filter="spot-future": 不显示
  - filter="future-spot": 显示 ✅

结果:
  - 同一个币对会在不同的过滤器之间"移动"
  - 这反映了真实的市场状态 ✅
```

## 对用户的影响

### 优点 ✅

1. **语义一致**: Pair Type 和 Buy/Sell 列完全对应
2. **直观理解**: "spot-future" 就是买现货卖合约，一目了然
3. **策略清晰**: 用户可以明确知道套利方向
4. **过滤准确**: 过滤器精确匹配实际的交易方向

### 需要注意 ⚠️

1. **Pair Type 会变化**:
   - 同一个币对的 Type 会随价格波动改变
   - 这是正常的，反映市场状态

2. **过滤器是动态的**:
   - 币对可能在不同过滤器之间"移动"
   - 选择 "spot-future" 时，看到的都是当前可以做正向套利的机会

3. **统计数量会波动**:
   - "Showing: spot-future only" 的数量会随市场变化
   - 这是正常的市场行为

## 实际使用场景

### 场景 1: 寻找正向套利（现货套保）

```
操作:
  1. 按 f 键，选择 "spot-future only"
  2. 查看表格

看到的内容:
  - 全是买现货卖合约的机会
  - 适用于现货溢价时（合约贴水）
  - 可以做正向套保策略

示例:
  BTCUSDT   spot-future   BUY BINANCE SPOT @42000   SELL BINANCE FUTURE @42100
  ETHUSDT   spot-future   BUY ASTER SPOT @2200      SELL BINANCE FUTURE @2205
```

### 场景 2: 寻找反向套利（合约套现）

```
操作:
  1. 按 f 键，选择 "future-spot only"
  2. 查看表格

看到的内容:
  - 全是买合约卖现货的机会
  - 适用于合约溢价时（合约升水）
  - 可以做反向套利策略

示例:
  BTCUSDT   future-spot   BUY BINANCE FUTURE @42000   SELL BINANCE SPOT @42100
  ETHUSDT   future-spot   BUY LIGHTER FUTURE @2200    SELL ASTER SPOT @2205
```

### 场景 3: 监控套利方向变化

```
操作:
  1. 选择 "all" 查看所有机会
  2. 观察 BTCUSDT 的 Pair Type 变化

可能的观察:
  T1: BTCUSDT   spot-future   ...  （现货便宜）
  T2: BTCUSDT   future-spot   ...  （合约便宜，方向反转）
  T3: BTCUSDT   spot-future   ...  （又反转回来）

结论:
  - 市场在正常波动
  - 套利方向在动态变化
  - 这是真实的市场行为
```

## 与 Calculator 的关系

### Calculator 的 opp.Type（保持不变）

Calculator 仍然根据买卖方向生成 `opp.Type`：

```go
// internal/arbitrage/calculator.go
func getArbitrageType(market1, market2 common.MarketType) string {
    // market1 = 买入市场，market2 = 卖出市场
    if market1 == SPOT && market2 == FUTURE {
        return "spot-future"
    } else if market1 == FUTURE && market2 == SPOT {
        return "future-spot"
    }
    ...
}
```

### UI 的 actualType（现在一致）

UI 现在也根据买卖方向生成 `actualType`：

```go
// internal/ui/bubbletea.go
if price1.AskPrice <= price2.BidPrice {
    // 买 price1，卖 price2
    actualType = getMarketTypeString(price1.MarketType, price2.MarketType)
} else {
    // 买 price2，卖 price1
    actualType = getMarketTypeString(price2.MarketType, price1.MarketType)
}
```

**结果**:
- UI 的 `actualType` 和 Calculator 的 `opp.Type` 语义一致
- 都表示实际的买卖方向
- 显示时两者应该相同（大部分情况）

## 技术细节

### 为什么使用 AskPrice 和 BidPrice？

```go
if price1.AskPrice <= price2.BidPrice {
    // 买 price1，卖 price2
}
```

**原因**:
- **AskPrice**: 我们要买入时需要支付的价格（卖方报价）
- **BidPrice**: 我们要卖出时能获得的价格（买方报价）
- 如果 `price1.Ask <= price2.Bid`，说明可以低价买入 price1，高价卖出 price2

**示例**:
```
BINANCE SPOT:
  Ask: $100（我们买入需要付 $100）
  Bid: $99.8（我们卖出只能得 $99.8）

BINANCE FUTURE:
  Ask: $102（我们买入需要付 $102）
  Bid: $101.8（我们卖出只能得 $101.8）

判断:
  SPOT.Ask ($100) < FUTURE.Bid ($101.8)
  → 可以买 SPOT @$100，卖 FUTURE @$101.8
  → 方向: spot-future ✅
```

### 缺失数据的处理

```go
// 第 339-350 行
else if m.knownPairs[pairKey] {
    // 之前有过数据，但现在缺失了
    // 对于缺失数据的行，使用固定的 marketType（因为无法确定价格方向）
    marketType := m.getMarketTypeString(src1.marketType, src2.marketType)

    // 应用过滤器
    if !m.shouldShowMarketType(marketType) {
        continue
    }

    row := m.createEmptyRow(symbol, src1, src2, price1, price2, marketType)
    rows = append(rows, row)
}
```

**原因**:
- 当数据缺失时，无法确定价格方向
- 使用固定的 marketType（基于 src1/src2 顺序）
- 这是合理的降级方案

## 验证方法

### 测试 1: 检查一致性

```bash
1. 运行程序: .\monitor.exe

2. 选择 filter = "all"

3. 找一个 Pair Type 为 "spot-future" 的行

4. 检查:
   - Buy From 列应该包含 SPOT
   - Sell To 列应该包含 FUTURE
   - 完全一致 ✅

5. 找一个 Pair Type 为 "future-spot" 的行

6. 检查:
   - Buy From 列应该包含 FUTURE
   - Sell To 列应该包含 SPOT
   - 完全一致 ✅
```

### 测试 2: 过滤器精确性

```bash
1. 按 f 键，选择 "spot-future only"

2. 检查所有行:
   - Pair Type 列全是 "spot-future" ✅
   - Buy From 列全包含 SPOT ✅
   - Sell To 列全包含 FUTURE ✅

3. 按 f 键，选择 "future-spot only"

4. 检查所有行:
   - Pair Type 列全是 "future-spot" ✅
   - Buy From 列全包含 FUTURE ✅
   - Sell To 列全包含 SPOT ✅
```

### 测试 3: 价格波动响应

```bash
1. 选择 filter = "all"

2. 找到一个币对，例如 BTCUSDT (BINANCE SPOT vs FUTURE)

3. 记录当前的 Pair Type（例如 "spot-future"）

4. 等待 1-2 分钟（价格可能波动）

5. 观察:
   - 如果价格方向反转，Pair Type 可能变成 "future-spot"
   - 这是正常的，反映市场变化 ✅
```

## 总结

### ✅ 修复的问题

| 问题 | 修复前 | 修复后 |
|------|--------|--------|
| **语义一致性** | Type 和 Buy/Sell 不一致 ❌ | 完全一致 ✅ |
| **用户理解** | "spot-future" 但买的是 FUTURE ❌ | "future-spot" 买的是 FUTURE ✅ |
| **过滤器准确性** | 基于数据源组合 | 基于实际买卖方向 ✅ |
| **策略清晰度** | 模糊 | 明确套利方向 ✅ |

### 📊 关键变化

| 方面 | 旧设计 | 新设计 |
|------|--------|--------|
| **Pair Type 含义** | 数据源组合顺序 | 实际买卖方向 |
| **Type 稳定性** | 固定不变 | 随价格动态变化 |
| **过滤器依据** | 数据源组合 | 实际套利方向 |
| **语义一致性** | Type 与 Buy/Sell 可能矛盾 | 完全一致 |

### 🎯 用户价值

**修复前**:
```
SPELLUSDT   spot-future   BUY FUTURE   SELL SPOT
            ↑ 说的是 spot-future    ↑ 实际买的是 FUTURE
            ← 矛盾！用户困惑 ❌
```

**修复后**:
```
SPELLUSDT   future-spot   BUY FUTURE   SELL SPOT
            ↑ 说的是 future-spot    ↑ 实际买的是 FUTURE
            ← 一致！清晰明了 ✅
```

---

**实现时间**: 2025-12-21
**版本**: v1.6
**文件**: `internal/ui/bubbletea.go` (第 310-350 行)
**修复级别**: 语义修正（关键）
**状态**: ✅ 已完成并编译
**编译产物**: `monitor.exe`
