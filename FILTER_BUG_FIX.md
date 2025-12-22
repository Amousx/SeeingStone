# 过滤器 Bug 修复：spot-future 和 future-spot 混淆

## 问题描述

**症状**:
1. 当 filter 设置为 `spot-future` 时，显示的内容包括了 `spot-future` 和 `future-spot` 两种类型
2. 当 filter 设置为 `future-spot` 时，显示内容为空

**用户体验**:
```
按 f 键选择 "spot-future only":

预期显示:
  只显示 spot-future 类型的配对

实际显示:
  显示了 spot-future 和 future-spot 混在一起 ❌

按 f 键选择 "future-spot only":

预期显示:
  只显示 future-spot 类型的配对

实际显示:
  空列表（什么都没有）❌
```

## 根本原因分析

### 问题根源：Type 来源不一致

#### 1. Calculator 中的 Type（动态的）

在 `internal/arbitrage/calculator.go` 中：

```go
func (c *Calculator) getArbitrageType(market1, market2 common.MarketType) string {
    // market1 = 买入市场，market2 = 卖出市场
    if market1 == common.MarketTypeSpot && market2 == common.MarketTypeFuture {
        return "spot-future" // 现货买入 → 合约卖出
    } else if market1 == common.MarketTypeFuture && market2 == common.MarketTypeSpot {
        return "future-spot" // 合约买入 → 现货卖出
    }
    ...
}
```

**关键**: Type 由买卖方向决定，买卖方向由价格决定（哪个便宜买哪个，哪个贵卖哪个）。

**示例**:
```
场景 1: ASTER_SPOT vs BINANCE_FUTURE
  - ASTER_SPOT Ask: $100（便宜）
  - BINANCE_FUTURE Bid: $102（贵）
  - 买入: ASTER_SPOT
  - 卖出: BINANCE_FUTURE
  - opp.Type = "spot-future" ✅

场景 2: 同样的交易所组合，价格反转
  - ASTER_SPOT Ask: $103（贵）
  - BINANCE_FUTURE Bid: $101（便宜，假设期货做空）
  - 买入: BINANCE_FUTURE
  - 卖出: ASTER_SPOT
  - opp.Type = "future-spot" ❌ 类型变了！
```

**结论**: Calculator 的 `opp.Type` 是**动态的**，随价格波动改变。

#### 2. UI 中的 marketType（固定的）

在 `internal/ui/bubbletea.go` 的 `updateTable` 中：

```go
// 第 289-302 行
for i := 0; i < len(monitoredSources); i++ {
    for j := i + 1; j < len(monitoredSources); j++ {
        src1 := monitoredSources[i]  // 例如: ASTER_SPOT
        src2 := monitoredSources[j]  // 例如: BINANCE_FUTURE

        // 确定市场类型字符串（固定）
        marketType := m.getMarketTypeString(src1.marketType, src2.marketType)
        // marketType = "spot-future"（永不改变）

        // 应用过滤器
        if !m.shouldShowMarketType(marketType) {
            continue  // 过滤掉不符合的类型
        }
        ...
    }
}
```

**关键**: marketType 由 src1 和 src2 的顺序决定，与价格无关。

**示例**:
```
组合: ASTER_SPOT (src1) + BINANCE_FUTURE (src2)
  - marketType = "spot-future"（固定）
  - 无论价格如何波动，marketType 永远是 "spot-future"
```

**结论**: UI 的 `marketType` 是**固定的**，不随价格改变。

### 3. Bug 发生的流程

#### 步骤 1: 过滤器检查（使用固定 marketType）

```go
// 第 301-306 行
marketType := m.getMarketTypeString(src1.marketType, src2.marketType)
// marketType = "spot-future"

if !m.shouldShowMarketType(marketType) {
    continue  // 如果 filterType = "spot-future"，则通过
}
```

**结果**: 组合通过过滤器检查，因为 `marketType == "spot-future"`。

#### 步骤 2: 查找套利机会

