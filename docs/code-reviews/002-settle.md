# 002-settle Review Report（新架构）

> 架构变更（2026-06-04）：
> - MySQL 是唯一写入点、唯一 Truth Source
> - 结算仍为同步调用（未改为 Outbox 模式，settleWithRetry 是 goroutine 重试）

---

## [P0] doExecuteSettle 非事务：Auction 更新与 Order 创建不原子

- **文件**：`server/internal/service/settle.go:81-144`
- **类型**：数据一致性
- **描述**：
  ```go
  // doExecuteSettle 中三次独立 DB 操作，无事务包裹：
  s.adminStore.UpdateAuction(ctx, auction)   // ① 更新竞拍状态（Save，无乐观锁）
  room, roomErr := s.adminStore.GetRoom(ctx, auction.RoomID)  // ② 查 Room 取 SellerID
  s.adminStore.CreateOrder(ctx, order)       // ③ 创建订单
  ```
  如果 ① 成功但 ② 失败 → 竞拍已是 `sold`/`failed`，但订单未创建。
  如果 ①② 成功但 ③ 失败 → 竞拍已是终态，但无对应订单。

  `settleWithRetry` 重试可缓解，但重试之间竞拍处于不一致状态（状态已改、订单不存在）。且重试最多 2 次，2 次后永久孤儿。
- **影响面**：竞拍终态无订单，用户看到成交/流拍但无后续流程可走。
- **建议修复**：
  ```go
  func (s *SettleService) doExecuteSettle(ctx context.Context, auction *model.Auction) (*SettleResult, error) {
      var result *SettleResult
      err := s.adminStore.WithTx(ctx, func(tx repository.AdminStore) error {
          // 判断成交/流拍
          // tx.UpdateAuction(...)
          // room, _ := tx.GetRoom(...)
          // tx.CreateOrder(...)
          return nil
      })
      return result, err
  }
  ```
  注意：`txGormAdminStore.UpdateAuction` 也是 `Save()`（无版本检查），但事务本身保障了原子性。

---

## [P0] UpdateAuction 使用 GORM Save() 无乐观锁，并发竞拍 + 结算可覆盖

- **文件**：`server/internal/repository/admin.go:130`（tx 版）、`admin.go:186`（非 tx 版）
- **类型**：并发安全 / 数据一致性
- **描述**：
  ```go
  func (s *txGormAdminStore) UpdateAuction(ctx context.Context, auction *model.Auction) error {
      return s.db.WithContext(ctx).Save(auction).Error  // ← 按主键 UPSERT，不检查 Version！
  }
  ```
  GORM `Save()` 按主键全量覆盖，不使用 `Version` 做乐观锁。而 `UpdateAuctionBidState` 使用 `WHERE version = ?` 做了乐观锁。

  调用 `UpdateAuction` 的地方：
  - `doExecuteSettle`（settle.go:88/102/115）— 结算时改竞拍状态
  - `transitionAuction`（admin.go:280）— 状态机流转
  - `StartAuction`（admin.go:247）— 启动竞拍

  如果在 `doExecuteSettle` 读取拍卖后、`Save()` 执行前，另一个并发出价（PlaceBid）通过 `UpdateAuctionBidState` 更新了价格和版本，`Save()` 会静默覆盖掉那次出价的更新。
- **影响面**：并发出价的价格被结算清空，或结算设置的状态被另一个结算覆盖。
- **建议修复**：
  `UpdateAuction` 也应使用乐观锁：
  ```go
  result := s.db.WithContext(ctx).
      Model(&model.Auction{}).
      Where("id = ? AND version = ?", auction.ID, auction.Version).
      Updates(map[string]interface{}{
          "status":        auction.Status,
          "cancel_reason": auction.CancelReason,
          "version":       gorm.Expr("version + 1"),
      })
  // 检查 RowsAffected
  ```
  或至少 `doExecuteSettle` 改用 `UpdateAuctionBidState` 风格的条件更新。

---

## [P1] SettleExpiredAuctions 批量结算被单次失败中断

