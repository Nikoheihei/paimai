# Review 批次 006-server-entry：服务端入口与路由

> 审查范围：`server/main.go`、`server/internal/handler/public.go`、`server/internal/handler/admin.go`、`server/internal/handler/settle.go`、`server/internal/handler/room.go`、`server/pkg/middleware/cors.go`、`server/pkg/middleware/auth.go`
> 审查日期：2026-06-04（初版）、2026-06-04（复查）、2026-06-04（二次复查）、2026-06-04（三次复查）

---

### [P0] Admin 路由无角色鉴权 ✅ 已修复

- **文件**：`server/main.go:94-95`
- **状态**：**已修复**。`main.go` 中 admin 路由组已挂载 `middleware.AdminRequired()` 中间件：
  ```go
  adminGroup := r.Group("/api/admin")
  adminGroup.Use(middleware.AdminRequired())
  ```
  `AdminRequired()` 中间件（`auth.go:63-83`）校验 `role == "seller" || role == "anchor"`，非管理员返回 403。

---

### [P1] CORS Allow-Origin:* + Allow-Credentials:true 矛盾 ✅ 已修复

- **文件**：`server/pkg/middleware/cors.go:14-21`
- **状态**：**已修复**。`CORS()` 中间件已改为动态设置 `Access-Control-Allow-Origin`：
  - localhost/127.0.0.1 来源：设置 `Allow-Credentials: true`
  - 其他来源：设置 `Allow-Origin` 但不设置 `Allow-Credentials`
  - 不再使用 `*`

---

### [P1] 多个 handler 缺少数据归属校验 ✅ 已修复

- **文件**：`server/internal/handler/settle.go:48`、`server/internal/handler/room.go:33,38`
- **状态**：**已修复**。
  - `settle.go:48` `listOrders` 已用 `mustGetUserID(c)` 获取 sellerID ✅
  - `room.go:33` `createRoom` 已用 `mustGetUserID(c)` ✅
  - `room.go:38` `listRooms` 已用 `mustGetUserID(c)` ✅
  - `public.go:138,148` `getBuyerOrder`/`payBuyerOrder` 已传 `mustGetUserID(c)` 给 service ✅
  - service 层 `GetBuyerOrder`/`PayBuyerOrder` 已校验订单归属 ✅

---

### [P1] 多处 `c.Get("userId").(uint64)` 无安全断言 ✅ 已修复

- **文件**：`server/internal/handler/admin.go`、`server/internal/handler/settle.go`、`server/internal/handler/room.go`、`server/internal/handler/public.go`
- **状态**：**已修复**。所有 handler 已统一使用 `mustGetUserID(c)` 安全断言函数，不再有裸断言 `.(uint64)`。

---

### [P2] Stream Consumer 使用 context.Background() 无法优雅关闭 ✅ 已修复

- **文件**：`server/main.go:52-54`
- **状态**：**已修复**。已使用 `context.WithCancel`：
  ```go
  streamCtx, streamCancel := context.WithCancel(context.Background())
  defer streamCancel()
  go streamConsumer.Start(streamCtx)
  ```

---

### [P2] DB 连接失败时服务空跑 ❌ 未修复（设计决策）

- **文件**：`server/main.go:27-33`
- **类型**：逻辑错误
- **描述**：若 `db.InitDB` 失败，`database == nil`，所有路由注册被跳过。服务器仍然启动并监听端口，但只有 `/ping` 可响应。
- **影响面**：MySQL 不可用时服务看起来正常但功能全部丧失，排查困难。
- **建议修复**：DB 初始化失败时 `log.Fatal` 退出，或启动健康检查端点返回 unhealthy。
- **当前状态**：团队决策保留当前行为，允许无 DB 时编译运行。

---

### [P3] SettleExpiredAuctions 启动时同步执行，无超时 ❌ 未修复

- **文件**：`server/main.go:109`
- **类型**：性能隐患
- **描述**：`settleService.SettleExpiredAuctions(context.Background())` 无超时控制。若过期竞拍数量多或 DB 慢，会延迟服务就绪。
- **影响面**：重启部署时服务就绪时间变长。
- **建议修复**：使用带超时的 context（如 30s），或移至后台 goroutine 异步执行。

