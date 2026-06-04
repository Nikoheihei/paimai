# Review 批次 004-ws-stream：WebSocket & Stream

> 审查范围：`server/internal/websocket/hub.go`、`server/internal/websocket/client.go`、`server/internal/stream/consumer.go`、`server/internal/handler/public.go`
> 审查日期：2026-06-04

---

### [P1] Broadcast 释放 RLock 后遍历 room.clients 存在数据竞态 ✅ 已修复

- **文件**：`server/internal/websocket/hub.go:77-102`
- **状态**：**已修复**（三次审查确认）。`Broadcast` 在 `RLock` 下拷贝 `clients` 切片（第 85-88 行），释放锁后遍历拷贝列表发送，避免了 map 并发遍历 panic。

---

### [P1] Broadcast 慢客户端断开与 Unregister 可 double close(send) ✅ 已修复

- **文件**：`server/internal/websocket/hub.go:54`
- **状态**：**已修复**（三次审查确认）。`unregister` 处理中已改为 `client.closeOnce.Do(func() { close(client.send) })`，`sync.Once` 保护生效，不会再 double close。

---

### [P1] serveWS 允许查询参数冒充任意用户 ✅ 已修复

- **文件**：`server/internal/handler/public.go:170-180`
- **状态**：**已修复**（二次审查确认）。`uid == 0` 时直接 400 拒绝，不再允许通过 `?userId=` 冒充。

---

### [P2] Consumer 使用固定 consumerName 不支持多实例 ✅ 已修复

- **文件**：`server/internal/stream/consumer.go:18`
- **状态**：**已修复**。`consumerName` 已改为 `fmt.Sprintf("instance-%d", os.Getpid())`，多实例部署时各自独立消费。

---

### [P2] Consumer poll 错误静默丢弃 ✅ 已修复

- **文件**：`server/internal/stream/consumer.go:108-111`
- **状态**：**已修复**。`poll` 中已加 `log.Printf("[stream] poll error: %v", err)`，Redis 故障可感知。

---

### [P2] WebSocket CheckOrigin 允许所有来源 ✅ 已修复

- **文件**：`server/internal/handler/public.go:185-191`
- **状态**：**已修复**。已改为校验 `Origin` 头：`origin == "" || strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1")`，不再允许任意来源。

---

### [P3] ReadPump 向 unregister 通道写入无背压 ✅ 已修复

- **文件**：`server/internal/websocket/client.go:51-55`
- **状态**：**已修复**。`ReadPump` defer 中已改为 `select { case c.hub.unregister <- c: default: ... }`，不再阻塞。

---

### [P3] ensureGroup 使用字符串匹配判断重复组 ✅ 已修复

- **文件**：`server/internal/stream/consumer.go:74`
- **状态**：**已修复**。已改为 `strings.Contains(err.Error(), "BUSYGROUP")`，Redis 版本升级不会误判。

---

### [P3] serveWS 未校验直播间存在性 ✅ 已修复

- **文件**：`server/internal/handler/public.go:160-165`
- **状态**：**已修复**。已启用 `h.service.GetRoom` 校验，直播间不存在时拒绝 WS 升级。

---

### 🆕 [P1] outbox at-least-once + consumer 端 EventUUID 去重 ✅ 已修复

- **设计决策**：承认 outbox 轮询是 at-least-once 语义，不在 outbox 层追求 exactly-once。
- **修复方式**：
  1. 每条出价事件在 PlaceBid 中生成 v4 UUID（32 位 hex）
  2. 存入 outbox_events.event_uuid（MySQL unique index）和 Stream payload 的 eventId 字段
  3. Consumer processMessage 先 SETNX event:<uuid> 1 EX 86400，已处理则直接 ack 跳过
  4. OutboxPoller 恢复简单 write-through 逻辑，不再需要两阶段提交
  5. OutboxEvent.StreamID 字段已移除

---

