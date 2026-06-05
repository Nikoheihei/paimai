# 新文件逻辑审查结果

## 🔴 必须修复的问题

### 1. `server/internal/handler/upload.go` — randomString 不是随机
**问题**：`randomString` 用 `i%len(letters)` 固定取模，结果永远是 `"abcdefghijklmn"`，所有文件名相同，会上传覆盖。
**修复**：改用 `crypto/rand` 或 `math/rand` 真随机。

### 2. `web-h5/src/components/ProductFloatPanel.tsx:100` — filtered.includes 类型错误
**问题**：`filtered.includes(a => ...)` —— `includes` 期望元素值而非谓词函数，编译不通过。
**修复**：改为 `filtered = auctions.filter(a => tabConfig.filter!.includes(a.status))`

### 3. `web-h5/src/shared/types.ts` — Address 类型无后端支撑
**问题**：定义了 `Address`（收货地址）类型，但后端没有任何地址相关 API。前端引用会拿到空数据或 404。
**修复**：✅ 后端已实现 `GET/POST/PUT/DELETE /api/addresses` 完整 CRUD（内存存储）；前端 `AddressListPage` 已对接真实 API。

### 4. `web-h5/src/components/AnchorHeader.tsx` — 直播间页拿不到主播 UserInfo
**问题**：`AnchorHeader` 的 `props.info` 类型是 `UserInfo`（含 nickname/avatarUrl），但后端 `/api/rooms/:id` 只返回 `LiveRoom`（只有 sellerId）。当前页面通过 `getMe()` 拿的**自己**的信息来冒充主播信息（`setUserInfo`），这是错的。
**修复**：✅ 后端 `GetRoom` 联表查询主播信息，返回 `anchorNickname` + `anchorAvatar`；前端 `LiveRoomPage` 移除 `getMe()` 冒充逻辑，改用房间接口返回的真实主播信息。

### 5. `web-h5/src/pages/LiveRoomPage.tsx` — productNames 只用了 productId 当名称
**问题**：`const productNames` 只做了 `a.productId -> "商品 #{a.productId}"` 的映射，没有从后端加载商品名称。因为 `/api/rooms/:roomId/auctions` 只返回 `productId`，不返回商品名称。需额外调 `/api/admin/products` 或后端在竞拍列表里联表返回商品名。
**修复**：✅ 后端 `ListRoomAuctions` 联表查询商品信息，返回 `productName` + `productImage`；前端 `productNames` / `productImages` 映射优先使用后端真实数据，fallback 到 `#ID`。

## 🟡 建议优化

### 6. `web-h5/src/components/ImageUploader.tsx` — 只用 base64，没对接上传 API
当前 H5 端的 ImageUploader 只用 FileReader 读 base64 做本地预览，没调后端的 `/api/upload`。Admin 端已对接。H5 端需要补上。

### 7. `server/internal/handler/upload.go` — 上传缺少鉴权
`RegisterUploadRoutes` 挂在 `r.Group("/api")` 下，没有走 `AuthRequired()` 中间件，任何人可上传文件。

## ✅ 正常项
- Toast 机制：`.ts` 文件用 DOM API 实现，`Toast.success()` 等静态调用方式正确
- Admin Toast：独立实现，逻辑正确
- Admin ImageUploader：调用了 `/api/upload`，逻辑正确
- StatusBadge / PriceDisplay / Countdown / EmptyState / ConfirmModal / AuctionResultModal：组件逻辑无问题
- Countdown 的 `onEnd` 回调 + `endedRef` 防重复触发：正确
- AuctionResultModal 的倒计时关闭逻辑：正确
- LiveRoomPage 的 WS/结果弹窗/商品浮层联动：逻辑正确

---

# 第二批：Phase 2 新增文件审查（2026-06-05）

## 🔴 必须修复的问题

### 1. `web-admin/src/pages/DashboardPage.tsx` — 最近竞拍动态数据源错误
**问题**：`recentAuctions` 从 `orders.slice(0, 5)` 映射而来，但 `orders` 只包含已成交订单（`paid` 状态）。应改为从竞拍列表 `listAuctions()` 取最近的竞拍。
**修复**：调用 `listAuctions()` 全量取，取最近 5 条；running 计数也改为从全量竞拍中筛选。