---

### [P3] Gin 未设置 ReleaseMode ❌ 未修复

- **文件**：`server/main.go:58`
- **类型**：性能隐患
- **描述**：`gin.Default()` 默认使用 debug 模式，输出大量路由和请求日志。
- **影响面**：日志量大，性能轻微影响。
- **建议修复**：根据配置设置 `gin.SetMode(gin.ReleaseMode)`。

---

### 🆕 [P2] main.go AllowAllOrigins 硬编码 ✅ 已修复

- **文件**：`server/main.go:105`
- **状态**：**已修复**。已从 `cfg.AllowAllWebSocketOrigins` 读取：
  ```go
  upgraderCfg := &handler.UpgraderConfig{AllowAllOrigins: cfg.AllowAllWebSocketOrigins}
  ```

---

### 路由注册顺序审计

| 路由组 | 注册位置 | 鉴权中间件 | 角色检查 | 状态 |
|---|---|---|---|---|
| `/api/auth/register` | main.go:80 | ❌ 无需 | N/A | ✅ 正确 |
| `/api/auth/login` | main.go:80 | ❌ 无需 | N/A | ✅ 正确 |
| `/ping` | main.go:64 | ❌ 无需 | N/A | ✅ 正确 |
| `/api/admin/*` | main.go:94-98 | ✅ AuthRequired | ✅ AdminRequired | ✅ 正确 |
| `/api/auth/me` | main.go:100 | ✅ AuthRequired | N/A | ✅ 正确 |
| `/api/auctions/*` | main.go:106 | ✅ AuthRequired | N/A | ✅ 正确 |
| `/api/rooms/:roomId/ws` | main.go:106 | ✅ AuthRequired | N/A | ✅ 正确 |

---

### 测试覆盖评估

| 审查重点 | 覆盖情况 |
|---|---|
| 路由注册顺序 | ❌ 无集成测试 |
| Admin 路由角色鉴权 | ✅ `TestAdminRequiredNoRole/BuyerRole/SellerRole/AnchorRole` |
| CORS 配置 | ❌ 未覆盖 |
| 数据归属校验（越权） | ❌ 未覆盖 |
| userId 安全断言 | ⚠️ `mustGetUserID` 无独立单元测试 |
| Stream Consumer 优雅关闭 | ❌ 未覆盖 |
| DB 连接失败时的行为 | ❌ 未覆盖 |
| SettleExpiredAuctions 启动超时 | ❌ 未覆盖 |

**测试通过率**：因 `JWT_SECRET` 环境变量未设置，service 包测试 panic，无法获取准确通过率。建议 CI 中统一设置 `JWT_SECRET` 环境变量。

---

### 修复记录

| 编号 | 问题 | 状态 |
|---|---|---|
| 1 | [P0] Admin 路由无角色鉴权 | ✅ 已修复（`AdminRequired` 中间件） |
| 2 | [P1] CORS Allow-Origin:* + Allow-Credentials:true 矛盾 | ✅ 已修复（动态 Origin + 白名单） |
| 3 | [P1] 多处 handler 缺少数据归属校验 | ✅ 已修复（`mustGetUserID` + service 层归属校验） |
| 4 | [P1] `c.Get("userId").(uint64)` 无安全断言 | ✅ 已修复（`mustGetUserID` 统一使用） |
| 5 | [P2] Stream Consumer context.Background 无法优雅关闭 | ✅ 已修复（`context.WithCancel`） |
| 6 | [P2] DB 连接失败时服务空跑 | ❌ 设计决策（允许编译运行，有日志告警） |
| 7 | [P3] SettleExpiredAuctions 启动时同步执行无超时 | ❌ 未修复（低优） |
| 8 | [P3] Gin 未设置 ReleaseMode | ❌ 未修复（低优） |
| 9 | [🆕 P1] `getBuyerOrder`/`payBuyerOrder` 缺少用户归属校验 | ✅ 已修复（传 userID + service 层校验） |
| 10 | [🆕 P2] AllowAllOrigins 硬编码为 true | ✅ 已修复（从 env 读取） |

**修复率：7/10（70%）**，剩余 3 项中 1 项为设计决策，2 项为低优 P3。
