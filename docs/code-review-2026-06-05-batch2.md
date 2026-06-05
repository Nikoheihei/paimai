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
