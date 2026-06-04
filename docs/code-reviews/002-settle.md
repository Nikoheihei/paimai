# Review 批次 002-settle：结算与订单

> 审查范围：`server/internal/service/settle.go`、`server/internal/service/admin.go`、`server/internal/service/settle_test.go`、`server/internal/service/admin_test.go`
> 审查日期：2026-06-04

---

### [P0] doExecuteSettle 非事务执行，UpdateAuction 与 CreateOrder 可分叉

- **文件**：`server/internal/service/settle.go:81-144`
- **类型**：逻辑错误
- **描述**：`doExecuteSettle` 中成交路径执行了三步独立 DB 操作：① `UpdateAuction(status=sold)` → ② `GetRoom` → ③ `CreateOrder`。若步骤 ① 成功但 ③ 失败（DB 连接断开、唯一约束冲突等），竞拍已标记为 `sold` 但无订单生成。后续幂等重入 `SettleAuction` 会走到 `auction.Status == "sold"` 分支，尝试 `GetOrderByAuction` 失败后注释写"尝试生成"，但代码实际落入 `auction.Status != "running"` 校验，返回错误 `只能结算进行中的竞拍, 当前状态: sold`。该竞拍永远卡在 `sold` 状态无法生成订单。
- **影响面**：任何导致 `CreateOrder` 失败的瞬时故障都会使竞拍永久无法生成订单，买家无法付款，卖家无法收款。
- **建议修复**：
  1. 将 ①②③ 包裹在同一个 DB 事务中（`repository.WithTx`），① 失败则全部回滚；
  2. 修复幂等分支：`sold` 状态但无订单时应补创建订单，而非返回错误；
  3. 或采用 Saga 补偿模式：`CreateOrder` 失败时回退 auction 状态为 `running`。

---

### [P1] 并发结算缺少乐观锁，可重复生成订单

- **文件**：`server/internal/service/settle.go:41-78`、`server/internal/service/admin.go:267-284`
- **类型**：并发安全
- **描述**：`SettleAuction` 读取 auction 后判断状态为 `running`，再调用 `doExecuteSettle` 更新状态。两个并发请求可同时通过状态检查，各自执行 `doExecuteSettle`，导致：① 竞拍状态被覆盖两次（虽然都是 `sold`/`failed`，结果一致）；② `CreateOrder` 执行两次，可能生成重复订单（若 auctionID 唯一索引则第二次报错，导致一个请求失败但 DB 状态已变）。`model.Auction` 已有 `Version int32` 字段但从未使用。
- **影响面**：多触发源（出价成交 + 过期结算 + 手动结算）并发场景下可产生重复订单或请求失败。
- **建议修复**：在 `UpdateAuction` 中加入乐观锁 `WHERE version = :old_version`，版本不匹配时返回错误让调用方重试。或在 DB 层使用 `SELECT ... FOR UPDATE` 排他锁。

---

### [P1] StartAuction 先写 DB 后初始化 Redis 缓存，缓存失败无法回滚

- **文件**：`server/internal/service/admin.go:225-253`
- **类型**：逻辑错误
- **描述**：`StartAuction` 先将 auction 更新为 `running` 并写入 DB（第 247 行），再调用 `initAuctionCache` 写入 Redis（第 250 行）。若 `initAuctionCache` 失败（Redis 不可用），函数返回错误，但 DB 中 auction 已是 `running` 状态。此时所有出价请求因 Redis 缓存缺失返回 `AUCTION_CACHE_MISSING`，竞拍实质上不可用，且状态已无法回退。
- **影响面**：Redis 短暂不可用时，正在启动的竞拍会进入"DB 已启动但 Redis 未初始化"的僵尸状态。
- **建议修复**：
  1. 先初始化 Redis 缓存，再更新 DB 状态（Redis 写入可重试，DB 状态变更是"事实"）；
  2. 或在 `initAuctionCache` 失败时回退 DB 状态为 `scheduled`；
  3. 或在出价路径中增加缓存懒加载机制：`AUCTION_CACHE_MISSING` 时从 DB 加载缓存后重试出价。

---

### [P1] initAuctionCache WaitReplicas 失败导致启动假失败

- **文件**：`server/internal/service/admin.go:326-327`
- **类型**：逻辑错误
- **描述**：与批次 001 同源问题。`initAuctionCache` 最后调用 `WaitReplicas(ctx, 1, 50ms)`，若副本未及时确认则返回错误。此时 DB 已更新、Redis Master 已写入，但调用方收到错误可能认为启动失败。
- **影响面**：主从延迟波动时，竞拍启动返回 500 但实际已生效。
- **建议修复**：降级为日志告警，不阻塞启动流程。

