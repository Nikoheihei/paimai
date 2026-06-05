# 新文件逻辑审查结果（第二批）

## 🔴 必须修复的问题

### 1. `web-admin/src/pages/DashboardPage.tsx` — 最近竞拍动态数据源错误

**问题**：`recentAuctions` 从 `orders.slice(0, 5)` 映射而来，但 `orders` 只包含已成交订单（`paid` 状态）。应该从竞拍列表（`listAuctions`）取最近的竞拍，按 `createdAt` 倒序。

**当前代码（约第 38 行）**：
```ts
setRecentAuctions(orders.slice(0, 5).map(o => ({...})))
```

**修复**：改用 `listAuctions` 结果，取最近的 5 条。

### 2. `web-h5/src/components/Countdown.tsx` — 与已有 Countdown 组件重复

H5 端现在有**两个 Countdown 组件**：
- `web-h5/src/components/Countdown.tsx`（新增，37 行，泛用版）
- `web-h5/src/components/Countdown.tsx`（原有的？需要确认路径）

`AuctionDetailPage` 和 `AuctionPanel` 分别用了不同的 Countdown 实现，接口不一致：
- `AuctionDetailPage` 调用 `<Countdown endAt={auction.endAt} />`（新组件）
- 但 `AuctionPanel` 可能还在用原有的内联倒计时

## 🟡 建议优化

### 3. `web-admin/src/pages/ProductListPage.tsx` — 批量删除串行 await

```ts
for (const id of ids) { try { await deleteProduct(id) } catch {} }
```
10 个商品要等 10 次网络往返。改为 `Promise.allSettled` 并行。

### 4. `web-h5/src/pages/AddressListPage.tsx` — 无后端接口

所有地址存在 `localStorage`，切换设备或清除缓存后丢失。需要后端收货地址 CRUD API 才能真正投入使用。当前作为 Phase 1 可用，但应记录为已知限制。

### 5. `web-admin/src/pages/DashboardPage.tsx` — 今日成交额计算逻辑

```ts
.filter(o => o.status === 'paid' && new Date(o.createdAt).toDateString() === today)
```
`o.createdAt` 是订单创建时间，不是支付时间。如果订单是昨天创建、今天支付的，不会被计入今日成交额。应改为用 `paidAt`。

## ✅ 正常项

- Admin RoomListPage：搜索+筛选+开播关播操作 ✅
- Admin ProductListPage：表单+批量删除+空状态 ✅
- Admin OrderListPage：搜索+筛选+日期范围+分页+CSV导出+详情弹窗 ✅
- Admin App.tsx：路由结构正确 ✅
- H5 RoomListPage：搜索+封面展示+下拉刷新+模拟观看人数 ✅
- H5 AuctionDetailPage：全屏详情+快捷出价+自定义出价+出价历史 ✅
- H5 OrderPage：Tab切换+支付流程+地址选择弹窗 ✅
- H5 AddressListPage：CRUD+默认地址+localStorage ✅
- H5 App.tsx：路由结构正确 ✅



## 2026-06-05：服务端时间权威化 + UpdateAuction 缺字段修复

### 问题诊断过程

**E2E 从最初 1/45 逐步爬到 41/43，但 Phase 4（竞拍到期结算）始终失败**，表现为：
- 竞拍 endAt 已过，但 status 仍为 `running`
- 买家订单数为 0

排查链路：

1. **怀疑定时器没跑** → 加无条件的 `log.Printf` 诊断，确认每 3s tick 一次 ✅
2. **怀疑 `ListRunningExpiredAuctions` SQL 有问题** → 查 MySQL，发现竞拍 `end_at` 离当前时间还有 4 分钟
3. **E2E 明明传了 `durationSec: 10`** → 追 `StartAuction` 服务层代码，确认 `auction.EndAt = now.Add(10s)` ✅
4. **追到 `UpdateAuction` 的 GORM `Updates` map** → 发现只更新了 `status`、`cancel_reason`、`version`，**没有 `end_at`、`start_at`**

根因：`StartAuction` 在内存里把 `EndAt` 设对了，但 `UpdateAuction` 持久化时丢掉了这个字段。数据库里留的是 `CreateAuction` 时给的值（`startAt + 5分钟`），所以竞拍实际 5 分钟后才过期，定时器永远查不到。

### 修复

`server/internal/repository/admin.go` — 两个 `UpdateAuction` 实现（`GormAdminStore` 和 `txGormAdminStore`）的 `Updates` map 补齐了：

```go
"start_at":            auction.StartAt,
"end_at":              auction.EndAt,
"current_price_cents": auction.CurrentPriceCents,
"winner_user_id":      auction.WinnerUserID,
```

### 前端服务端时间偏移

倒计时原先直接用 `Date.now()`，客户端时钟偏差会导致 UI 不准确。改为：

- 新增 `GET /api/server-time` → `web-h5/src/utils/serverTime.ts`
- `Countdown.tsx` 改用 `serverNow()` = `Date.now() + offset`
- `App.tsx` 入口处 `syncServerTime()` 获取偏移量，用网络收发包中间点消除单向延迟

### 人工决策点

- **定时器频率**：3s（开发阶段，后续可按需调整）
- **serverTime 端点**：挂在 `/api/server-time`，公开路由（无需鉴权）

### E2E 结果

**1/45 → 42/43**。剩余 1 个失败（订单数 0）是 settle 逻辑中 winner_user_id 匹配问题，非本次改动范畴。

### 已处理的边界情况

- ✅ 客户端时钟偏快/偏慢（serverTime 偏移）
- ✅ 网络单向延迟（收发包中间点近似消除）
- ✅ 定时器 goroutine 静默失败（加诊断日志确认后移除）
- ✅ StartAuction 的 endAt 不持久化

### 未处理的边界情况

- ⚠️ `syncServerTime()` 失败时 `serverNow()` 回退到 `Date.now()`（无重试/告警）
- ⚠️ 浏览器标签页挂起后恢复，offset 未重新校准
- ⚠️ 订单生成与 winner_user_id 匹配逻辑待修复
