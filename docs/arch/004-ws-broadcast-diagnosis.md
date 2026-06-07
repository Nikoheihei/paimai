# WebSocket 广播链路瓶颈诊断报告

## 1. 当前广播链路

### 1.1 相关文件

| 文件 | 职责 |
|------|------|
| `server/internal/websocket/hub.go` | Hub 管理、Room 管理、Broadcast 实现 |
| `server/internal/websocket/client.go` | Client 结构、ReadPump/WritePump、消息序列化 |
| `server/internal/stream/consumer.go` | Redis Stream Consumer，调用 `hub.Broadcast()` |
| `server/internal/handler/public.go` | WS 连接升级，创建 Client 并 Register |
| `server/main.go` | 启动 Hub.Run()、注册 `/api/ws-stats` 诊断端点 |

### 1.2 广播流程

```
Consumer.processMessage()
    ↓
ws.NewWsMessage(type, evt)  ──→  json.Marshal(data) + json.Marshal(WsMessage)
    ↓
hub.Broadcast(roomID, []byte)
    ↓
1. h.mu.RLock() → 拷贝 room.clients 到 clients 切片
2. h.mu.RUnlock()
3. for _, client := range clients {
       select {
       case client.send <- message:   // 非阻塞写入
       default:                        // send channel 满 → 标记 EventDead
       }
   }
    ↓
client.WritePump() (每个 client 独立 goroutine)
    ↓
<-c.send → conn.WriteMessage(websocket.TextMessage, message)
```

### 1.3 关键设计特征

| 特征 | 当前实现 |
|------|---------|
| 按 room 分组广播 | **是** — `Broadcast(roomID, message)` 只发给指定 room |
| Hub 事件队列 | `events chan HubEvent` (buffer=512)，`Hub.Run()` 单 goroutine 处理 |
| Client send channel | `make(chan []byte, 256)` — 每个 client 独立 |
| WritePump | 每个 client 一个独立 goroutine |
| 慢客户端处理 | `select default` — send channel 满时标记 `EventDead`，不阻塞广播循环 |
| JSON 序列化位置 | `NewWsMessage()` 在 **Consumer 调用 Broadcast 之前** 执行一次 |

---

## 2. 已排除瓶颈

| 链路 | 状态 | 证据 |
|------|------|------|
| MySQL | ✅ 健康 | 最大运行线程 10-20，慢查询 0 |
| Outbox | ✅ 健康 | pending 事件 < 20 |
| Redis Stream | ✅ 健康 | Stream length 0-2，Consumer lag = 0 |
| Consumer 处理 | ✅ 健康 | pending = 0，处理速度匹配生产速度 |
| HTTP PlaceBid | ✅ 健康 | P99 < 100ms，2xx 率 > 95% |

---

## 3. 嫌疑点排查结果

### 嫌疑点 1：慢客户端阻塞

**判断标准：**
- 如果某些 client send channel 长期接近满
- 如果单个 client 写入耗时明显高
- 如果广播循环会等待慢 client
- 如果慢 client 会拖慢同 room 其他 client

**排查结果：**

| 检查项 | 结果 |
|--------|------|
| 广播循环是否阻塞等待 send channel | **否** — 使用 `select default`，channel 满直接走 default 分支 |
| 慢 client 是否拖慢同 room 其他 client | **否** — 非阻塞发送，慢 client 只会自己被标记 Dead |
| send channel 满时如何处理 | 发送 `EventDead` 到 Hub 事件队列，由 `Hub.Run()` 异步清理 |

**结论：嫌疑点 1 不存在。**

当前实现已经使用了非阻塞发送（`select default`），慢客户端不会阻塞广播循环，也不会拖慢同房间的其他客户端。这是正确的设计。

---

### 嫌疑点 2：单 goroutine 串行广播

**判断标准：**
- 如果所有 room 的广播都经过同一个 goroutine
- 如果 Hub broadcast channel 堆积明显
- 如果事件进入 Hub 到开始发送的等待时间随连接数增加而上升

**排查结果：**