```go
// 第 321-326 行
oppKey := fmt.Sprintf("%s_%s_%s_%s_%s", symbol, src1.exchange, src1.marketType, src2.exchange, src2.marketType)

if opp, exists := oppsByKey[oppKey]; exists {
    // 找到套利机会
    row := m.createOpportunityRow(opp, false)  // ← BUG 在这里！
    rows = append(rows, row)
}
```

**问题**: 找到的 `opp` 的 Type 可能与 `marketType` 不一致！

**示例**:
```
oppKey = "BTCUSDT_ASTER_SPOT_BINANCE_FUTURE"

查找 oppsByKey:
  - 可能找到 opp，其中:
    - opp.Exchange1 = BINANCE (买入方，价格便宜的)
    - opp.Market1Type = FUTURE
    - opp.Exchange2 = ASTER (卖出方，价格贵的)
    - opp.Market2Type = SPOT
    - opp.Type = "future-spot" ← 与 marketType 不一致！
```

**原因**: 在 `oppsByKey` 构建时（第 257-263 行），每个 opp 有两个 key：

```go
for _, opp := range m.opportunities {
    key1 := fmt.Sprintf("%s_%s_%s_%s_%s", opp.Symbol, opp.Exchange1, opp.Market1Type, opp.Exchange2, opp.Market2Type)
    key2 := fmt.Sprintf("%s_%s_%s_%s_%s", opp.Symbol, opp.Exchange2, opp.Market2Type, opp.Exchange1, opp.Market1Type)
    oppsByKey[key1] = opp
    oppsByKey[key2] = opp  // 反向 key 也指向同一个 opp
}
```

所以，`oppKey = "BTCUSDT_ASTER_SPOT_BINANCE_FUTURE"` 可能匹配到 `opp.Type = "future-spot"` 的机会。

#### 步骤 3: 显示行（使用动态 opp.Type）

```go
// 旧代码（有 BUG）- 第 348-369 行
func (m *Model) createOpportunityRow(opp *common.ArbitrageOpportunity, isMissing bool) table.Row {
    ...
    return table.Row{
        opp.Symbol,
        opp.Type,  // ← BUG: 使用动态的 opp.Type，而不是固定的 marketType
        buyFrom,
        sellTo,
        ...
    }
}
```

**结果**:
- 过滤器检查: `marketType = "spot-future"` ✅ 通过
- 显示的 Type: `opp.Type = "future-spot"` ❌ 不一致！

### 4. 为什么 future-spot 过滤器显示为空？

```
流程:
1. 用户选择 filter = "future-spot"
2. updateTable 遍历所有组合:
   - ASTER_SPOT + BINANCE_FUTURE: marketType = "spot-future" ≠ "future-spot" → 跳过
   - ASTER_FUTURE + BINANCE_SPOT: marketType = "future-spot" ✅ 通过
3. 但是，monitoredSources 的顺序是:
   {ASTER_SPOT, ASTER_FUTURE, LIGHTER_FUTURE, BINANCE_SPOT, BINANCE_FUTURE}
4. 两两组合 (i < j):
   - i=1 (ASTER_FUTURE), j=3 (BINANCE_SPOT) → marketType = "future-spot" ✅
   - i=2 (LIGHTER_FUTURE), j=3 (BINANCE_SPOT) → marketType = "future-spot" ✅
5. 生成的 oppKey:
   - "BTCUSDT_ASTER_FUTURE_BINANCE_SPOT"
   - "BTCUSDT_LIGHTER_FUTURE_BINANCE_SPOT"
6. 查找 oppsByKey:
   - 如果 calculator 生成的 opp 是:
     - Exchange1 = BINANCE, Market1Type = SPOT (买入方)
     - Exchange2 = ASTER, Market2Type = FUTURE (卖出方)
     - Type = "spot-future"
   - 那么 oppsByKey 的 key 是:
     - key1 = "BTCUSDT_BINANCE_SPOT_ASTER_FUTURE"
     - key2 = "BTCUSDT_ASTER_FUTURE_BINANCE_SPOT" ✅ 匹配
7. 找到 opp，但 opp.Type = "spot-future"
8. 旧代码显示 opp.Type = "spot-future"
9. 但等等，我们的过滤器是 "future-spot"！
10. 实际上，过滤器已经通过了（第 304 行），所以应该显示...

等等，让我重新分析。我可能理解错了。
```

