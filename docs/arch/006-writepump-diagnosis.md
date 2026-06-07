# WebSocket writePump 诊断手册

## 背景

Hub 锁竞争已消除（Room 独立锁），`broadcast_cost_p99` 已降至 < 10ms。
瓶颈已从"消息进不了房间"转移到"消息进了房间但还没真正发到用户"。
此手册用于判断瓶颈究竟卡在哪个环节。

---

## 核心指标（来自 `/api/ws-stats`）

| 指标 | 来源 | 含义 |
|---|---|---|
| `broadcast_cost_p99` | `hub.go` Broadcast | 从调用 Broadcast 到遍历完 clients 入队的总耗时 |
| `write_cost_p50/p95/p99` | `client.go` writePump | `WriteMessage` 纯系统调用耗时 |
| `write_loop_p50/p95/p99` | `client.go` writePump | 从 select 命中到 WriteMessage 完成的总耗时 |
| `write_pump_msg_count` | `client.go` writePump | 累计从 send channel 取出的消息数 |
| `broadcast_msgs` | `hub.go` Broadcast | 累计成功入队 send channel 的消息数 |
| `send_channel_full` | `hub.go` Broadcast | send channel 满，消息被丢弃的次数 |

---

## 四象限判断法

### 场景 A：broadcast_cost 低，write_cost 高

```
broadcast_cost_p99 ≈ 1ms
write_cost_p99     > 50ms
```

**结论**：真正卡在 WebSocket 写 socket。

**根因**：
- `WriteMessage` 内部调用 `syscall.Write`，受 TCP 拥塞控制、内核缓冲区、网卡队列影响
- 某个客户端网络慢，其 writePump goroutine 阻塞在系统调用
- 但由于每个 client 独立 goroutine，**不会拖慢其他 client**
- 如果大量 client 同时 write_cost 高 → 可能是服务端网卡/内核参数瓶颈

**验证**：
- 看 `ss -tin` 或 `netstat -s` 的 TCP retransmits
- 看 `iftop` 或 `nicstat` 网卡利用率
- 看 `dmesg` 有无 TCP drop

**对策**：
- 调优内核 TCP 参数（`net.core.wmem_max`, `net.ipv4.tcp_wmem`）
- 升级网卡/带宽
- 考虑 Room goroutine 分片（一个 goroutine 负责一批 client 的 write，批量 syscall）

---

### 场景 B：write_cost 低，write_loop 高

```
write_cost_p99  < 1ms
write_loop_p99  > 50ms
```

**结论**：writePump 内部 select/调度/队列处理有延迟。

**根因**：
- `select { case msg := <-c.send: ... }` 虽然命中了，但 goroutine 调度延迟高
- Go runtime 调度器在大量 goroutine 下，单个 goroutine 从 runnable 到 running 的延迟增加
- 或者 `c.conn.SetWriteDeadline` 等非 WriteMessage 操作有隐藏耗时

**验证**：
- 看 `GODEBUG=gctrace=1` 的 GC 停顿
- 看 `runtime.ReadMemStats` 的 GC 频率
- 看 `top` 的 CPU 利用率，是否跑满

**对策**：
- 减少 writePump 数量（Room goroutine 分片，一个 goroutine 写多个 client）
- 调优 GOMAXPROCS
- 检查是否有其他 CPU 密集型任务抢占

---

### 场景 C：write_pump_msg_count 明显小于 broadcast_msgs

```
broadcast_msgs      = 1,000,000
write_pump_msg_count = 500,000
send_channel_full   > 0
```

**结论**：send channel 到 writePump 消费速度跟不上。

**根因**：
- `send` channel 缓冲 256，Broadcast 入队快，writePump 消费慢
- channel 满后走 `default`，消息被丢弃
- 但注意：由于 select 非阻塞，**不会阻塞 Broadcast**

**验证**：
- 直接看 `send_channel_full` 是否持续增长
- 如果 `send_channel_full` 为 0 但计数对不上 → 可能是 client 被标记 Dead，channel 被关闭，消息丢失

**对策**：
- 增大 send channel 缓冲（256 → 1024）
- Room goroutine 分片（减少单个 room 的 client 数量，降低 Broadcast 竞争）
- 或者改为批量发送（一个 writePump 处理多个 client）

---

### 场景 D：write_cost、write_loop 都低，但客户端收到延迟高

```
write_cost_p99  < 1ms
write_loop_p99  < 1ms
broadcast_cost  < 10ms
但压测报告：客户端收到消息延迟 > 1000ms
```

**结论**：瓶颈在压测客户端接收/网络/统计口径。

**根因**：
- 服务端 writePump 很快完成了 `WriteMessage`，但数据还在内核 TCP 发送缓冲区
- 或者压测客户端（Node.js ws 库）接收线程忙不过来
- 或者统计口径问题：客户端记录的是"收到最后一条消息的时间"，而不是"每条消息的延迟"

**验证**：
- 在消息里加服务端时间戳，客户端计算 `client_recv_time - server_send_time`
- 对比 `tcpdump` 抓包的时序
- 单独测试 localhost 绕过网络层

**对策**：
- 优化压测客户端接收逻辑（多线程/多进程消费 ws 消息）
- 检查统计脚本是否准确

---

## 压测执行清单

1. 启动服务端：`cd server && go run main.go`
2. 启动压测：`node scripts/load-test/03-multi-room.mjs`（或单房间压测）
3. 压测过程中多次请求：`curl -s http://localhost:8080/api/ws-stats | jq .`
4. 记录关键指标，对照上表判断
5. 同时监控服务端：`top`, `ss -tin`, `dmesg | tail`

---

## 下一步可能的优化方向（按优先级）

| 优先级 | 方向 | 适用场景 | 复杂度 |
|---|---|---|---|
| P1 | 调优 TCP/内核参数 | 场景 A | 低 |
| P2 | 增大 send channel 缓冲 | 场景 C | 极低 |
| P3 | Room goroutine 分片 | 场景 B/C | 中 |
| P4 | 消息批量发送 | 场景 A/B | 中 |
| P5 | 压测客户端优化 | 场景 D | 低 |