- **文件**：`server/internal/service/settle.go:190-197`
- **类型**：逻辑错误 / 健壮性
- **描述**：
  ```go
  for _, auction := range auctions {
      a := auction
      if _, err := s.doExecuteSettle(ctx, &a); err != nil {
          return count, fmt.Errorf("结算竞拍 %d 失败: %w", auction.ID, err)
          // ← 第一个失败立即 return，后续过期竞拍无人处理
      }
      count++
  }
  ```
  启动时批量结算过期竞拍，如果第 3 个因为 Room 不存在等原因失败，第 4~N 个全部跳过。上次 count 个已结算的成功结果也被丢弃（不返回给调用方）。
- **影响面**：部分过期竞拍永不被结算，积累在 running 状态下不处理。
- **建议修复**：
  ```go
  var errs []error
  for _, auction := range auctions {
      if _, err := s.doExecuteSettle(ctx, &a); err != nil {
          errs = append(errs, fmt.Errorf("竞拍 %d: %w", auction.ID, err))
          continue  // 继续处理后续
      }
      count++
  }
  if len(errs) > 0 {
      return count, fmt.Errorf("部分结算失败: %v", errors.Join(errs...))
  }
  return count, nil
  ```

---

## [P1] transitionAuction 无事务包裹，读-改-写非原子

- **文件**：`server/internal/service/admin.go:267-284`
- **类型**：并发安全 / 数据一致性
- **描述**：
  ```go
  auction, err := s.getAuction(ctx, id)       // ① 读
  next, err := transition(auction.Status, event)  // ② 校验
  auction.Status = string(next)               // ③ 改
  s.store.UpdateAuction(ctx, auction)         // ④ 写（Save，无版本检查）
  ```
  读（①）和写（④）之间有竞态窗口。例如两个并发 CancelAuction：
  - T1 读到 status="draft"，校验通过，写 status="cancelled"
  - T2 读到 status="draft"（T1 还没写完），校验通过，也写 status="cancelled"

  两次 `Save()` 都成功，没有数据损失但存在多余操作。更严重的是：
  - T1 读到 status="scheduled" 执行 PublishAuction → 写 status="running"
  - T2 读到 status="scheduled"（T1 还没写完）→ 也写 status="running"
  - 两次 `StartAuction` 都把 `EndAt` 重置为 `now+duration`，第一个设置的结束时间被覆盖

  虽然管理后台并发操作不太频繁，但逻辑上存在缺陷。
- **影响面**：极端情况下 `StartAuction` 的结束时间被覆盖；`CancelAuction` 的取消原因被覆盖。
- **建议修复**：`transitionAuction` 也使用 `WITH version` 条件更新，或包裹在事务内。

---

## [P2] doExecuteSettle 成交路径的 UpdateAuction 是冗余操作

- **文件**：`server/internal/service/settle.go:113-117`
- **类型**：逻辑冗余
- **描述**：
  ```go
  // 成交
  auction.Status = string(statemachine.StateSold)
  if err := s.adminStore.UpdateAuction(ctx, auction); err != nil {
      return nil, err
  }
  ```
  当 PlaceBid 触发结算时，竞拍已经在 MySQL 事务中被 `UpdateAuctionBidState` 设置为 `sold`（public.go:252 传入 `newStatus="sold"`）。这里的 `UpdateAuction` 是冗余的全量 `Save()`，只重复设置同值 `status="sold"`，但用 `Save()` 会覆盖版本号（`version` 被重置为旧值）。

  更严重的是：`auction.Version` 是 `doExecuteSettle` 参数传入时的值，而不是数据库当前值。`Save()` 按主键匹配，会把这个过期 version 写回。如果 PlaceBid 已经 `version+1`，`doExecuteSettle` 的 `Save()` 会把 version 回退。
- **影响面**：乐观锁 version 被回退，后续 `UpdateAuctionBidState` 的版本检查失效。
- **建议修复**：
  成交路径跳过后，或改为：
  ```go
  // 成交路径无需再设 status，直接跳到创建订单
  // 只在流拍路径需要 UpdateAuction 设置 status="failed"
  ```
  流拍路径（无人出价/保留价未达）确实需要 `UpdateAuction` 来将状态从 `running` 改为 `failed`。

---

## [P2] StartAuction 中 initAuctionCache 失败不影响竞拍状态，但已写入 DB