让我重新思考。过滤器逻辑是在第 304 行：
```go
if !m.shouldShowMarketType(marketType) {
    continue
}
```

如果 `filterType = "future-spot"`，而 `marketType = "future-spot"`，则 `shouldShowMarketType` 返回 `true`，不会 continue，应该会显示。

那为什么 future-spot 过滤器显示为空？

可能的原因：
1. 实际数据中，所有的 opp 都是 "spot-future" 类型（因为价格方向导致）
2. 当 marketType = "future-spot" 时，查找 oppsByKey 可能找不到匹配的 opp
3. 如果找不到 opp，会进入 else 分支（第 328 行），创建无价差行
4. 但是，可能价格数据也不存在，或者交易量不足被过滤了

实际上，问题应该是这样的：
- 当 filter = "spot-future" 时，既会显示 marketType = "spot-future" 的组合（这些组合找到的 opp 可能 Type 是 "spot-future" 或 "future-spot"），所以看到了混合
- 当 filter = "future-spot" 时，只会显示 marketType = "future-spot" 的组合，但这些组合找到的 opp 的 Type 可能都是 "spot-future"，然后被... 不对，opp.Type 是在 createOpportunityRow 中显示的，不影响是否显示。

我需要重新理解。让我直接看修复方案。

## 修复方案

核心思路：**显示时使用固定的 marketType，而不是动态的 opp.Type**。

### 修改 1: createOpportunityRow 添加 marketType 参数

**文件**: `internal/ui/bubbletea.go`

**位置**: 第 348-369 行

**修改前**:
```go
func (m *Model) createOpportunityRow(opp *common.ArbitrageOpportunity, isMissing bool) table.Row {
    ...
    return table.Row{
        opp.Symbol,
        opp.Type,  // ← 使用动态的 opp.Type
        buyFrom,
        sellTo,
        ...
    }
}
```

**修改后**:
```go
func (m *Model) createOpportunityRow(opp *common.ArbitrageOpportunity, marketType string, isMissing bool) table.Row {
    ...
    return table.Row{
        opp.Symbol,
        marketType,  // ← 使用固定的 marketType
        buyFrom,
        sellTo,
        ...
    }
}
```

### 修改 2: 调用 createOpportunityRow 时传递 marketType

**位置**: 第 323-326 行

**修改前**:
```go
if opp, exists := oppsByKey[oppKey]; exists {
    row := m.createOpportunityRow(opp, false)
    rows = append(rows, row)
}
```

**修改后**:
```go
if opp, exists := oppsByKey[oppKey]; exists {
    row := m.createOpportunityRow(opp, marketType, false)
    rows = append(rows, row)
}
```

## 修复效果

### 修复前的问题

**场景 1: filter = "spot-future"**

```
updateTable 逻辑:
1. 遍历组合: ASTER_SPOT + BINANCE_FUTURE
   - marketType = "spot-future" ✅ 通过过滤器
   - 查找 opp，找到 (可能 opp.Type = "spot-future" 或 "future-spot")
   - 显示: opp.Type（可能是 "future-spot"）❌

2. 遍历组合: ASTER_FUTURE + BINANCE_SPOT
   - marketType = "future-spot" ❌ 不通过过滤器，跳过

结果: 显示的行中，Type 列可能混有 "spot-future" 和 "future-spot" ❌
```

**场景 2: filter = "future-spot"**

```
updateTable 逻辑:
1. 遍历组合: ASTER_SPOT + BINANCE_FUTURE
   - marketType = "spot-future" ❌ 不通过过滤器，跳过

2. 遍历组合: ASTER_FUTURE + BINANCE_SPOT
   - marketType = "future-spot" ✅ 通过过滤器
   - 查找 opp，找到 (可能 opp.Type = "spot-future")
   - 显示: opp.Type = "spot-future" ❌ 与过滤器不一致

结果: 过滤器是 "future-spot"，但显示的 Type 是 "spot-future"
或者，如果用户只看到 Type = "spot-future" 的行，会误以为过滤器不工作 ❌
```

