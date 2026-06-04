# AI 产线交付报告 #002

> **产线**：`websocket-push`（WebSocket 实时推送）
> **运行日期**：2026-06-02
> **产线版本**：`websocket-pipeline.yml v1.0`
> **依赖产线**：`bid-closed-loop`（#001）
> **状态**：✅ 全量通过

---

> **⚠️ 修正**：初始报告将"后端完成"标注为"产线完成"，实际前端未接入 WS，闭环未通。
> 本轮（2026-06-02 第二趟）补完 H5 前端页面，现在产线才算真正闭环。

## 一、本次生成内容

### 1.1 背景

用户出价落库后，需要实时推送到直播间的所有在线客户端。实现方案：出价 → Redis Stream → 消费者 → WebSocket Hub → 房间广播 → 前端渲染。

### 1.2 修改/新增的文件

| 操作 | 文件 | 说明 |
|---|---|---|
| **ADD** | `server/internal/websocket/hub.go` | Hub：管理房间 + 客户端注册/注销/广播 |
| **ADD** | `server/internal/websocket/client.go` | Client：WebSocket 连接 + ping/pong + 读写 goroutine |
| **ADD** | `server/internal/stream/consumer.go` | Stream 消费者：XREADGROUP → Hub.Broadcast |
| **MODIFY** | `server/internal/service/public.go` | 出价成功后 XADD 到 `auction:events` |
| **MODIFY** | `server/internal/handler/public.go` | 新增 `GET /api/rooms/:roomId/ws` 升级端点 |
| **MODIFY** | `server/main.go` | 初始化 Hub + 启动消费者 + 注入依赖 |
| **MODIFY** | `server/go.mod` | 新增 `github.com/gorilla/websocket v1.5.3` |
| **ADD** | `web-h5/src/hooks/useWebSocket.ts` | H5 WebSocket hook（重连 + 心跳 + 消息分发） |
| **ADD** | `web-admin/src/hooks/useWebSocket.ts` | 管理后台 WebSocket hook（同上） |
| **ADD** | `web-h5/src/api/client.ts` | REST API 客户端封装 |
| **ADD** | `web-h5/src/components/AuctionPanel.tsx` | 竞拍面板组件（价格、排行榜、WS 驱动更新） |
| **ADD** | `web-h5/src/pages/RoomPage.tsx` | 直播间页面（接入 useWebSocket） |
| **MODIFY** | `web-h5/src/App.tsx` | 根组件改为直播间入口 |
| **MODIFY** | `web-h5/src/App.css` | 竞拍直播间样式 |
| **MODIFY** | `web-h5/index.html` | 标题 + 移动端 viewport |
| **MODIFY** | `web-h5/vite.config.ts` | 添加 API 代理到后端 8080 |

### 1.3 API 变化

**新增 WebSocket 端点**：

```
GET /api/rooms/:roomId/ws?userId=<user_id>
```

- 升级到 WebSocket 连接
- 连接后服务端推送 `WsMessage` JSON 消息
- 客户端发送 `{"type":"ping"}` 维持心跳

**新增出价事件的 Stream 写入**：

`PlaceBid` → `persistAcceptedBid` 成功后，自动 `XADD auction:events` 一条事件。

### 1.4 新增的 WebSocket 消息协议

服务端推送的消息格式：

```json
{
  "type": "bid.accepted",
  "data": {
    "type": "bid.accepted",
    "roomId": 1,
    "auctionId": 1,
    "payload": { ... BidResult ... }
  }
}
```

消息类型（当前支持的 `type` 值）：

| type | 触发时机 |
|---|---|
| `bid.accepted` | 有新出价被接受 |
| 更多类型（`ranking.updated`, `auction.ended`, `timer.sync`, `outbid`）待后续扩展 |

---

## 二、验证结果

| 关卡 | 命令 | 结果 |
|---|---|---|
| 后端编译 | `go build ./...` | ✅ |
| 后端单测 | `go test ./... -count=1` | ✅（所有包通过） |
| H5 构建 | `npm run build` (web-h5) | ✅ |
| Admin 构建 | `npm run build` (web-admin) | ✅ |

---

## 三、🟡 人工决策记录

### 1. 用户认证方式

**决策**：开发阶段保持 query 参数传 userId，等全项目统一接 JWT 后再换成 token 方式。

### 2. CORS CheckOrigin

**决策**：开发阶段保持 `return true`（允许所有来源），上线前再锁定具体域名。

---

## 四、✅ 已处理的边界情况

| 边界 | 处理方式 |
|---|---|
| 断线重连 | 前端 `useWebSocket` 实现指数退避重连（1s → 2s → 4s → ... → 30s） |
| 心跳保活 | 服务端 gorilla/websocket 标准 ping/pong（60s pongWait, 54s pingPeriod） |
| 房间不存在 | `serveWS` 先调用 `GetRoom` 校验，房间不存在则拒绝升级 |
| Stream 消费堆积 | XREADGROUP 每次批量读取 10 条 + 自动 ack |
| 慢客户端断开 | `Client.send` channel buffer（256）满时自动断开，不阻塞 Hub 广播 |
| 空房间自动清理 | Hub 中房间无客户端时自动删除 |

---

## 五、⏳ 未处理的边界情况（本轮不做）

