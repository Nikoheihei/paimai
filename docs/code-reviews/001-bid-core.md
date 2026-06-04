# 001-bid-core Review Report（新架构）

> 架构变更（2026-06-04）：
> - MySQL 是唯一写入点、唯一 Truth Source
> - Redis 只做预校验和热缓存
> - 出价事件通过 MySQL Outbox → OutboxPoller → Redis Stream → Consumer 异步分发
> - 幂等由 MySQL `(auction_id, idempotency_key)` 唯一索引保证

---

## ~~[P0] 事务内读取竞拍使用了非事务 Store，乐观锁形同虚设~~ ✅ 已修复

- **文件**：`server/internal/service/public.go:181` → `public.go:182`
- **类型**：并发安全 / 数据一致性
- **描述**：原使用 `s.store.GetAuction()` 在事务外读取，`auction.Version` 不参与事务隔离。
- **修复**：改为 `tx.GetAuction(ctx, auctionID)`，在事务内读取，配合 `WHERE version = ?` 形成真正乐观锁。

---

## ~~[P0] UpdateAuctionBidState 版本冲突时静默成功~~ ✅ 已修复

- **文件**：`server/internal/repository/admin.go:82-93` → `admin.go:83-101`
- **类型**：并发安全 / 数据一致性
- **描述**：GORM `Updates` 在 `RowsAffected=0`（版本冲突）时返回 `nil`，导致并发下出价记录已写入但竞拍状态未更新。
- **修复**：增加 `result.RowsAffected == 0` 检查，返回 `"auction version conflict"` 错误，触发事务回滚。`txGormAdminStore` 和 `GormAdminStore` 两个版本均已修复。

---

## ~~[P1] 双写 Redis Stream：直接 Publish + OutboxPoller 重复投递~~ ✅ 已修复

- **文件**：`server/internal/service/public.go:306-317`（原行号）
- **类型**：架构设计问题
- **描述**：Outbox 路径和直接 `s.stream.Publish()` 同时生效，同一条事件被 Consumer 处理两次。
- **修复**：删除直接 Publish 步骤，事件仅通过 MySQL Outbox → OutboxPoller → Stream 单一路径分发。

---

## ~~[P1] 成交结算 goroutine 无错误重试和用户可观察性~~ ✅ 已修复

- **文件**：`server/internal/service/public.go:320-328`（原行号）
- **类型**：逻辑错误 / 可靠性
- **描述**：结算失败只打印日志，无重试。用户看到"已成交"但无订单。
- **修复**：新增 `settleWithRetry` 函数（`public.go:599-615`），带 2 次重试和分级日志。PlaceBid 中 `result.Sold` 时调用 `go settleWithRetry(...)`。

---

## [P2] Redis Lua 频率检查仅依赖异步更新的缓存

- **文件**：`server/internal/service/public.go:506-511`、`server/internal/stream/consumer.go:199-201`
- **类型**：边界遗漏 / 设计权衡
- **描述**：
  Lua 脚本检查频率用的 `auction:{id}:last_bid_ts:{userId}` 键，由 Consumer 异步更新（L199-201）。从 MySQL 事务提交到 Consumer 写入此键之间有延迟（最多 ~200ms）。同一用户的两笔出价如果间隔 < 200ms，可能同时通过 Redis 频率检查。
- **影响面**：短时间窗口内允许客观上超频的出价到达 MySQL。但 MySQL 唯一索引 `(auction_id, idempotency_key)` 可防重，不同幂等键的超频出价仍会通过。如果业务上严格要求频率限制，需在 MySQL 层增加用户级别的出价间隔检查（如在事务内 `SELECT ... FOR UPDATE` 锁住用户最后出价时间）。
- **建议修复**：
  - 如果频率限制是软性的（仅优化体验），当前方案可接受，建议在注释中明确标注为"best-effort"
  - 如果是硬性要求，在 MySQL 事务内增加 `SELECT MAX(server_ts) FROM bids WHERE auction_id=? AND user_id=? FOR UPDATE`，与当前时间比较后再决定是否允许

---

## [P2] 重复键检测使用字符串匹配 MySQL 错误信息

- **文件**：`server/internal/service/public.go:240`
- **类型**：代码健壮性
- **描述**：
  ```go
  if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "UNIQUE") {
  ```
  字符串匹配 MySQL 错误消息在不同 MySQL 版本、不同 driver 或切换数据库时可能失效。
- **影响面**：唯一索引冲突被当作未知错误抛出，导致事务回滚、用户收到 500，而非正确返回幂等重放结果。
- **建议修复**：
  ```go
  import "github.com/go-sql-driver/mysql"
  var mysqlErr *mysql.MySQLError
  if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
  ```
  或使用 GORM 的 `ErrDuplicatedKey`（如果版本支持）。

---

## ~~[P2] Consumer handleBidAccepted 的 sold 状态更新在 Pipeline 之外~~ ✅ 已修复

- **文件**：`server/internal/stream/consumer.go:209-211`（原行号）
- **类型**：可靠性 / 原子性
- **描述**：sold 状态的 `HSet` 在 Pipeline `Exec` 之后独立执行，非原子。
- **修复**：删除独立 HSet，`status` 已在 Pipeline HSET 中正确设置（payload.status 在 MySQL 事务中已设为 `"sold"`），无需重复写入。`consumer.go:207` 注释说明。

---

## ~~[P3] sortRankingItems 使用 O(n²) 冒泡排序~~ ✅ 已修复

- **文件**：`server/internal/service/public.go:357-362` → `public.go:345-347`
- **类型**：性能隐患
- **修复**：改用标准库 `sort.Slice()`，O(n log n)。

---

## [P3] 旧代码注释区含误导信息

- **文件**：`server/internal/service/public.go:403-421`
- **类型**：维护性
- **描述**：
  文件末尾旧代码区的注释说明 `runBidScript` 是老脚本"后续可删"，但测试 `TestBidLuaResultToBidResult` 等仍引用 `bidLuaResult` 和 `toBidResult`。保留区中的 `runBidScript` 已经只是一个 alias 指向 `runBidLiteScript`，不存在实际旧逻辑，但注释造成混淆。
- **建议修复**：清理注释或标注测试依赖的具体结构体，避免误删。

---

## 修复完成状态

| 等级 | 问题 | 状态 |
|------|------|:----:|
| P0 | 事务内读取竞拍使用非事务 Store → 改为 `tx.GetAuction` | ✅ |
| P0 | `UpdateAuctionBidState` 版本冲突静默成功 → 增加 `RowsAffected` 检查 | ✅ |
| P1 | 双写 Redis Stream → 删除直接 Publish 路径 | ✅ |
| P1 | 结算 goroutine 无重试 → `settleWithRetry` 带 2 次重试 | ✅ |
| P2 | Consumer sold 状态在 Pipeline 之外 → 删除独立 HSet，并入 Pipeline | ✅ |
| P3 | `sortRankingItems` O(n²) 冒泡 → 改用 `sort.Slice` | ✅ |
| P2 | Lua 频率检查 best-effort 延迟 | ⚪ 设计决策 |
| P2 | 字符串匹配 MySQL 错误码 | ⚪ 未修复 |
| P3 | 旧代码注释 | ⚪ 低优 |

> 修复时间：2026-06-04。两个 P0 和四个 P1-P3 问题已全部修复，出价核心链路并发安全性和可靠性显著提升。
