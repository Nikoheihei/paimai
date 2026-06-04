# 偏差仲裁记录

> AI 产线运行中出现的偏差案例记录。
> 每条偏差必须转化为一条新规则，防止同类问题重复出现。

## 记录格式

```markdown
### YYYY-MM-DD: [偏差标题]

- **产线**：[产线名称]  
- **AI 输出**：[AI 做了什么 / 生成了什么]  
- **偏差表现**：[哪里错了？测试怎么发现的？]  
- **根因分析**：[为什么 AI 会犯这个错？提示词模糊？规则缺失？]  
- **人的仲裁**：[人介入后做了什么决策]  
- **新增规则**：[由此产出的规则 ID，对应 auction-rules.yaml 中的条目]  
- **复测结果**：[更新规则后 AI 重跑是否通过]  
```

---

### 2026-06-02: 出价频率限制缺失

- **产线**：`bid-closed-loop`
- **AI 输出**：原有 PHP 风格的 Go 出价 Lua 脚本没有检查同一用户的出价间隔
- **偏差表现**：规则库 `auction-rules.yaml` 已定义 `minimum-bid-interval`（两次出价 >= 1 秒），但实际 Lua 脚本和 Go 代码均未实现
- **根因分析**：初始开发时未将规则库作为强制验证输入，规则库落后于代码实现
- **人的仲裁**：在 Lua 脚本中增加 `KEYS[4]`（`lastBidTsKey`），在幂等检查之前加入频率检查；Go 侧同步更新 `bidLuaResult`、`BidResult`、`runBidScript`、`bidRejectMessage`
- **新增规则**：`minimum-bid-interval`（已在规则库中，现代码级落地）
- **复测结果**：✅ 编译通过，全量 `go test ./...` 通过，新增 7 个测试用例全部通过

---

### 2026-06-02: 规则库与 API 响应字段不同步

- **产线**：`bid-closed-loop`
- **AI 输出**：新增 `tooFrequent` 字段到 `bidLuaResult` 后，忘记更新 `BidResult` 结构体和 `toBidResult` 映射
- **偏差表现**：测试 `TestBidLuaResultTooFrequent` 编译失败 — `BidResult` 缺少 `TooFrequent` 字段
- **根因分析**：修改 Lua 返回结构时，Go 侧的 DTO 层需要联动修改，没有自动化检查跨层字段一致性
- **人的仲裁**：补上 `BidResult.TooFrequent` 字段和 `toBidResult` 中的赋值
- **新增规则**：无（Go 编译器的类型检查本身已捕获此问题，属于正常迭代流程）
- **复测结果**：✅ 编译通过，测试通过

### 2026-06-02: NewPublicService 未赋值 stream 字段

- **产线**：`websocket-push`
- **AI 输出**：修改了 `NewPublicService` 签名（加 `publisher *stream.Publisher` 参数），但在返回的 `PublicService` 结构体中**没有把参数赋值给 `stream` 字段**
- **偏差表现**：全链路集成测试 `TestBidToWSFullPipeline` 失败——`PlaceBid` 成功后 WS 未收到广播。排查发现 `s.stream` 为 `nil`，`Publish` 从未执行
- **根因分析**：修改函数签名时只改了参数列表和接口调用点（`main.go`、测试文件），漏掉了函数体内部的字段赋值。Go 编译器不检查"参数有名但没用"的情况
- **人的仲裁**：补上 `stream: publisher` 赋值
- **新增规则**：无（此为常见的 Go struct 字面量遗漏问题，类型系统和编译无法捕获接口实现缺失但结构体字段赋值遗漏。建议后续加集成测试覆盖）
- **复测结果**：✅ 3 个集成测试全部通过

### 2026-06-03: Admin 路由注册在鉴权中间件之前