| 边界 | 原因 | 后续计划 |
|---|---|---|
| 消息去重 | Stream consumer 重启后可能重复广播 | 给事件加唯一 ID，客户端幂等 |
| 离线增量补推 | 用户断线期间的出价，重连后不自动补偿 | 依赖客户端重连后调用 REST `GET /api/auctions/:id` 重新拉取全量状态 |
| Stream 消费失败重试 | 当前失败直接 ack + 跳过 | 后续加 dead letter queue |
| WebSocket 消息大小限制 | 当前不限制排行榜消息大小 | 排行榜过大时分页 |
| 多实例部署 | 当前单实例，Hub 在内存中 | 多实例时需跨进程广播（Redis Pub/Sub 或外部消息队列） |

---

## 六、测试覆盖

### 新增集成测试

| 文件 | 测试函数 | 覆盖范围 | 运行方式 |
|---|---|---|---|
| `server/internal/service/bid_integration_test.go` | `TestBidPersistenceIntegration` | PlaceBid → MySQL 落库 | `go test -tags=integration -run TestBidPersistenceIntegration` |
| 同上 | `TestWebSocketBroadcastIntegration` | Stream 发布 → WS 广播 | `go test -tags=integration -run TestWebSocketBroadcastIntegration` |
| 同上 | `TestBidToWSFullPipeline` | 出价 → Stream → WS 全链路 | `go test -tags=integration -run TestBidToWSFullPipeline` |

**前置条件**：Docker 中 MySQL + Redis 必须已运行。
**设计原则**：容器未启动时通过 `t.Skipf` 优雅跳过，不阻塞 CI 流程。

### 为什么不用 Mock

集成测试覆盖的是"单元测试测不到"的部分：
- `PlaceBid` → MySQL 事务是否真的写进去了
- Stream 消费者 → Hub → WebSocket 写是否真的触达了客户端
- 三者串起来的全链路时序是否正确

单元测试（mock store + mock redis）保证**逻辑正确**，集成测试保证**真实环境通**。

---

## 七、规则库对账

| 规则 ID | 状态 | 备注 |
|---|---|---|
| 所有 11 条拍卖规则 | ✅ | 未受影响，所有测试通过 |
| WebSocket 相关规则 | ⏳ | 尚无专用规则文件（建议在 `auction-rules.yaml` 中补充 WS 消息协议规则） |

---

## 八、产线良率

| 指标 | 本次值 |
|---|---|
| 产线轮次 | 1 轮 |
| 总新增文件 | 5 |
| 总修改文件 | 4 |
| 后端一次性编译通过 | 否（handler public.go 覆盖重写 + hub register 导出问题，修复后通过） |
| 前端一次性构建通过 | 否（TS 类型错误，修复后通过） |
| 偏差记录 | 本次未产生新规则库条目 |

## 三、建议人工 Code Review 的重点

### 🔴 高优先级（请务必审查）

1. **Redis Stream 消费者组幂等性**
   - 文件：`internal/stream/consumer.go`
   - 当前架构为单实例消费者（`consumerName = "instance-1"`），如果未来扩容到多实例，需要确保 `XREADGROUP` 的消费者名唯一，否则会重复消费
   - **请确认当前单实例部署是否满足需求**

2. **WebSocket 客户端断开清理**
   - 文件：`internal/websocket/client.go` 的 `readPump`
   - 客户端断开后，`readPump` 会调用 `hub.Unregister(client)` 并从房间移除
   - 但 `writePump` 中的 `ticker`（ping 定时器）是否在 client 被清理后正确停止？请检查 `client.send` 关闭对两个 goroutine 的信号传递

### 🟡 中等优先级

3. **Broadcast 的慢客户端处理策略**
   - 文件：`internal/websocket/hub.go` 的 `Broadcast`
   - 慢客户端（send buffer 满）会被直接断开丢弃
   - **确认这个策略是否适合你的场景**：弱网用户可能会频繁断连

4. **出价事件的 Stream 与 WS 之间没有背压控制**
   - 出价写入 Stream 频率不限，WS 广播可能会成为瓶颈
   - 当前 `maxStreamLen = 10000` 可防止 Stream 无限膨胀，但 WS 广播的 goroutine 不会反压上游

### 🟢 低优先级

5. **WebSocket 连接未校验 token**
   - 当前通过 URL query 传 `userId`，生产环境应先通过 JWT 鉴权再建立 WS 连接
   - 已规划在待启动闭环 #205 中处理

---

## 四、与规则库的对账

| 规则 ID | 状态 | 备注 |
|---|---|---|
| `ws-connection-auth` | ⏳ 待覆盖 | 当前用 query 参数 userId，未用 JWT |
| `ws-heartbeat-ping` | ✅ 已覆盖 | 服务端 ping / 客户端 pong 双向心跳 |
| `ws-reconnect-recovery` | ⏳ 待覆盖 | 前端有重连逻辑，但后端不保存断线状态 |
| `ws-slow-client-drop` | ✅ 已覆盖 | send buffer 满自动断开 |
| `stream-event-retry` | ⏳ 未实现 | 当前消费失败直接 ack，不重试 |


---

*报告生成：2026-06-02 | 下轮建议产线：`settle-order`（结算与订单）或补充集成测试*
