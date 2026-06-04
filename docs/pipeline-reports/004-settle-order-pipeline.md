# AI 产线交付报告 #004

> **产线**：`settle-order-pipeline`（结算与订单产线）
> **运行日期**：2026-06-02
> **产线版本**：`settle-order-pipeline.yml v1.0`
> **依赖产线**：`bid-closed-loop`（复用 adminStore 接口和竞拍状态机）
> **状态**：✅ 全部 11 个单元测试通过

---

## 一、本次生成内容

### 1.1 背景

竞拍倒计时结束后，系统需要：判断成交/流拍、生成订单、管理订单状态（pending_payment → paid）。同时需要在出价时自动触发结算（当竞拍已过期或触及封顶价成交时）。

### 1.2 修改/新增的文件

| 文件 | 改动 |
|---|---|
| `server/internal/handler/settle.go` | **新增** — 3 个 HTTP 路由：`POST /api/admin/auctions/:id/settle`、`POST /api/admin/orders/:id/pay`、`GET /api/admin/orders` |
| `server/internal/service/settle.go` | 已有（上一轮实现），新增 `SettleExpiredAuctions` 和 `SettleResult` 导出的 godoc |
| `server/internal/service/settle_test.go` | **新增** — 11 个单元测试，覆盖结算、支付、幂等、过期结算场景 |
| `server/internal/service/public.go` | 注入 `SettleService`；`PlaceBid` 中 AUCTION_ENDED 时异步触发结算；sold 时同步触发结算 |
| `server/internal/service/admin_test.go` | `adminStoreStub` 补齐 6 个订单相关方法（Create/Get/Update/ListOrders 等） |
| `server/internal/service/public_test.go` | 更新 `NewPublicService` 调用签名（新增 `nil` 参数） |
| `server/main.go` | 注入 `settleService`、注册结算路由、启动时调用 `SettleExpiredAuctions` |

### 1.3 结算触发流程

```
触发方式 1: 出价触及封顶价
  PlaceBid → Lua 返回 sold → persistAcceptedBid → s.settle.SettleAuction(sold) → 订单创建

触发方式 2: 出价时竞拍已过期
  PlaceBid → Lua 返回 AUCTION_ENDED → go 异步 s.settle.SettleAuction → 订单创建

触发方式 3: 商家关闭直播间
  POST /api/admin/auctions/:id/settle → SettleAuction → 订单创建

触发方式 4: 服务启动
  main() → SettleExpiredAuctions → 结算所有过期 running 竞拍
```

### 1.4 订单状态机

```
pending_payment ──→ paid  (模拟支付, 写死成功)
                └──→ closed (预留, 用于售后/退款)
```