- **文件**：`server/internal/service/admin.go:247-252`
- **类型**：数据一致性
- **描述**：
  ```go
  if err := s.store.UpdateAuction(ctx, auction); err != nil {
      return nil, err  // DB 更新失败 → 正确回滚
  }
  if err := s.initAuctionCache(ctx, auction); err != nil {
      return nil, err  // DB 已写入 running，但返回错误→调用方认为启动失败
  }
  ```
  `UpdateAuction` 成功后竞拍已持久化为 `running`，但如果 `initAuctionCache`（Redis）失败，函数返回 error，调用方认为启动失败。实际上竞拍已经在 running 状态，Redis 缺少缓存。

  `WaitReplicas` 的使用也有问题（initAuctionCache 中 L326）：之前的架构变更说"不再等待 Redis 副本确认"，但 `initAuctionCache` 仍调用 `s.redis.WaitReplicas`。
- **影响面**：DB 状态和 Redis 缓存不一致；调用方获得错误信号。
- **建议修复**：
  - `initAuctionCache` 失败时仅 log warning，不返回 error
  - 或改为 Outbox 模式：DB 事务中写入 outbox 事件 `auction.started`，Consumer 负责初始化 Redis 缓存
  - 移除 `WaitReplicas` 调用

---

## [P3] PayOrder 使用 UpdateOrder(Save()) 无版本检查

- **文件**：`server/internal/service/settle.go:167`、`server/internal/repository/admin.go:138/249`
- **类型**：并发安全（低风险）
- **描述**：
  `PayOrder` 读取订单后直接用 `UpdateOrder(Save())` 写回。两个并发的 PayOrder 调用可能都读到 `pending_payment`，都做 `Save()`。虽然没有数据损失（都是写 `paid`），但 `PaidAt` 会被覆盖为后一次的时间。
- **影响面**：极低，支付场景并发概率小，且都是最终一致状态。
- **建议修复**：改为条件更新：
  ```go
  s.db.Model(&model.Order{}).
      Where("id = ? AND status = ?", orderID, "pending_payment").
      Updates(map[string]interface{}{"status": "paid", "paid_at": now})
  ```

---

## 总结

| 等级 | 数量 | 关键问题 |
|------|------|----------|
| P0 | 2 | doExecuteSettle 非事务；UpdateAuction Save() 无乐观锁 |
| P1 | 2 | SettleExpiredAuctions 失败中断；transitionAuction 读-改-写非原子 |
| P2 | 2 | 成交路径冗余 UpdateAuction 回退 version；initAuctionCache 失败处理 |
| P3 | 1 | PayOrder Save() 无版本检查 |

**最优先修复**：P0-1 `doExecuteSettle` 用 `WithTx` 包裹；P0-2 `UpdateAuction` 改为条件更新加 `RowsAffected` 检查（与 `UpdateAuctionBidState` 一致）。

---

## 修复完成状态

> 修复时间：2026-06-04

| 等级 | 问题 | 状态 | 变更 |
|------|------|:----:|------|
| P0 | doExecuteSettle 非事务 | ✅ | `settle.go:84` — `WithTx` 包裹，全部操作使用 `tx.*` |
| P0 | UpdateAuction Save() 无乐观锁 | ✅ | `admin.go:131/211` — tx + non-tx 两版均改为 `WHERE version = ?` + `RowsAffected` 检查 |
| P1 | SettleExpiredAuctions 失败中断 | ✅ | `settle.go:197-208` — `errs []error` + `continue`，返回部分成功计数 |
| P1 | transitionAuction 读-改-写非原子 | ✅ | `UpdateAuction` 的乐观锁兜底，版本冲突时正确返回 error |
| P2 | 成交路径冗余 UpdateAuction | ⚪ | 保留但安全（事务内 + version 检查），注释说明幂等设计 |
| P2 | initAuctionCache 失败返回 error | ✅ | `admin.go:251` — 忽略错误返回值 |
| P3 | PayOrder Save() 无版本检查 | ✅ | `settle.go:172` → `UpdateOrderStatus(ctx, id, "paid", &now)` 条件更新 |

> 注意：`initAuctionCache` 失败静默忽略后，Redis 缓存缺失会导致 Lua 预校验返回 `AUCTION_CACHE_MISSING` 拒绝所有出价，需确保 Redis 可用或后续增加补偿初始化机制。
