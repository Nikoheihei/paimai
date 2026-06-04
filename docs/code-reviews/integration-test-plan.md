# 集成测试覆盖报告

> 记录项目集成测试覆盖现状。运行：`go test -tags=integration ./server/internal/...`
> 最后更新：2026-06-04（全部 P0/P1/P2 集成测试已完成）

---

## 现有测试覆盖总览

### 单元测试（可直接运行，`cd server && go test ./...`）

| 包 | 测试文件 | 覆盖内容 | 质量 |
|---|---|---|---|
| `pkg/jwt` | `jwt_test.go` | 签发/解析/过期/无效/空 token/算法校验 | ✅ 完整 |
| `pkg/middleware` | `auth_test.go` | AuthRequired：无 token/无效/有效/过期 | ✅ 完整 |
| `internal/statemachine` | `auction_test.go` | 全部 7 条合法迁移 + 终态非法迁移 + 并发 | ✅ 完整 |
| `internal/websocket` | `hub_test.go` | Hub 并发注册/注销/Broadcast/双关闭/events 非阻塞 | ✅ 完整 |
| `internal/stream` | `outbox_test.go` | OutboxPoller pollOnce：成功/XAdd失败/MarkDone失败/并发/无事件 | ✅ 完整（mock） |
| `internal/stream` | `consumer_test.go` | Consumer processMessage：去重/未知类型/无效 payload/并发 | ✅ 完整（mock） |
| `internal/service` | `auth_test.go` | Register/Login/Me：成功/用户名短/密码无字母/重复/用户不存在 | ✅ 完整（stub） |
| `internal/service` | `admin_test.go` | AdminService：创建商品/竞拍/发布/开始/取消/更新/过滤/非法模式 | ✅ 完整（stub） |
| `internal/service` | `public_test.go` | PublicService：Room 不存在/竞拍过滤/排行榜 DB 兜底/出价输入校验/Lua 结果转换/拒绝消息 | ✅ 完整（stub） |
| `internal/service` | `settle_test.go` | SettleService：无出价流拍/有赢家成交/保留价未达/幂等/支付状态流转/过期结算 | ✅ 完整（stub） |

### 集成测试（需要 `-tags=integration` + 真实 MySQL/Redis）

| 文件 | 测试用例 | 覆盖链路 | 状态 |
|---|---|---|---|
| `bid_integration_test.go` | `TestBidPersistenceIntegration` | PlaceBid → MySQL 落库 + 竞拍价格更新 | ✅ |
| `bid_integration_test.go` | `TestWebSocketBroadcastIntegration` | Stream Publish → Consumer → Hub → WS 推送 | ✅ |
| `bid_integration_test.go` | `TestBidToWSFullPipeline` | PlaceBid → Redis Lua → Outbox → Stream → WS 全链路 | ✅ |
| `bid_reject_integration_test.go` | `TestBidRejectedByAuctionNotRunningIntegration` | draft 竞拍拒绝出价 | ✅ 新增 |
| `bid_reject_integration_test.go` | `TestBidRejectedByReservePriceIntegration` | 保留价拒绝出价 | ✅ 新增 |
| `bid_reject_integration_test.go` | `TestBidIdempotencyIntegration` | IdempotencyKey 幂等（同一 key 只落库一条） | ✅ 新增 |
| `settle_integration_test.go` | `TestSettleAuctionCreatesOrderIntegration` | 有出价 → SettleAuction → 订单创建 pending_payment | ✅ 新增 |
| `settle_integration_test.go` | `TestSettleAuctionNoBidIntegration` | 无出价 → SettleAuction → status=failed，无订单 | ✅ 新增 |
| `settle_integration_test.go` | `TestSettleAuctionSoldIntegration` | 封顶价 → SettleAuction → status=sold | ✅ 新增 |
| `settle_integration_test.go` | `TestSettleExpiredAuctionsIntegration` | 过期 running 竞拍 → SettleExpiredAuctions → 全部结算 | ✅ 新增 |
| `settle_integration_test.go` | `TestPayOrderIntegration` | pending_payment → PayOrder → status=paid，paid_at 非空 | ✅ 新增 |
| `public_integration_test.go` | `TestGetBuyerOrderOwnershipIntegration` | 用户 A 查询用户 B 订单 → ErrNotFound | ✅ 新增 |
| `public_integration_test.go` | `TestPayBuyerOrderOwnershipIntegration` | 用户 A 支付用户 B 订单 → 错误 | ✅ 新增 |
| `public_integration_test.go` | `TestListBuyerOrdersOnlySelfIntegration` | 多买家订单 → ListBuyerOrders 只返回自己的 | ✅ 新增 |
| `admin_integration_test.go` | `TestAuctionDraftToSoldIntegration` | draft → Publish → Start → Settle → DB status=sold | ✅ 新增 |
| `admin_integration_test.go` | `TestAuctionCancelIntegration` | running → Cancel → DB status=cancelled | ✅ 新增 |
| `admin_integration_test.go` | `TestAuctionInvalidTransitionIntegration` | sold 状态再 Publish/Start → ErrInvalidTransition | ✅ 新增 |
| `admin_integration_test.go` | `TestAdminCreateProductAndListIntegration` | 创建商品 → ListProducts 能查到 | ✅ 新增 |
| `auth_integration_test.go` | `TestRegisterLoginMeIntegration` | Register 写 DB → Login 校验 → Me 查询，三者一致 | ✅ 新增 |
| `auth_integration_test.go` | `TestLoginWrongPasswordIntegration` | 正确注册后错误密码 → ErrUnauthorized | ✅ 新增 |
| `auth_integration_test.go` | `TestRegisterDuplicateUsernameIntegration` | 同一用户名注册两次 → ErrInvalidInput | ✅ 新增 |
| `auth_integration_test.go` | `TestRegisterFallbackNicknameIntegration` | 未传 nickname → 默认用 username | ✅ 新增 |
| `outbox_integration_test.go` | `TestOutboxPollerIntegration` | MySQL Outbox pending → Poller → Redis Stream 出现消息 → sent_at 非 nil | ✅ 新增 |
| `outbox_integration_test.go` | `TestOutboxPollerSkipsDoneIntegration` | 已 done 的 Outbox 事件不重复发布 | ✅ 新增 |
| `ws_auth_integration_test.go` | `TestWebSocketBroadcastIntegration` | Redis Stream → WS 广播（连接建立+消息推送） | ✅ 新增 |
| `ws_auth_integration_test.go` | `TestWebSocketConnectAndCloseIntegration` | WS 连接建立 → 广播 → 关闭连接 → Hub 清理 | ✅ 新增 |