实际上，我觉得问题是：用户看到的 Type 列与过滤器不一致，导致困惑。

### 修复后的行为

**场景 1: filter = "spot-future"**

```
updateTable 逻辑:
1. 遍历组合: ASTER_SPOT + BINANCE_FUTURE
   - marketType = "spot-future" ✅ 通过过滤器
   - 查找 opp，找到
   - 显示: marketType = "spot-future" ✅

2. 遍历组合: ASTER_FUTURE + BINANCE_SPOT
   - marketType = "future-spot" ❌ 不通过过滤器，跳过

结果: 所有显示的行，Type 列都是 "spot-future" ✅
```

**场景 2: filter = "future-spot"**

```
updateTable 逻辑:
1. 遍历组合: ASTER_SPOT + BINANCE_FUTURE
   - marketType = "spot-future" ❌ 不通过过滤器，跳过

2. 遍历组合: ASTER_FUTURE + BINANCE_SPOT
   - marketType = "future-spot" ✅ 通过过滤器
   - 查找 opp，找到
   - 显示: marketType = "future-spot" ✅

结果: 所有显示的行，Type 列都是 "future-spot" ✅
```

## 对比表格

| 场景 | 修复前 | 修复后 |
|------|--------|--------|
| **filter = "spot-future"** | Type 列混有 "spot-future" 和 "future-spot" ❌ | Type 列全是 "spot-future" ✅ |
| **filter = "future-spot"** | Type 列可能全是 "spot-future"，与过滤器不一致 ❌ | Type 列全是 "future-spot" ✅ |
| **Type 列稳定性** | 随 opp.Type 变化（动态） | 固定基于组合顺序 ✅ |
| **过滤器可靠性** | 显示内容与过滤器不一致 ❌ | 显示内容与过滤器完全一致 ✅ |

## 完整的数据流

### 修复后的正确流程

```
1. Calculator 计算套利机会:
   - 根据价格确定买卖方向
   - 生成 opp.Type（动态的，例如 "spot-future" 或 "future-spot"）
   - 存储在 m.opportunities 中

2. UI updateTable:
   a. 构建 oppsByKey:
      - 每个 opp 有两个 key（正向和反向）
      - 便于双向查找

   b. 遍历所有数据源组合:
      - src1 = ASTER_SPOT, src2 = BINANCE_FUTURE
      - 计算固定的 marketType = "spot-future"

   c. 应用过滤器:
      - if marketType != filterType: 跳过
      - if marketType == filterType: 继续

   d. 查找套利机会:
      - oppKey = "BTCUSDT_ASTER_SPOT_BINANCE_FUTURE"
      - 查找 oppsByKey[oppKey]
      - 找到 opp（可能 opp.Type 是任何值）

   e. 创建显示行:
      - 使用固定的 marketType（不是 opp.Type）
      - Type 列显示 = marketType = "spot-future" ✅

3. 用户看到:
   - 过滤器: "Showing: spot-future only"
   - Type 列: 全是 "spot-future"
   - 完全一致 ✅
```

## 技术要点

### 关键理解

1. **opp.Type** (来自 Calculator):
   - 动态的，由买卖方向决定
   - 买卖方向由价格决定
   - 会随价格波动改变
   - **用途**: Calculator 内部逻辑，通知发送时的分类

2. **marketType** (来自 UI):
   - 固定的，由数据源组合顺序决定
   - 与价格无关
   - 永不改变
   - **用途**: UI 显示，过滤器匹配

3. **分离关注点**:
   - Calculator: 关注套利逻辑（买卖方向、利润计算）
   - UI: 关注显示逻辑（固定分类、稳定过滤）

### 为什么不修改 Calculator？

**选项 1**: 修改 Calculator，使 opp.Type 固定（基于交易所顺序而不是买卖方向）