### 🆕 [P1] handleBidAccepted 中 Pipeline Exec 失败时无 XACK，消息会重新投递 ✅ 已修复

- **文件**：`server/internal/stream/consumer.go:208-212`
- **状态**：**已修复**。`pipe.Exec` 失败时已调用 `c.ack(ctx, messageID)`（第 210 行），消息不会重复投递。


---

### 架构决策记录：Hub 事件模型

#### 当前实现（方案一：合并事件队列）

2026-06-04 从 3 channel + select 重构为单 `events chan HubEvent` 方案。

**背景**：
- 原本 `Hub` 有 `register` / `unregister` / `dead` 三个独立 channel
- `Broadcast` 在 RLock 下拷贝 client 列表，遍历时向 `client.send` 发消息
- 慢客户端通过 `dead` channel 通知 `Hub.Run` 清理
- `select` 在多个 ready channel 间均匀伪随机选择，无法保证 `dead` 优先于 `register` 处理

**当前方案（方案一：合并事件队列）**：
```go
type HubEvent struct {
    Type   HubEventType  // EventRegister / EventUnregister / EventDead
    Client *Client
}

type Hub struct {
    mu     sync.RWMutex
    rooms  map[uint64]*Room
    events chan HubEvent  // 单队列，buffer=512
}
```

- `Hub.Run` 通过 `for e := range h.events` 串行消费
- 所有 state mutation（增删 room.clients、close(send)）都在 Run goroutine 中完成
- `Broadcast` 只做两件事：拷贝 client 列表、try-send；慢客户端投递 `EventDead` 到 events 队列
- FIFO 天然无饥饿，代码最简单

**优点**：单 goroutine 无锁（mu 只保护 room 快照拷贝）、可测试、无竞态。

#### 备选方案（当 events 队列成为瓶颈时考虑）

**方案二：拆 goroutine（多消费者）**
```
go handleRegister(events)
go handleUnregister(events)
go handleDead(events)
```
每个 channel 独立 goroutine，Go runtime 并行调度。代价是需要 `sync.Mutex` 保护 `h.rooms` 共享状态，可能引入锁竞争和竞态回归。

**方案三：batch drain（批量排干）**
在单 goroutine 中，每次循环先批量排干 high-priority channel：
```go
for i := 0; i < 50; i++ {
    select {
    case c := <-h.dead:
        cleanup(c)
    default:
        break
    }
}
// 再处理 unregister / register
```
不引入新 goroutine、不加锁，用纯 channel 操作模拟优先级。代价是需要调参（batch size 和 drain 频率）。

---

### 测试覆盖评估

| 审查重点 | 覆盖情况 |
|---|---|
| Hub 并发安全（Register/Unregister/Broadcast） | ✅ `TestConcurrentRegisterUnregister`、`TestBroadcastDuringRegisterUnregister` |
| Broadcast 慢客户端 double close（closeOnce） | ✅ `TestDoubleCloseNoPanic` |
| ReadPump unregister 背压（non-blocking） | ✅ `TestUnregisterChannelNonBlocking` |
| Broadcast 房间不存在 | ✅ `TestBroadcastRoomNotFound` |
| Consumer 幂等去重（SET NX eventUUID） | ✅ `TestEventDedup_SameUUID`、`TestEventDedup_DifferentUUID` |
| Consumer 无效 payload / 未知类型 | ✅ `TestProcessMessageInvalidPayload`、`TestProcessMessageNoPayload`、`TestProcessMessageUnknownType` |
| Consumer 并发处理 | ✅ `TestConcurrentProcessMessage` |
| `extractEventUUID` 解析（含边界） | ✅ `TestExtractEventUUID`（4 子用例） |
| `writePump` ping/pong 路径 | ❌ 无测试（依赖真实 WS 连接，属于集成测试） |

**测试通过率：11/11（100%）**——`websocket` 5 个 + `stream` 6 个全部 PASS。