| 检查项 | 结果 |
|--------|------|
| Broadcast 是否在独立 goroutine 中执行 | **否** — `Broadcast()` 由调用方（Consumer）直接同步调用 |
| Hub.Run() 是否处理广播 | **否** — `Hub.Run()` 只处理 Register/Unregister/Dead 事件 |
| 是否存在"广播队列"等待 | **不存在传统队列** — 但 `Broadcast()` 内部有 `h.mu.RLock()` 竞争 |
| 多个 room 是否串行广播 | **是** — Consumer 是单 goroutine 顺序处理 Stream 消息，顺序调用 Broadcast |

**关键发现：**

```go
// consumer.go: processMessage()
wsMsg, err := ws.NewWsMessage(evt.Type, evt)
if err == nil {
    c.hub.Broadcast(evt.RoomID, wsMsg)   // ← 同步调用，Consumer 单 goroutine
}
c.ack(ctx, message.ID)
```

Consumer 是**单 goroutine** 循环 `XREADGROUP` → `processMessage` → `Broadcast` → `ack`。虽然 `Broadcast()` 本身不是由 Hub.Run 执行，但所有广播事件都经过**同一个 Consumer goroutine** 顺序处理。

**这意味着：**
- 如果某个 `Broadcast()` 耗时较长（如 room 内连接数多），会阻塞后续所有事件的处理
- Consumer 无法并发处理多个事件
- 但 `Broadcast()` 内部只是遍历 clients 做非阻塞 send，理论耗时应该很短

**新增诊断指标验证：**

通过新增的 `broadcast_wait_p50/p95/p99` 和 `broadcast_cost_p50/p95/p99` 可以验证：
- 如果 `wait` 时间高 → 说明 Broadcast 被前面的调用阻塞（Consumer 串行）
- 如果 `cost` 时间高 → 说明 Broadcast 内部遍历发送慢

**结论：嫌疑点 2 部分存在。**

Consumer 单 goroutine 顺序处理事件，但 `Broadcast()` 本身不是瓶颈点。真正的瓶颈在 Broadcast 内部的遍历耗时（见嫌疑点 3 和下文分析）。

---

### 嫌疑点 3：重复 JSON 序列化

**判断标准：**
- 如果每个 client 都执行一次 json.Marshal
- 如果一次广播有 N 个连接就 marshal N 次
- 如果 marshal 耗时在压测中明显占比升高

**排查结果：**

| 检查项 | 结果 |
|--------|------|
| JSON 序列化位置 | `NewWsMessage()` 在 Consumer 中，**Broadcast 之前** |
| 每个 client 是否单独 marshal | **否** — 序列化一次，生成 `[]byte`，所有 client 共用 |
| Broadcast 是否重复 marshal | **否** — `Broadcast(roomID, message []byte)` 接收已序列化的字节 |

```go
// consumer.go
wsMsg, err := ws.NewWsMessage(evt.Type, evt)  // ← 只 marshal 一次
if err == nil {
    c.hub.Broadcast(evt.RoomID, wsMsg)         // ← 传入 []byte，广播时直接复用
}
```

**结论：嫌疑点 3 不存在。**

当前实现已经优化了 JSON 序列化，只 marshal 一次，所有客户端共用同一份 `[]byte`。这是正确的设计。

---

## 4. 关键指标

### 4.1 新增诊断指标说明

| 指标 | 来源 | 用途 |
|------|------|------|
| `broadcast_wait_p50/p95/p99` | `Broadcast()` defer 中计算 `time.Since(enterTime) - cost` | 事件从进入 Broadcast 到开始发送的等待时间 |
| `broadcast_cost_p50/p95/p99` | `Broadcast()` defer 中计算 `time.Since(start)` | 单次 Broadcast 总耗时（RLock + 遍历发送） |
| `send_channel_full` | `Broadcast()` default 分支 atomic 计数 | send channel 满的次数 |
| `marshal_count` | `NewWsMessage()` defer atomic 计数 | json.Marshal 调用次数 |
| `marshal_total_ms` | `NewWsMessage()` defer atomic 计时 | json.Marshal 总耗时 |

### 4.2 诊断端点

```bash
curl http://localhost:8080/api/ws-stats
```