- **产线**：`admin-panel`（#102）
- **AI 输出**：在 `main.go` 中将 `handler.RegisterAdminRoutes(r, adminService)` 放在了 `r.Use(middleware.AuthRequired())` 之前
- **偏差表现**：`POST /api/admin/products` 返回 HTTP 500 panic：`interface conversion: interface {} is nil, not uint64`。`c.Get("userId")` 返回 nil，因为该路由没有被 auth 中间件保护
- **根因分析**：Gin 的路由注册顺序决定中间件作用范围。AI 在 `main.go` 中先注册了 admin 路由，后挂载全局中间件，导致 admin 接口绕过了鉴权。这是一个典型的「Gin 中间件注册顺序」陷阱
- **人的仲裁**：将 `RegisterAdminRoutes`、`RegisterAdminSettleRoutes`、`RegisterRoomRoutes` 全部移到 `r.Use(middleware.AuthRequired())` 之后。服务初始化（`NewAdminService`、`NewSettleService` 等）放在之前
- **新增规则**：`gin-middleware-order` — 所有需要鉴权的路由组必须注册在 `r.Use(middleware.AuthRequired())` **之后**。服务初始化可以放在之前，但路由注册必须在中间件之后
- **复测结果**：✅ 编译通过，`init-demo.sh` 跑通全流程

### 2026-06-03: SellerID 硬编码为 RoomID + 结算在 DB 更新前调用

- **产线**：`settle-order-pipeline`（#004）
- **AI 输出**：`doExecuteSettle` 中创建订单时将 `SellerID` 写死为 `auction.RoomID`。`persistAcceptedBid` 中成交结算调用在 `UpdateAuctionBidState` 之前
- **偏差表现**：订单金额为出价前的旧值；商家订单列表查不到已支付的订单
- **根因分析**：
  - `SellerID = auction.RoomID` 是#004产线留下的简化写法，当时没有直播间表的概念，后来加了 Room 表但没有回改这里
  - `SettleAuction` 在 `UpdateAuctionBidState` 之前调用，MySQL 里还是旧的价格
- **人的仲裁**：`SellerID` 改为从 `GetRoom` 查询取 `room.SellerID`；结算移到 `UpdateAuctionBidState` 之后
- **新增规则**：`settle-sellerid-from-room` — 创建订单时 `SellerID` 必须从 `LiveRoom` 表查询，禁止硬编码或复用其他 ID 字段
- **复测结果**：✅ 全量 45+ 测试通过

### 2026-06-03: Admin 路由注册在鉴权中间件之前（补记录）

- **产线**：`admin-panel`（#102）
- **AI 输出**：`handler.RegisterAdminRoutes` 放在 `r.Use(middleware.AuthRequired())` 之前
- **偏差表现**：`POST /api/admin/products` 返回 500 panic
- **根因分析**：Gin 的路由按注册顺序匹配中间件。AI 在重构 `main.go` 时没有意识到路由注册顺序的重要性，把 admin 路由放在了全局中间件前面
- **人的仲裁**：将 admin 所有路由组移到中间件之后注册
- **新增规则**：`gin-middleware-order` — 所有需要鉴权的路由组必须在 `r.Use(middleware.AuthRequired())` **之后**注册。服务初始化可以放在之前
- **复测结果**：✅ 全流程通过

### 2026-06-03: 反复修改暴露的「试错式开发」问题

- **表现**：同一个 bug 需要多个来回才修好。`init-demo.sh` 修了 5 个版本、`SellerID` 修了 2 轮、测试 stub 修了 3 轮
- **根因分析**：
  - 修问题前没有停下来完整追溯根因，而是基于表象做"看上去对的"修改
  - 改完代码没有检查相关测试和调用链
  - 修复脚本问题时不先 curl 确认 API 正常，直接改脚本反复尝试
- **人的仲裁**：修复流程改为：①确认问题表象 → ②追溯完整调用链 → ③定位根因 → ④一次改到位 → ⑤跑全部测试验证 → ⑥通知用户。不反复试探
- **新增规则**：`fix-process` — 每个 bug 修复必须经过「定位根因 → 修改 → 全量测试」三轮，缺一不可
- **复测结果**：✅ 当前所有 45+ 测试通过
