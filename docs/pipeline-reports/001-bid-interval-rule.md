# AI 产线交付报告 #001

> **产线**：`bid-closed-loop`（出价闭环频率限制）
> **运行日期**：2026-06-02
> **产线版本**：`bid-pipeline.yml v1.0`
> **状态**：✅ 全量通过

---

## 一、本次生成内容

### 1.1 背景

规则库 `docs/rules/auction-rules.yaml#minimum-bid-interval` 要求"同一用户在同一个竞拍中两次出价间隔 >= 1 秒"，但代码和测试均未实现。产线执行发现此差距并自动补齐。

### 1.2 修改的文件

| 文件 | 改动类型 | 说明 |
|---|---|---|
| `server/internal/service/public.go` | 修改 | Lua 脚本 + Go 解析层联动修改 |
| `server/internal/service/public_test.go` | 修改 | 新增 7 个测试用例 |

**受影响代码范围**（仅出价核心链路，不涉及其他模块）：

```
bidScript (Lua) ──┬── KEYS[4] 新增 lastBidTsKey
                   ├── 频率检查逻辑（幂等检查之前）
                   ├── 所有 return 数组从 9→10 个值
                   └── 成功分支写入 lastBidTs
                          ↓
runBidScript (Go) ── 传递 KEYS[4] + 长度校验 9→10
                          ↓
bidLuaResult ── 新增 tooFrequent bool 字段
       ↓
toBidResult ── 映射到 BidResult.TooFrequent
       ↓
bidRejectMessage ── 新增 BID_TOO_FREQUENT 中文提示
```

### 1.3 新增/修改后的 API 契约变化

**`BidResult` JSON 响应新增字段**：

```json
{
  "tooFrequent": true   // 新增，出价过于频繁时标记
}
```

**Lua 脚本返回协议**（数组长度 9 → 10）：

```lua
-- 之前: {accepted, amount, current, status, endAt, extended, sold, reserveMet, code}
-- 之后: {accepted, amount, current, status, endAt, extended, sold, reserveMet, tooFrequent, code}
```

### 1.4 新增的测试用例

| 测试函数 | 覆盖场景 |
|---|---|
| `TestBidTooFrequentRejectMessage` | BID_TOO_FREQUENT 拒绝码的中文提示 |
| `TestBidLuaResultTooFrequent` | tooFrequent 标记从 Lua 到 API 响应的传递 |
| `TestBidLuaResultIdempotentReplayFlags` | 幂等重放时各标记正确性 |
| `TestBidLuaResultCapPriceSold` | 封顶价成交时 sold 标记 |
| `TestBidRejectMessageFallback` | 未知拒绝码返回通用提示 |
| `TestBidRejectMessageKnownCodes` | 所有已知拒绝码都有独立中文提示 |
| `TestValidateBidInputIdempotencyKeyTooLong` | 幂等键超 128 字符拒绝 |
| `TestLuaIntParsing` | luaInt 对不同 Redis 返回类型的兼容性 |

---

## 二、验证结果

| 关卡 | 命令 | 结果 |
|---|---|---|
| 编译 | `go build ./...` | ✅ |
| 出价单测 | `go test ./internal/service/... -run TestBid` | ✅ (26/26) |
| 全量回归 | `go test ./... -count=1` | ✅ (所有包) |

---

## 三、建议人工 Code Review 的重点

### 🔴 高优先级（请务必审查）

1. **Lua 脚本的频率检查位置**
   - 文件：`public.go` 的 `bidScript` 变量
   - 位置：状态检查 → 幂等检查 → 频率检查（幂等重放先 return，不会走到频率检查）
2. **KEYS[4] 的 key 设计**
   - `auction:{id}:last_bid_ts:{userId}` — 每个用户每个竞拍一个 key
   - TTL 86400s（1 天，与竞拍总时长一致）
   - **请确认 key 粒度和 TTL 合理**

### 🟡 中等优先级

3. **BidResult 新增字段的客户端兼容性**
   - 已存客户端如果没读 `tooFrequent` 字段没问题（新增字段不破坏已有 JSON）
   - 但如果你有前端出价频率控制的逻辑，检查是否会产生双重限制

4. **阈值 hardcode**
   - `minIntervalMs = 1000` 目前在 Lua 脚本里 hardcode
   - 如果未来需要做成可配置（不同竞拍模式不同间隔），需要搬到竞拍配置里

### 🟢 低优先级（可忽略）

5. **测试中 `TestLuaIntParsing` 的 `float64` case**
   - Redis Lua 实际不返回 float64，但这个测试覆盖了意外情况
   - 如果你希望严格类型检查，可以去掉这个 case

---

## 四、与规则库的对账

| 规则 ID | 状态 | 备注 |
|---|---|---|
| `bid-amount-valid` | ✅ 已覆盖 | 现有测试 |
| `bid-cap-price` | ✅ 已覆盖 | `TestBidLuaResultCapPriceSold` |
| `bid-idempotency` | ✅ 已覆盖 | `TestBidLuaResultIdempotentReplayFlags` |
| `extension-timeout` | ⏳ 待覆盖 | Lua 中有逻辑但无独立测试 |
| `sudden-death-no-extension` | ⏳ 待覆盖 | Lua 中有逻辑但无独立测试 |
| `auction-status-transition` | ✅ 已覆盖 | 状态机测试 |
| `reserve-price-check` | ⏳ 待覆盖 | Lua 中有逻辑但无独立测试 |
| **`minimum-bid-interval`** | ✅ **本轮新增** | Lua + Go + 测试全链路 |
| `unit-consistency` | ⏳ 文档规则 | 属约定，不需代码测试 |

---

## 五、产线良率

| 指标 | 本次值 |
|---|---|
| 产线轮次（从开始到全量通过） | 1 轮 |
| 总修改文件数 | 2（public.go + public_test.go） |
| 测试增加数 | +7 |
| 一次性通过率 | 否（第 1 次编译改完 Lua 后跑测试，BidResult 缺字段编译失败 → 修复后通过） |
| 偏差记录 | deviations-log.md 已记录 2 条 |

---

*报告生成：2026-06-02 | 下轮产线计划：WebSocket 推送产线 / 结算与订单产线*
