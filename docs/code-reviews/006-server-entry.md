# Review 批次 006-server-entry：服务端入口与路由

> 审查范围：`server/main.go`、`server/internal/handler/public.go`、`server/internal/handler/admin.go`、`server/internal/handler/settle.go`、`server/internal/handler/room.go`、`server/pkg/middleware/cors.go`
> 审查日期：2026-06-04

---

### [P0] Admin 路由无角色鉴权，任何登录用户可操作管理接口

- **文件**：`server/main.go:82-87`、`server/internal/handler/admin.go:24-42`
- **类型**：逻辑错误
- **描述**：所有 admin 路由仅受 `AuthRequired()` 中间件保护（验证 JWT 有效），但未检查用户角色。任何 `buyer` 角色的用户都可以：创建/删除商品、创建/启动/取消竞拍、手动结算、查看所有订单。JWT 中已有 `role` 字段且中间件已注入 `c.Set("role", claims.Role)`，但未被使用。
- **影响面**：普通买家可冒充商家执行所有管理操作，包括启动/取消竞拍、触发结算、查看其他商家订单。
- **建议修复**：增加 `AdminRequired()` 中间件，校验 `c.Get("role") == "seller" || c.Get("role") == "anchor"`，非管理员角色返回 403。Admin 路由组使用此中间件。

---

### [P1] CORS 配置 Allow-Origin: * 与 Allow-Credentials: true 矛盾

- **文件**：`server/pkg/middleware/cors.go:12-13`
- **类型**：逻辑错误
- **描述**：`Access-Control-Allow-Origin: *` 和 `Access-Control-Allow-Credentials: true` 不能同时生效。浏览器规范要求当 `Credentials: true` 时，`Allow-Origin` 必须是具体域名，不能是通配符 `*`。实际效果是：跨域携带 Cookie/Authorization 的请求被浏览器拒绝。
- **影响面**：前端跨域部署时，所有鉴权请求被浏览器 CORS 策略拦截。
- **建议修复**：从请求 `Origin` 头动态设置 `Allow-Origin`（白名单校验），或移除 `Allow-Credentials: true`（若不需要 Cookie）。

---

### [P1] 多个 handler 缺少数据归属校验，存在越权风险

- **文件**：`server/internal/handler/admin.go:146-153`、`server/internal/handler/settle.go:37-44`
- **类型**：逻辑错误
- **描述**：
  1. `getOrder`：`h.service.GetOrder(ctx, id)` 未传 sellerID，任何用户可查看任意订单；
  2. `payOrder`：`h.settleService.PayOrder(ctx, id)` 未校验付款人是订单买家，任何用户可为任意订单付款；
  3. `createAuction`：未校验当前用户是 Room 的 SellerID，任何用户可在别人的直播间创建竞拍。
- **影响面**：水平越权——用户可访问/操作不属于自己的资源。
- **建议修复**：
  - `getOrder`：从 JWT 获取 sellerID，只返回该卖家的订单；
  - `payOrder`：校验当前用户是订单的 BuyerID；
  - `createAuction`：校验当前用户是 RoomID 对应直播间的 SellerID。

---

### [P1] 多处 `c.Get("userId").(uint64)` 无安全断言，中间件放行时可 panic

- **文件**：`server/internal/handler/admin.go:50-51`、`admin.go:57-58`、`settle.go:48-49`、`room.go:33-34`、`room.go:39-40`、`public.go:124-125`、`public.go:139-140`、`public.go:150-151`
- **类型**：边界遗漏
- **描述**：与 [003 P2] 同源。所有 handler 通过 `c.Get("userId").(uint64)` 获取用户 ID，但 `AuthRequired` 中间件在无 token 时放行（不设置 userId），此时 `c.Get("userId")` 返回 nil，断言 panic。
- **影响面**：当前部署（开发模式中间件）下，未登录用户访问任何鉴权路由均导致 500 panic。
- **建议修复**：统一使用安全断言工具函数：
  ```go
  func mustGetUserID(c *gin.Context) (uint64, bool) {
      v, exists := c.Get("userId")
      if !exists { return 0, false }
      uid, ok := v.(uint64)
      return uid, ok
  }
  ```