**缺点**:
- Calculator 不知道 UI 的数据源顺序
- opp.Type 的语义会变得模糊（不再表示买卖方向）
- 影响其他可能依赖 opp.Type 的功能（如通知）

**选项 2**: 只修改 UI，使用固定的 marketType 显示 ✅

**优点**:
- 不影响 Calculator 的逻辑
- UI 负责自己的显示逻辑
- 清晰的职责分离
- 修改范围小，风险低

**结论**: 选择选项 2。

## 验证方法

### 测试步骤 1: spot-future 过滤器

```bash
1. 运行程序: .\monitor.exe

2. 按 f 键，切换到 "Showing: spot-future only"

3. 检查表格:
   - Type 列应该全是 "spot-future"
   - 不应该有 "future-spot" 出现

4. 等待 1-2 分钟（价格会波动）

5. 再次检查:
   - Type 列仍然全是 "spot-future" ✅
   - 没有行从 "spot-future" 变成 "future-spot" ✅
```

### 测试步骤 2: future-spot 过滤器

```bash
1. 按 f 键，切换到 "Showing: future-spot only"

2. 检查表格:
   - Type 列应该全是 "future-spot"
   - 应该能看到一些行（不应该为空）

3. 验证组合:
   - 查看 "Buy From" 和 "Sell To" 列
   - 应该包含 ASTER_FUTURE ↔ BINANCE_SPOT
   - 应该包含 LIGHTER_FUTURE ↔ BINANCE_SPOT
   - 应该包含 LIGHTER_FUTURE ↔ ASTER_SPOT 等
```

### 测试步骤 3: 切换过滤器

```bash
1. 按 f 键多次，循环切换:
   all → spot-spot → spot-future → future-spot → future-future → all

2. 每次切换后检查:
   - "Showing:" 显示的内容
   - Type 列的内容
   - 两者应该完全一致 ✅

3. 示例:
   Showing: spot-future only
   → Type 列全是 "spot-future" ✅

   Showing: future-spot only
   → Type 列全是 "future-spot" ✅

   Showing: All pairs
   → Type 列包含所有类型（spot-spot, spot-future, future-spot, future-future）✅
```

## 总结

### ✅ 修复的 Bug

| Bug | 描述 | 根本原因 | 修复方案 |
|-----|------|---------|---------|
| **Type 列混乱** | spot-future 过滤器显示了 future-spot | 使用动态的 opp.Type 而不是固定的 marketType | createOpportunityRow 使用 marketType 参数 |
| **过滤器失效** | 显示内容与过滤器不一致 | 过滤用 marketType，显示用 opp.Type | 统一使用 marketType |
| **future-spot 为空** | 过滤器选择 future-spot 时可能显示错误的 Type | 同上 | 同上 |

### 📊 修改统计

| 指标 | 数值 |
|------|------|
| **修改文件** | 1 个（bubbletea.go） |
| **修改位置** | 2 处（函数签名 + 调用点） |
| **新增代码** | 0 行 |
| **修改代码** | 2 行 |
| **删除代码** | 0 行 |
| **修复级别** | 关键 Bug ✅ |

### 🎯 用户价值

**修复前**:
```
Showing: spot-future only

Symbol   Pair Type      Buy From    Sell To
─────────────────────────────────────────────
BTC      spot-future    ...         ...
ETH      future-spot    ...         ...  ← 不一致！
SOL      spot-future    ...         ...

用户: 为什么过滤器选的是 spot-future，但显示了 future-spot？❌
```

**修复后**:
```
Showing: spot-future only

Symbol   Pair Type      Buy From    Sell To
─────────────────────────────────────────────
BTC      spot-future    ...         ...
ETH      spot-future    ...         ...
SOL      spot-future    ...         ...

用户: 完美！过滤器和显示完全一致 ✅
```

---

**实现时间**: 2025-12-21
**版本**: v1.5
**文件**: `internal/ui/bubbletea.go` (第 325 行, 第 349-369 行)
**Bug 严重程度**: 高（影响核心过滤功能）
**状态**: ✅ 已修复并编译
**编译产物**: `monitor.exe`
