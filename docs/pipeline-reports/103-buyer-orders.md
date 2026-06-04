# AI 产线交付报告 #103

> **产线**：`buyer-orders`（买家订单闭环）
> **运行日期**：2026-06-03
> **依赖产线**：#101 用户认证、#004 结算订单
> **状态**：✅ 后端编译 + 全量测试通过，前端构建通过

---

## 一、本次生成内容

### 1.1 背景

买家之前只能在直播间出价，竞拍结束后看不到自己的订单。本次在 H5 端新增订单列表/详情/支付功能。

### 1.2 修改的文件

#### 后端

| 文件 | 改动 |
|---|---|
| `server/internal/repository/public.go` | **修改** — PublicStore 接口 + GORM 实现新增 `ListBuyerOrders`、`GetOrder` |
| `server/internal/service/public.go` | **修改** — 新增 `ListBuyerOrders`、`GetBuyerOrder`、`PayBuyerOrder` |
| `server/internal/handler/public.go` | **修改** — 新增 3 个 handler + 3 条路由 |
| `server/internal/service/public_test.go` | **修改** — publicStoreStub 补齐新接口方法 |

#### 前端 H5

| 文件 | 改动 |
|---|---|
| `src/api/client.ts` | **修改** — 新增 `Order` 类型 + `payBuyerOrder` API |
| `src/pages/OrderPage.tsx` | **新增** — 订单列表/详情/支付页面 |
| `src/App.tsx` | **修改** — Hash 路由（直播间/订单切换） |
| `src/App.css` | **修改** — 导航栏 + 订单页面样式 |

### 1.3 新增 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/orders` | 当前用户的订单列表 |
| GET | `/api/orders/:id` | 订单详情（越权校验） |
| POST | `/api/orders/:id/pay` | 模拟支付 |

### 1.4 H5 导航

```
┌─────────────────────┐
│ [直播间]  [我的订单]  │  ← 底部导航切换
├─────────────────────┤
│ 订单列表             │
│ ┌─────────────────┐ │
│ │ #1 · 竞拍 #1    │ │
│ │ ¥50.00  待付款  │ │
│ │ 06/03 12:00     │ │
│ └─────────────────┘ │
│ 点击进入详情         │
└─────────────────────┘
```

---

## 二、验证结果

| 关卡 | 命令 | 结果 |
|---|---|---|
| 后端编译 | `go vet ./...` | ✅ |
| 后端测试 | `go test ./...` | ✅ |
| 前端编译 | `tsc -b && vite build` | ✅ (205KB JS + 8.5KB CSS) |

---

## 三、人工决策记录

| 决策点 | 结论 |
|---|---|
| 订单归属校验 | 买家查询订单详情时校验 `order.buyer_id == currentUserId`，越权返回 404 |
| 路由方式 | H5 使用 Hash 路由，不引入 react-router |
| 支付入口 | H5 订单详情页 + 管理后台订单列表均可支付 |

---

## 四、已处理的边界情况

| 边界 | 处理方式 |
|---|---|
| 查别人的订单 | 返回 404（不暴露订单存在） |
| 已支付重复支付 | 幂等返回成功 |
| 已关闭订单支付 | 返回 ErrInvalidTransition |
| 订单列表为空 | 返回 `[]`，显示"暂无订单" |

---

## 三、建议人工 Code Review 的重点

### 🔴 高优先级（请务必审查）

1. **订单状态的可见性控制**
   - 文件：`internal/handler/public.go` 的 `listBuyerOrders` 和 `getBuyerOrder`
   - 买家只应看到自己的订单，卖家只应看到自己店铺的订单
   - **请确认 buyer_id 的过滤在所有订单查询中都正确实现**，避免越权查看

2. **支付接口的幂等性与单向性**
   - `POST /api/orders/:id/pay` 只允许 `pending_payment → paid`
   - **请确认已支付的订单重复调用不会产生副作用**（当前检查了 status != pending_payment 直接返回）

### 🟡 中等优先级

3. **前端订单列表无 WS 实时更新**
   - 当前买家需要手动刷新页面看到订单状态变化
   - 后续可增加 WS 消息推送订单状态变化到买家端

4. **订单详情页缺少竞拍/商品信息关联**
   - 当前订单详情只显示价格和状态，没有关联的竞拍记录、出价次数等信息
   - **当前阶段够用，后续可以补充**

### 🟢 低优先级

5. **订单关闭后不允许重新打开**
   - 当前 `closed` 状态是终态，没有 reopen 流程
   - 售后相关需 #204 产线处理

---

## 四、与规则库的对账

| 规则 ID | 状态 | 备注 |
|---|---|---|
| `buyer-order-list` | ✅ 已覆盖 | 买家订单列表 |
| `buyer-order-detail` | ✅ 已覆盖 | 订单详情 |
| `buyer-pay-success` | ✅ 已覆盖 | 模拟支付 |
| `order-status-fsm` | ✅ 已覆盖 | pending_payment → paid / closed |
| `order-ownership` | ✅ 已覆盖 | buyer_id 过滤 |
| `order-ws-push` | ⏳ 待覆盖 | 无 WS 推送 |


## 五、产线良率

| 指标 | 本次值 |
|---|---|
| 产线轮次 | 1 轮 |
| 修改文件（后端） | 4 |
| 修改/新增文件（前端） | 4 |
| 一次性编译通过 | 否（2 次 TypeScript 修复） |
