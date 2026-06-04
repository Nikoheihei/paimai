# Review 批次 004-ws-stream：WebSocket & Stream

> 审查范围：`server/internal/websocket/hub.go`、`server/internal/websocket/client.go`、`server/internal/stream/consumer.go`、`server/internal/handler/public.go`
> 审查日期：2026-06-04

---

### [P1] Broadcast 释放 RLock 后遍历 room.clients 存在数据竞态

- **文件**：`server/internal/websocket/hub.go:77-98`
- **类型**：并发安全
- **描述**：`Broadcast` 先用 `RLock` 获取 room 引用，然后立即 `RUnlock`，再遍历 `room.clients`。在释放锁和遍历之间，`Run` goroutine 可通过 `unregister` 修改 `room.clients`（删除客户端），导致：
  1. **concurrent map read/write panic**：遍历期间 map 被修改直接 crash；
  2. **消息丢失**：新注册的客户端不会被本次广播覆盖。
- **影响面**：高并发出价场景下，Hub 的 Broadcast 和 Unregister 竞争可导致服务 panic 崩溃。
- **建议修复**：在 `RLock` 保护下完成整个遍历和发送，或拷贝客户端列表后释放锁再发送：
  ```go
  h.mu.RLock()
  clients := make([]*Client, 0, len(room.clients))
  for c := range room.clients { clients = append(clients, c) }
  h.mu.RUnlock()
  for _, c := range clients { ... }
  ```

---

### [P1] Broadcast 慢客户端断开与 Unregister 可 double close(send)

- **文件**：`server/internal/websocket/hub.go:92-96`、`hub.go:49-55`
- **类型**：并发安全
- **描述**：`Broadcast` 检测到慢客户端时执行 `close(client.send)`（第 94 行），随后客户端的 `ReadPump` 检测到连接断开，将 client 发送到 `unregister` 通道。`Run` 处理 unregister 时再次执行 `close(client.send)`（第 54 行），导致 **panic: close of closed channel**。
- **影响面**：慢客户端场景下 Hub goroutine panic，所有 WebSocket 连接断开。
- **建议修复**：使用 `sync.Once` 保护 `close(client.send)`，或在 close 前将 client 从 room 中移除后设置标记，unregister 时检查标记再决定是否 close。

---

### [P1] serveWS 允许查询参数冒充任意用户

- **文件**：`server/internal/handler/public.go:170-179`
- **类型**：逻辑错误
- **描述**：WebSocket 升级时，若 JWT 中间件未设置 `userId`（开发模式下中间件放行无 token 请求），代码回退到读取 `?userId=X` 查询参数。攻击者可设置任意 `userId` 冒充其他用户接收出价推送、模拟他人身份。
- **影响面**：与 [003-batch P0] 叠加，当前部署中任何人都可通过 `?userId=42` 接收他人的出价实时推送。
- **建议修复**：移除查询参数回退逻辑，强制要求 JWT token 认证。WebSocket 的 token 应通过 `?token=` 传递（中间件已支持），不应允许 `?userId=` 覆盖。

---

### [P2] Consumer 使用固定 consumerName 不支持多实例

- **文件**：`server/internal/stream/consumer.go:23`
- **类型**：边界遗漏
- **描述**：`consumerName = "instance-1"` 硬编码。Redis Stream 消费者组中，同一 consumerName 的多个连接会导致消息在它们之间重新分配，行为不可预测。水平扩展部署多实例时，只有一个实例能收到消息，其余实例静默空转。
- **影响面**：单实例无影响；多实例部署时事件推送只到部分用户。
- **建议修复**：使用主机名 + PID 或 UUID 生成唯一 consumerName：`consumerName = fmt.Sprintf("instance-%d", os.Getpid())`

---

### [P2] Consumer poll 错误静默丢弃

- **文件**：`server/internal/stream/consumer.go:107-117`
- **类型**：边界遗漏
- **描述**：`poll` 中 `XReadGroup` 返回错误时直接 return，不记录日志。Redis 连接断开、超时等故障会导致消费者静默停止处理消息，且无告警。
- **影响面**：Redis 故障期间出价事件无法推送，运维无感知。
- **建议修复**：增加错误日志 `log.Printf("[stream] poll error: %v", err)`，可考虑加退避重试。

---

### [P2] WebSocket CheckOrigin 允许所有来源

- **文件**：`server/internal/handler/public.go:21`
- **类型**：边界遗漏
- **描述**：`CheckOrigin: func(r *http.Request) bool { return true }` 允许任意 origin 的 WebSocket 连接，存在跨站 WebSocket 劫持（CSWSH）风险。恶意网站可在用户不知情时建立 WebSocket 连接，接收出价推送。
- **影响面**：当前无敏感写入操作通过 WS（只推送），但推送数据仍可被窃取。
- **建议修复**：校验 Origin 头是否在允许列表内，开发模式可通过配置开关放行。

---

### [P3] ReadPump 向 unregister 通道写入无背压

- **文件**：`server/internal/websocket/client.go:49`
- **类型**：性能隐患
- **描述**：`ReadPump` 的 defer 中 `c.hub.unregister <- c` 是阻塞写入。如果 Hub 的 Run goroutine 处理慢且 unregister 通道（容量 256）已满，ReadPump goroutine 会阻塞。极端场景下大量客户端同时断开可导致 goroutine 泄漏。
- **影响面**：正常运营下不会触发。
- **建议修复**：使用 `select` 非阻塞写入，丢弃失败时仅记录日志：
  ```go
  select { case c.hub.unregister <- c: default: log.Printf("unregister channel full") }
  ```

---

### [P3] ensureGroup 使用字符串匹配判断重复组

- **文件**：`server/internal/stream/consumer.go:79`
- **类型**：代码健壮性
- **描述**：`err.Error() != "BUSYGROUP Consumer Group name already exists"` 通过错误消息字符串判断是否为"组已存在"。不同 Redis 版本或集群模式可能返回不同消息格式，导致误判。
- **影响面**：Redis 升级或切换集群模式时消费者启动可能失败。
- **建议修复**：使用 `strings.Contains(err.Error(), "BUSYGROUP")` 或检查 Redis 错误前缀。

---

### [P3] serveWS 未校验直播间存在性

- **文件**：`server/internal/handler/public.go:162-167`
- **类型**：边界遗漏
- **描述**：直播间存在校验被注释掉。任何 roomID 都可建立 WebSocket 连接，即使直播间不存在。连接后不会收到任何消息，但浪费服务器资源（空 Room 对象）。
- **影响面**：资源浪费，不影响功能正确性。
- **建议修复**：启用直播间存在校验。

---

### 测试覆盖评估

| 审查重点 | 覆盖情况 |
|---|---|
| Hub 的并发安全（sync.RWMutex） | ❌ 无并发测试 |
| Broadcast 慢客户端处理 | ❌ 未覆盖 |
| readPump / writePump goroutine 退出路径 | ❌ 未覆盖 |
| Stream Consumer 的消息解析健壮性 | ❌ 未覆盖 |
| ack 确认机制 | ❌ 未覆盖 |
| 断线重连的场景覆盖 | ❌ 未覆盖 |