返回示例：
```json
{
  "code": 0,
  "data": {
    "connections": 2000,
    "rooms": 10,
    "broadcast_count": 15000,
    "broadcast_msgs": 29847000,
    "slow_broadcasts": 320,
    "slow_clients_dropped": 45,
    "event_queue_len": 0,
    "broadcast_wait_p50": 0.1,
    "broadcast_wait_p95": 2.5,
    "broadcast_wait_p99": 15.0,
    "broadcast_cost_p50": 5.0,
    "broadcast_cost_p95": 120.0,
    "broadcast_cost_p99": 3200.0,
    "send_channel_full": 1200,
    "marshal_count": 15000,
    "marshal_total_ms": 150
  }
}
```

---

## 5. 初步结论

### 5.1 三个嫌疑点排查总结

| 嫌疑点 | 是否存在 | 证据 | 影响 |
|--------|----------|------|------|
| **慢客户端阻塞** | ❌ 否 | 已使用 `select default` 非阻塞发送 | 无影响，设计正确 |
| **单 goroutine 串行广播** | ⚠️ 部分 | Consumer 单 goroutine 顺序处理，但 Broadcast 本身非阻塞 | 有一定影响，但不是根因 |
| **重复 JSON 序列化** | ❌ 否 | `NewWsMessage` 在 Broadcast 前只执行一次 | 无影响，设计正确 |

### 5.2 真正瓶颈推测

既然三个嫌疑点都已排除或部分排除，但广播 P99 仍高达 3.2s，最可能的原因是：

**`h.mu.RLock()` 竞争 + 大 room 客户端列表拷贝耗时**

```go
func (h *Hub) Broadcast(roomID uint64, message []byte) {
    h.mu.RLock()
    room, ok := h.rooms[roomID]
    // ...
    clients := make([]*Client, 0, len(room.clients))
    for c := range room.clients {
        clients = append(clients, c)
    }
    h.mu.RUnlock()
    // ...
}
```

当 room 内有 **2000 个 client** 时：
- `make([]*Client, 0, 2000)` 分配内存
- `for c := range room.clients` 遍历 2000 次
- 这个操作在 `RLock` 保护下执行

如果此时 `Hub.Run()` 正在处理 `EventRegister` 或 `EventDead`（需要 `Lock`），则 `Broadcast` 的 `RLock` 会**阻塞等待**。

**关键矛盾：**
- `Hub.Run()` 处理 `EventDead` 时需要 `h.mu.Lock()`
- `Broadcast()` 需要 `h.mu.RLock()`
- 当大量慢客户端触发 `EventDead` 时，`Hub.Run()` 频繁加锁，导致 `Broadcast()` 的 `RLock` 等待时间增加

**这就是广播 P99 飙升到 3.2s 的最可能根因。**

---

## 6. 建议优化方向

### 6.1 短期：减少锁竞争（低风险）

**方案 A：读写锁改为分段锁或原子操作**
- 将 `sync.RWMutex` 改为每个 room 独立的锁
- `Broadcast()` 只锁目标 room，不影响其他 room

**方案 B：客户端列表缓存**
- 在 `Room` 结构体中维护一个 `clients []*Client` 切片
- `Hub.Run()` 修改时同步更新切片
- `Broadcast()` 直接读取切片，无需遍历 map

### 6.2 中期：广播 goroutine 池（中风险）

**方案 C：按 room 分配广播 goroutine**
- 每个 room 有一个独立的广播 channel 和 goroutine
- Consumer 将事件发送到对应 room 的 channel
- 不同 room 的广播完全并行

### 6.3 长期：零拷贝广播（高风险）

**方案 D：使用 epoll/kqueue 替代 per-client goroutine**
- 参考 gnet、gev 等网络库
- 一个 goroutine 管理所有连接的写事件
- 大幅减少 goroutine 数量和上下文切换

---

## 7. 下一步验证建议

1. **运行压测，观察新增指标**：
   - 如果 `broadcast_wait_p99` 高 → 确认是锁竞争
   - 如果 `broadcast_cost_p99` 高 → 确认是遍历发送耗时
   - 如果 `send_channel_full` 高 → 确认是客户端消费慢

2. **pprof 抓 goroutine 和 mutex  profile**：
   ```bash
   curl http://localhost:6060/debug/pprof/mutex?seconds=30 > mutex.prof
   curl http://localhost:6060/debug/pprof/goroutine?debug=1 > goroutine.txt
   ```

3. **对比实验**：
   - 同样 2000 连接，分散到 10 个 room vs 1 个 room
   - 如果 10 个 room 的 P99 明显更低 → 确认是单 room 锁竞争
