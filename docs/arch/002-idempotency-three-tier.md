# 幂等 + 频率三层架构——设计心路历程

> 从"出价 API 简单校验→写入"到"三层幂等架构"的完整推导，
> 以及人工对每个架构决策的修正记录。

---

## 起点：为什么觉得不对

最早的出价流程是：

```
客户端 → Lua（只读预校验） → MySQL 事务（唯一索引做幂等）
```

Lua 只做快速拒绝，把真正的写入和判定全扔给 MySQL。合理但不够——出价 API 的本质不是"先校验再写入"，而是**"先占位再落库"**。

---

## 第一代：三层雏形（Lua 内做所有事）

```
Lua:
  ① 查 idemKey 缓存（命中→直接返回序列化结果）
  ② SETNX inflightKey（冲突→返回 IN_FLIGHT）
  ③ 频率检查（lastBidTsKey）
  ④ 金额/状态校验

MySQL 事务:
  写入 bids + auction 更新 + outbox 事件

事务后:
  SET idemKey（序列化完整响应） + DEL inflightKey
```

**问题**：Lua 里同时做了读（缓存检查、校验）和写（SETNX inflightKey），职责边界模糊。

---

## 🔧 人工决策 #1：幂等缓存只存标志位，不存序列化结果

**触发点**：讨论"idem:key 应该存什么"

> 工业级做法：存 `{"bidId":123, "amount":800, "accepted":true}`，重放时原样返回第一次结果。

**人工决策**：**不序列化，只存标志位**。

**理由**（来自你）：

> 直播拍卖有排行榜。如果用户已经知道自己领先就不会出价，如果用户不是领先，出价也不会被幂等拦截（因为用的是新的 idempotencyKey）。所以重放时不需要返回原始金额——客户端自己知道状态。

**澄清过程**：

| 假设场景 | 是否会被拦截 |
|----------|-------------|
| 用户1 第一次出价（key=A） | 正常通过 |
| 用户2 出价（key=B） | 正常通过（不同 key） |
| 用户1 第三次出价（key=C） | **不会被拦截**——是不同的 key |

→ 所以缓存只需要 tell 调用方"这个 key 已经处理过了"，不需要告诉它处理结果是什么。

**落地**：`idem:<auctionId>:<md5(key)> = "1"`，TTL 24h。

---

## 🔧 人工决策 #2：inflight 锁从 Lua 迁移到 Go 层

**触发点**：审视三层职责。

**原方案**：Lua 里做 `SETNX inflightKey`，成功后继续校验。

**人工决策**：**Lua 不做任何写操作，inflight 锁交由 Go 层在 Lua 通过后执行。**

**理由**：

> Lua 应该保持纯只读预校验——快速拒绝非法请求。inflight 锁是"占位"行为，不是校验行为，应该放在 Go 层紧邻 MySQL 事务的入口处。

**好处**：
- Lua 回归纯函数语义：输入→校验→通过/拒绝，无副作用
- inflight 锁的生命周期和 MySQL 事务紧耦合，Go 层控制更精准
- 如果未来换 Lua 脚本逻辑，inflight 锁不受影响

**落地后的流程**：

```
Lua（只读）：
  ① EXISTS idemKey → 命中直接返回
  ② 频率检查（lastBidTsKey）
  ③ 金额/状态校验

Go 层：
  SETNX inflightKey EX 30 → 失败返回 IN_FLIGHT

MySQL 事务：
  唯一索引保底

事务后（goroutine / 事务内）：
  pipeline: SET idemKey + DEL inflightKey + ZADD ranking
```

---

## 🔧 人工决策 #3：ZADD 替代 ZINCRBY（排行榜幂等写）

**触发点**：HTTP goroutine 和 Stream Consumer 可能同时写排行榜 ZSET。

**问题**：如果两边都用 `ZINCRBY`，同一个用户的出价会被累加两次。

**人工决策**：**用 ZADD（设 Score=当前价）而不是 ZINCRBY（增量加）。**

**理由**：ZADD 是幂等操作——同一个 member 设同一个 score，写多少次结果都一样。ZINCRBY 是增量操作，多写一次就多增一次。

**约束**：WS 广播和 ZSET 写入不得相互依赖。Consumer 的 ZADD 是"校准式写入"（full snapshot），不是增量更新。

---

## 🔧 人工决策 #4：inflightKey 泄漏修复

**触发点**：你问"inflight 锁到底是个什么样子的状态"，我画完整链路后发现一个 gap。

**问题**：MySQL 事务内有 3 个路径会拒绝出价但不释放 inflightKey：

| 拒绝场景 | inflightKey 状态 |
|----------|-----------------|
| `auction.Status != "running"` | ❌ 泄漏（30s TTL） |
| `now.After(auction.EndAt)` | ❌ 泄漏 |
| `金额 < 当前价 + 加价幅度` | ❌ 泄漏 |
| 唯一索引冲突（幂等重放） | ✅ 正常清理 |
| 出价成功 | ✅ 正常清理 |

**后果**：用户用一个合法但金额不够的 key 出价被拒后，30s 内用同一个 key 重试会被 IN_FLIGHT 挡住——哪怕上一次是业务拒绝而非并发冲突。

**人工决策**：**在 `!result.Accepted` 返回前统一 DEL inflightKey。**

**落地**：

```go
if !result.Accepted {
    // 事务内 reject → 清理 inflightKey，避免阻塞后续重试
    go func() {
        s.redis.Master.Del(ctx, inflightKey)
    }()
    return result, err
}
```

---

## 最终形态

```
                请求进来（相同 idempotencyKey）
                          │
                          ▼
                ┌─────────────────┐
                │ ① Lua（只读）    │
                │  · EXISTS idemKey│  ← flag only，不序列化
                │  · 频率检查      │
                │  · 金额/状态校验  │
                └────────┬────────┘
                         │ accepted=true
                         ▼
                ┌─────────────────┐
                │ ② Go SETNX      │  ← inflight 锁（30s TTL）
                │   失败→IN_FLIGHT │
                └────────┬────────┘
                         │ 成功
                         ▼
                ┌─────────────────┐
                │ ③ MySQL 事务    │  ← 唯一真相
                │  · 唯一索引保底  │
                └────────┬────────┘
                         │
            ┌────────────┼────────────┐
            ▼            ▼            ▼
       出价成功      幂等重放      业务拒绝
            │            │            │
            ▼            ▼            ▼
       goroutine   事务内同步    goroutine
       pipeline:   pipeline:    DEL inflight
       SET idemKey SET idemKey  ✅ (刚修)
       DEL inflight DEL inflight
       ZADD rank   ZADD rank
```

### 四条核心原则

1. **MySQL 是唯一真相，Redis 只是缓存的真相。**
2. **Redis 每一次写入都必须在 MySQL 成功后。**
3. **Lua 只读，不写。** 所有写操作在 Go 层控制。
4. **inflightKey 的生命周期必须闭环。** 无论出价成功、幂等重放还是业务拒绝，都要释放。

### 两个兜底

| 场景 | 兜底 |
|------|------|
| Go 在 SETNX 后、DEL inflight 前崩溃 | inflightKey 30s TTL 自然过期 |
| Redis 全量数据丢失 | MySQL 唯一索引保证业务正确 |

---

**核心认知转变：**

出价 API 不是"先校验再写入"的逻辑，而是"先占位再落库"的逻辑。
出价的本质是一个锁竞争行为——
**Lua 是门禁，SETNX 是取号机，MySQL 是柜台，Redis 是回执。**