---

### [P2] SettleExpiredAuctions 部分失败中断，剩余竞拍不结算

- **文件**：`server/internal/service/settle.go:184-199`
- **类型**：边界遗漏
- **描述**：`SettleExpiredAuctions` 在循环中遇到第一个 `doExecuteSettle` 错误即中断返回，后续过期竞拍不会被结算。如果某个竞拍因 DB 暂时故障无法结算，其余正常竞拍也被跳过。
- **影响面**：服务重启时批量结算过期竞拍，一个异常记录可阻塞全部后续结算。
- **建议修复**：收集错误继续处理，最后汇总报告失败列表。或至少将失败记录记入日志/告警，不中断循环。

---

### [P2] SettleAuction 幂等分支 sold 无订单时未补创建

- **文件**：`server/internal/service/settle.go:51-63`
- **类型**：逻辑错误
- **描述**：当 `auction.Status == "sold"` 但 `GetOrderByAuction` 返回错误时，注释写"数据异常，尝试生成"，但代码实际没有生成订单——它只是跳出了 `if` 块，随后被 `auction.Status != "running"` 拦截并返回错误。该注释与实现不一致，且未提供数据修复路径。
- **影响面**：与 [P0] 关联——`doExecuteSettle` 中 `CreateOrder` 失败后，此幂等分支无法自愈。
- **建议修复**：在 `GetOrderByAuction` 失败后，应继续执行订单创建逻辑（从 auction 获取 winner、从 Room 获取 SellerID），补生成缺失订单。

---

### [P2] transitionAuction/StartAuction 无乐观锁保护

- **文件**：`server/internal/service/admin.go:267-284`、`admin.go:225-253`
- **类型**：并发安全
- **描述**：与 [P1] 同源。`transitionAuction` 和 `StartAuction` 都是"读→改→写"模式，无版本校验。并发请求可导致后写入覆盖先写入。例如两个 `StartAuction` 请求可能同时读到 `scheduled` 状态，都执行成功但后者覆盖前者的时间设置。
- **影响面**：管理端并发操作（如误触双击"开始竞拍"）可能导致状态或时间被意外覆盖。
- **建议修复**：使用 `Version` 字段做乐观锁，或对关键操作加分布式锁。

---

### [P3] DeleteProduct 全量加载竞拍列表

- **文件**：`server/internal/service/admin.go:450-462`
- **类型**：性能隐患
- **描述**：`DeleteProduct` 调用 `ListAuctions(ctx, repository.AuctionFilter{})` 加载全部竞拍到内存后过滤，当竞拍数量增长到万级以上时效率低下。
- **影响面**：管理端低频操作，当前规模无影响。
- **建议修复**：增加 `ListAuctionsByProduct(ctx, productID)` 仓储方法，只查询该商品关联的竞拍。

---

### [P3] validateNoExtensionRules 允许负值以外的任意值

- **文件**：`server/internal/service/admin.go:412-417`
- **类型**：边界遗漏
- **描述**：sudden_death 模式下 `validateNoExtensionRules` 只检查 `extendThreshold < 0 || extendDuration < 0`，允许正数延时参数通过。虽然不影响功能（Lua 脚本中 mode != "extension" 不走延时逻辑），但语义上 sudden_death 模式配置延时参数容易引起混淆。
- **影响面**：不影响运行时行为，仅语义清晰性。
- **建议修复**：sudden_death 模式下若传入了正数延时参数，返回警告或直接拒绝（视为配置错误）。

---

### 测试覆盖评估

| 审查重点 | 覆盖情况 |
|---|---|
| 四种触发方式的幂等性 | ⚠️ 部分覆盖（sold/failed 幂等已测，但 sold 无订单的修复路径未测） |
| doExecuteSettle 的事务完整性 | ❌ 未覆盖（无事务、CreateOrder 失败场景未测） |
| 并发结算的安全性 | ❌ 未覆盖（无并发测试） |
| 流拍/保留价未达成的判定逻辑 | ✅ 覆盖 |
| 订单状态机（pending → paid / closed） | ✅ 覆盖 |
| transitionAuction 的乐观锁 | ❌ 未覆盖（Version 字段未使用） |
| 多商家数据隔离（seller_id 过滤） | ⚠️ SellerID 查询已修复，但 ListOrdersBySeller 未测隔离正确性 |