### 2. `web-h5/src/components/Countdown.tsx` — 与已有 Countdown 组件重复
H5 端现在有**两个 Countdown 组件**，`AuctionDetailPage` 和 `AuctionPanel` 分别用了不同的 Countdown 实现，接口不一致（`endTime` vs `endAt`）。
**修复**：统一为 `endAt` prop，AuctionPanel / AuctionResultModal / AuctionDetailPage 全部使用同一个 `Countdown` 组件。

### 3. `server/internal/handler/*.go` — Admin 路由重复嵌套 `/api/admin`
**问题**：`main.go` 已创建 `r.Group("/api/admin")`，但 `admin.go`、`room.go`、`settle.go` 内部又套了一层 `r.Group("/api/admin")`，导致实际路径变为 `/api/admin/api/admin/products`，前端请求 404。
**修复**：去掉三个 handler 中多余的 `Group("/api/admin")`，直接使用传入的 `r` 注册路由。

## 🟡 建议优化

### 4. `web-admin/src/pages/ProductListPage.tsx` — 批量删除串行 await
**问题**：`for (const id of ids) { try { await deleteProduct(id) } catch {} }` 10 个商品要等 10 次网络往返。
**修复**：改为 `Promise.allSettled(ids.map(id => deleteProduct(id)))` 并行，并统计成功/失败数。

### 5. `web-h5/src/pages/AddressListPage.tsx` — 无后端接口
所有地址存在 `localStorage`，切换设备或清除缓存后丢失。
**修复**：文件头补充完整后端 API 路径标注（`GET/POST/PUT/DELETE /api/addresses`），Phase 3 补充。

### 6. `web-admin/src/pages/DashboardPage.tsx` — 今日成交额计算逻辑
**问题**：`o.createdAt` 是订单创建时间而非支付时间，跨天支付不会计入今日成交额。
**修复**：改为 `o.paidAt` 并加 null 守卫。

## ✅ 正常项（第二批）
- Admin RoomListPage：搜索+筛选+开播关播操作 ✅
- Admin ProductListPage：表单+批量删除+空状态 ✅
- Admin OrderListPage：搜索+筛选+日期范围+分页+CSV导出+详情弹窗 ✅
- Admin App.tsx：路由结构正确 ✅
- H5 RoomListPage：搜索+封面展示+下拉刷新+模拟观看人数 ✅
- H5 AuctionDetailPage：全屏详情+快捷出价+自定义出价+出价历史 ✅
- H5 OrderPage：Tab切换+支付流程+地址选择弹窗 ✅
- H5 AddressListPage：CRUD+默认地址 ✅
- H5 App.tsx：路由结构正确 ✅
- Countdown 组件已统一为 `endAt` prop，三处使用方一致 ✅

---

# 第三批：Phase 3 后端 API 补充 + Bug 修复（2026-06-05）

## ✅ 已完成

### 后端 API 补充（Phase 3）
| # | 接口 | 方法 | 说明 |
|---|------|------|------|
| 1 | `/api/admin/products/:id` | `PATCH` | 编辑商品（name/imageUrl/description） |
| 2 | `/api/admin/auctions/:id/bids` | `GET` | 出价历史（按金额降序，最多 50 条） |
| 3 | `/api/admin/rooms/:id/stats` | `GET` | WS 实时在线人数统计 |
| 4 | `/api/addresses` | `GET/POST/PUT/DELETE` | 收货地址完整 CRUD（内存存储） |
| 5 | `/api/orders/:id` | `GET` | 买家订单详情 |
| 6 | `POST /api/orders/:id/pay` | 支持 `addressId` + `addressSnapshot` | 支付时记录收货地址 |

### Order 模型扩展
- 新增 `address_id` + `address_snapshot` 字段，支付时持久化收货地址

### Bug 修复
| # | 问题 | 修复 |
|---|------|------|
| 4 | AnchorHeader 用 getMe 冒充主播 | `GetRoom` 返回真实主播信息；前端改用 `anchorNickname`/`anchorAvatar` |
| 5 | productNames 只显示 `#ID` | `ListRoomAuctions` 联表返回 `productName`/`productImage`；前端优先使用 |

### 前端对接
- `web-admin/src/api/client.ts` — 新增 `updateProduct`、`listAuctionBids`、`getRoomStats`
- `web-h5/src/api/client.ts` — 新增地址 CRUD + `getBuyerOrder` + `payBuyerOrder` 传地址
- `web-h5/src/pages/AddressListPage.tsx` — 完全重写为后端 API 对接版本
- `web-h5/src/pages/OrderPage.tsx` — 支付流程对接真实地址 API，支持地址选择 + 快照记录