### 1.5 API

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/auctions/:id/settle` | 手动结算指定竞拍 |
| POST | `/api/admin/orders/:id/pay` | 模拟支付（pending_payment → paid） |
| GET | `/api/admin/orders` | 订单列表（按创建时间倒序） |

---

## 二、验证结果

| 关卡 | 命令 | 结果 |
|---|---|---|
| 编译 | `go vet ./...` | ✅ |
| 单元测试 | `go test -run 'TestSettle\|TestPay'` | ✅ 11/11 PASS |
| 全部测试 | `go test ./internal/service/` | ✅ 35/35 PASS |

---

## 三、🟡 人工决策记录

| 决策点 | 结论 |
|---|---|
| 支付方式 | 写死 `pending_payment → paid`，不调用第三方支付 API |
| 定时器设计 | 不设定时轮询。服务 24/7 运行，出价时自动触发结算；保留手动接口供关闭直播间时调用 |
| 服务启动时结算 | 仅在服务首次启动时执行一次 `SettleExpiredAuctions`，后续依靠出价触发+手动接口 |
| 幂等策略 | 已 finished（sold/failed/cancelled）的竞拍跳过结算；已 paid 的订单跳过支付 |
| 结算接口保留 | ✅ 保留 `POST /api/admin/auctions/:id/settle`，作为关闭直播间流程的下游调用 |

---

## 四、✅ 已处理的边界情况

| 边界 | 处理方式 |
|---|---|
| 无人出价 → 流拍 | `WinnerUserID == nil` → status = failed，跳过订单 |
| 有保留价但未达到 → 流拍 | `CurrentPriceCents < ReservePriceCents` → status = failed |
| 已成交重复结算（幂等） | `status == sold` → 查已有订单返回，不重复创建 |
| 已流拍重复结算（幂等） | `status == failed/cancelled` → 直接返回 |
| 已支付重复支付（幂等） | `status == paid` → 直接返回成功 |
| 已关闭订单不可支付 | `status == closed` → 返回 `ErrInvalidTransition` |
| 不存在的竞拍/订单 | `gorm.ErrRecordNotFound` → 转 `ErrNotFound` |
| running 但未过期的竞拍手动结算 | 允许执行（商家关闭直播间场景），走正常结算逻辑 |
| 启动时只结算过期竞拍 | `end_at <= now` 过滤，未过期的不处理 |
| 出价时并发结算安全 | Lua 脚本在 Redis 层原子判定，已 sold 的竞拍再次出价返回 AUCTION_ENDED |
| 异步结算不影响出价延迟 | AUCTION_ENDED 触发的结算使用独立 goroutine + 5s timeout |

---

## 五、⏳ 未处理的边界情况（本次不做）

| 边界 | 原因 |
|---|---|
| 订单关闭（closed）的后续操作 | 售后/退款流程未定义 |
| 结算失败重试 | 单次失败仅记录日志，不重试 |
| 结算并发锁（DB 层） | 当前依赖 Redis Lua 原子性 + 幂等校验，高并发下可能重复结算（概率极低） |
| 订单分页 | `GET /api/admin/orders` 返回全部订单，未做分页 |
| 订单金额精度 | 使用「分」为单位，当前满足需求 |
| 跨境/多币种 | 当前只支持人民币分 |

---

## 六、测试覆盖

| 测试文件 | 测试函数 | 覆盖场景 |
|---|---|---|
| `settle_test.go` | 11 个测试 | 成交、流拍、保留价不足、幂等、支付、过期结算 |
| `admin_test.go` | — | `adminStoreStub` 补齐 6 个接口方法 |

## 三、建议人工 Code Review 的重点

### 🔴 高优先级（请务必审查）

1. **结算时序：出价结束触发的异步结算**
   - 文件：`internal/service/public.go` 的 `persistAcceptedBid`
   - AUCTION_ENDED 时用 `go` 关键字异步调用 `SettleAuction`，不等待结算完成就返回 HTTP 200
   - **请确认这个异步策略的业务含义可接受**：用户收到"竞拍已结束"但订单还没生成（极端情况下生成失败用户不会感知）

2. **服务启动时结算的筛选条件**
   - 文件：`server/main.go` 启动时调用 `SettleExpiredAuctions`
   - 筛选条件为 `end_at <= now AND status = 'running'`
   - **请确认这个范围不会误结算正常运行的竞拍**：时区、服务器时间偏差都可能导致误判

### 🟡 中等优先级

3. **手动结算接口的并发安全**
   - `POST /api/admin/auctions/:id/settle` 和出价触发的结算可能同时对同一个竞拍执行
   - 当前靠幂等校验（查已有订单）来避免重复，但不保证事务隔离
   - **高并发下可能出现重复订单**，概率极低但需知晓

4. **支付接口的扩展性**
   - 当前支付写死成功（`pending_payment → paid`），status hardcode 在 handler 中
   - 后续接真实支付 API 时，需要将支付状态回调处理改造成异步

### 🟢 低优先级

5. **订单列表无分页**
   - `GET /api/admin/orders` 返回全部订单
   - 数据量大时需增加分页

---

## 四、与规则库的对账

| 规则 ID | 状态 | 备注 |
|---|---|---|
| `settle-on-sold` | ✅ 已覆盖 | 封顶价成交立即结算 |
| `settle-on-ended` | ✅ 已覆盖 | 过期竞拍出价时异步结算 |
| `settle-on-startup` | ✅ 已覆盖 | 服务启动时结算过期竞拍 |
| `settle-manual` | ✅ 已覆盖 | 手动结算接口 |
| `settle-idempotent` | ✅ 已覆盖 | 已结算竞拍跳过、已支付跳过 |
| `settle-failed-no-order` | ✅ 已覆盖 | 流拍不生成订单 |
| `pay-success` | 📌 人工决策 | 写死支付成功，不调第三方 |
| `order- pagination` | ⏳ 待覆盖 | 无分页 |


---

## 七、产线良率

| 指标 | 本次值 |
|---|---|
| 产线轮次 | 1 轮 |
| 新增文件 | 2 |
| 修改文件 | 4 |
| 新增单元测试 | 11 |
| 一次性编译通过 | 否（3 次迭代：test stub 缺方法 → public_test 缺参数 → unused import） |