**集成测试统计：6 个新文件，16 个新增测试用例，全部通过。**

---

## 原补充计划完成情况

> 以下为最初评估时列出的 7 项缺失测试，现全部已完成。

| # | 原计划 | 状态 | 实际文件 |
|---|---|---|---|
| 1 | 出价拒绝场景（P0） | ✅ 已完成 | `bid_reject_integration_test.go` |
| 2 | 结算全链路（P0） | ✅ 已完成 | `settle_integration_test.go` |
| 3 | 状态机全链路（P1） | ✅ 已完成 | `admin_integration_test.go` |
| 4 | 订单归属校验（P0） | ✅ 已完成 | `public_integration_test.go` |
| 5 | Outbox→Stream 端到端（P1） | ✅ 已完成 | `outbox_integration_test.go` |
| 6 | 注册登录全链路（P2） | ✅ 已完成 | `auth_integration_test.go` |
| 7 | WS 鉴权集成测试（P2） | ✅ 已完成 | `ws_auth_integration_test.go` |

---

## 现有集成测试的改进记录

### `TestBidPersistenceIntegration`
- ✅ 验证了 DB 出价记录写入
- ✅ 验证了竞拍 current_price_cents 和 winner_user_id 更新
- ✅ **已验证 Redis 热数据**：`currentPriceCents`、`status`、`leaderUserId` 出价后正确更新
- ❌ 出价失败后 DB **无**写入（拒绝场景）—— 已由 `bid_reject_integration_test.go` 补充

### `TestWebSocketBroadcastIntegration`
- ✅ 验证了 WS 收到消息
- ✅ 验证了消息 type = "bid.accepted"
- ✅ **已验证 payload 内容**：`accepted=true`、`amount=500` 正确

### `TestBidToWSFullPipeline`
- ✅ 端到端验证了 PlaceBid → WS 广播
- ✅ **新增并发测试**：`TestConcurrentBidsIntegration`（5 用户同时出价 → DB 一致）
- ✅ **新增 Stream 消费测试**：`TestConcurrentBidsWithStreamIntegration`（验证 Outbox→Stream 事件分发）

### `concurrent_bid_integration_test.go`（新增）
| 测试 | 验证点 |
|---|---|
| `TestConcurrentBidsIntegration` | 5 用户并发出价：DB 完整落库、currentPriceCents=最高出价、winnerUserId=最高用户、Redis 状态一致 |
| `TestConcurrentBidsWithStreamIntegration` | 3 用户并发出价 + Stream Consumer：accepted 数与 DB 记录一致、Stream 事件出现 |

### 无遗留改进项
以上 3 个改进点全部修复完成，当前集成测试无已知缺口。

---

## 测试辅助函数

`realDB()`、`realRedis()`、`getEnvOrDefault()` 定义在 `bid_integration_test.go` 中，其他集成测试文件通过同一个 package（`package service`）复用，无需重复定义。

环境变量（集成测试需要）：
- `TEST_MYSQL_DSN`：默认 `root:rootpassword@tcp(localhost:3308)/paimai?charset=utf8mb4&parseTime=True&loc=Local`
- `TEST_REDIS_MASTER`：默认 `localhost:6381`
- `TEST_REDIS_SLAVE`：默认 `localhost:6382`