---

### [P2] Stream Consumer 使用 context.Background() 无法优雅关闭

- **文件**：`server/main.go:52`
- **类型**：边界遗漏
- **描述**：`go streamConsumer.Start(context.Background())` 的 context 永远不会取消。服务关闭时（SIGTERM），Consumer goroutine 不会被通知退出，可能在中途处理消息时被强制终止。
- **影响面**：部署更新时可能丢失正在处理的出价推送事件。
- **建议修复**：使用可取消的 context，在 shutdown hook 中调用 cancel：
  ```go
  ctx, cancel := context.WithCancel(context.Background())
  defer cancel()
  go streamConsumer.Start(ctx)
  ```

---

### [P2] DB 连接失败时服务空跑

- **文件**：`server/main.go:27-33`、`main.go:68-100`
- **类型**：逻辑错误
- **描述**：若 `db.InitDB` 失败，`database == nil`，所有路由注册被跳过（在 `if database != nil` 块内）。服务器仍然启动并监听端口，但只有 `/ping` 可响应，其余所有 API 返回 404。这种行为不直观，客户端和运维难以定位问题。
- **影响面**：MySQL 不可用时服务看起来正常但功能全部丧失，排查困难。
- **建议修复**：DB 初始化失败时 `log.Fatal` 退出，或启动健康检查端点返回 unhealthy。

---

### [P3] SettleExpiredAuctions 启动时同步执行，无超时

- **文件**：`server/main.go:97-99`
- **类型**：性能隐患
- **描述**：`settleService.SettleExpiredAuctions(context.Background())` 在服务启动时同步执行，无超时控制。若过期竞拍数量多或 DB 慢，会延迟服务就绪。且 `context.Background()` 意味着无法通过 shutdown 信号中断。
- **影响面**：重启部署时服务就绪时间变长。
- **建议修复**：使用带超时的 context（如 30s），或移至后台 goroutine 异步执行。

---

### [P3] Gin 未设置 ReleaseMode

- **文件**：`server/main.go:56`
- **类型**：性能隐患
- **描述**：`gin.Default()` 默认使用 debug 模式，输出大量路由和请求日志。生产环境应使用 `gin.SetMode(gin.ReleaseMode)`。
- **影响面**：日志量大，性能轻微影响。
- **建议修复**：根据配置设置 `gin.SetMode(gin.ReleaseMode)`。

---

### 路由注册顺序审计

| 路由组 | 注册位置 | 鉴权中间件 | 角色检查 | 状态 |
|---|---|---|---|---|
| `/api/auth/register` | main.go:74 | ❌ 无需 | N/A | ✅ 正确 |
| `/api/auth/login` | main.go:74 | ❌ 无需 | N/A | ✅ 正确 |
| `/ping` | main.go:62 | ❌ 无需 | N/A | ✅ 正确 |
| `/api/admin/*` | main.go:85 | ✅ AuthRequired | ❌ 无 | ⚠️ 缺角色检查 |
| `/api/admin/auctions/:id/settle` | main.go:86 | ✅ AuthRequired | ❌ 无 | ⚠️ 缺角色检查 |
| `/api/admin/orders/*` | main.go:86 | ✅ AuthRequired | ❌ 无 | ⚠️ 缺角色检查 |
| `/api/admin/rooms/*` | main.go:87 | ✅ AuthRequired | ❌ 无 | ⚠️ 缺角色检查 |
| `/api/auth/me` | main.go:89 | ✅ AuthRequired | N/A | ✅ 正确 |
| `/api/auctions/*` | main.go:94 | ✅ AuthRequired | N/A | ⚠️ 出价需buyer角色？ |
| `/api/rooms/:roomId/ws` | main.go:94 | ✅ AuthRequired | N/A | ⚠️ dev模式可绕过 |
