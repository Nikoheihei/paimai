# 系统架构设计

> 本文档记录了拍卖系统的架构设计演进过程、核心决策和实现策略。
> 架构设计遵循 **MySQL 为唯一 Truth Source** 的原则，通过事件总线实现读写分离和最终一致。

---

## 一、架构设计思考过程

### 1.1 初始问题：Redis 和 MySQL 谁是真？

早期架构中，出价流程是：

```
Client → Redis Lua（原子判定 + 写入Redis） → MySQL（持久化）
```

问题在于：**Redis 先写入后，如果 MySQL 写入失败，Redis 和 MySQL 产生永久分叉。**
幂等重放时无条件跳过 MySQL，导致状态永远无法修复。

### 1.2 核心结论：只有一个 Truth Source

经过多轮讨论，确认以下原则：

- **MySQL 是唯一 Truth Source**，所有业务状态以 MySQL 为准
- Redis 是**只读缓存 + 热数据层**，可以从 MySQL 重建
- 所有写入必须经过 MySQL 事务，不存在"先写缓存再写 DB"的路径
- 事件传递可以异步，但事件本身必须由 MySQL 事务保证不丢

### 1.3 最终架构

```
                ┌──────────────┐
                │   Client     │
                └──────┬───────┘
                       ↓
            ┌────────────────────┐
            │  API / Bid Service │
            │  只做参数校验      │
            └────────┬───────────┘
                     ↓
        ┌──────────────────────────────┐
        │         MySQL (OLTP)         │
        │  bids / auctions / outbox    │
        │  ── 唯一写入点              │
        │  ── 唯一 Truth Source       │
        │  ── 幂等由唯一索引保证      │
        └──────────┬───────────────────┘
                   ↓
     ┌──────────────────────────────┐
     │ CDC (Canal / Debezium)      │
     │ binlog capture (未来)        │
     │ 当前: goroutine pool outbox  │
     └──────────┬───────────────────┘
                ↓
        ┌────────────────────┐
        │    事件总线        │
        │  Kafka (未来)      │
        │  Redis Stream (当前)│
        └──────┬─────────────┘
               ↓
   ┌───────────┼──────────────┐
   ↓           ↓              ↓
Command     Query          Data
Services    Services       Warehouse
(CQRS)      (CQRS)         (BI/治理)

   ↓           ↓              ↓
Redis      WebSocket     ClickHouse / Hive
State      Gateway       / S3 Lake
(ZSet)     (Push UI)
```

### 1.4 各层职责

| 层 | 职责 | 说明 |
|---|---|---|
| **Client** | 用户界面 | H5 移动端 / PC 管理后台 |
| **API / Bid Service** | 请求入口，参数校验 | 不再做复杂业务判定，只校验输入合法性 |
| **MySQL (OLTP)** | 唯一写入点 | `bids`、`auctions`、`orders`、`outbox` 四张核心表。所有业务写操作都在同一个 MySQL 事务中完成 |
| **CDC** | 数据变更捕获 | **未来**: Canal / Debezium 监听 binlog。**当前**: goroutine 轮询 `outbox` 表的 `pending` 事件 |
| **事件总线** | 可靠事件分发 | **未来**: Kafka（持久化、可回溯、多消费者组）。**当前**: Redis Stream（内存级、可丢） |
| **Command Services** | 更新 Redis 热数据 | 消费事件 → 更新 `auction:{id}:state` HSET + `auction:{id}:ranking` ZSet |
| **Query Services** | 读接口 | 查询 Redis（优先） → 回退 MySQL |
| **Data Warehouse** | 数据治理 | 消费事件 → 写入数仓，用于分析、BI、审计 |

### 1.5 出价流程详解

```
HTTP POST /api/auctions/:id/bids
  │
  ├─ ① 参数校验（金额格式、幂等键长度等）
  │
  ├─ ② MySQL 事务（唯一写入点）
  │   ├─ INSERT INTO bids (auction_id, user_id, amount_cents, idempotency_key, ...)
  │   ├─ UPDATE auctions SET current_price_cents=xxx, version=version+1
  │   │   WHERE id = xxx AND version = old_version  ← 乐观锁
  │   ├─ 若竞拍结束 → INSERT INTO orders (...)
  │   ├─ INSERT INTO outbox (event_type, payload, status='pending')
  │   │   ← 事件例子：{"type":"bid.accepted","auctionId":1,"userId":3,"amountCents":800}
  │   └─ COMMIT  ← 整个事务一起成功或一起回滚
  │
  ├─ ③ HTTP 202 返回用户（出价已接受）
  │
  └─ ④ 异步（goroutine 轮询 outbox → 事件总线）
      ├─ SELECT * FROM outbox WHERE status='pending' ORDER BY id LIMIT 100
      ├─ XADD auction:events → Redis Stream
      └─ UPDATE outbox SET status='done' WHERE id = xxx
                  │
                  ↓  Redis Stream（三个消费者组）
        ┌─────────┼──────────┐
        ↓         ↓          ↓
       C1        C2         C3
    HSET state  WS Push    Data Warehouse
    ZADD ranking Broadcast  (未来)
```

### 1.6 关键设计决策

| 决策 | 结论 | 理由 |
|---|---|---|
| Truth Source | **MySQL** | Redis 作为热缓存，可以从 MySQL 重建 |
| 写入路径 | **只经过 MySQL 事务** | 不存在"先写缓存再写 DB"的路径 |
| 幂等保证 | **MySQL 唯一索引** | `(auction_id, idempotency_key)` 唯一约束天然防止重复 |
| 排行榜 | **Redis ZSet 做热展示，MySQL 做重建源** | ZSet 只存最终分数，同一用户只保留最高出价 |
| 事件总线 | **Redis Stream（当前）→ Kafka（未来）** | MySQL Outbox 保证事件不丢，事件总线可降级 |
| CDC | **goroutine poll 轮询（当前）→ binlog（未来）** | 业务逻辑不变，只换事件捕获方式 |

---

## 二、两步走策略

### 第一步：当前阶段 — 快速实现、架构不变形

**目标**：今天能够运行、测试、验证的核心链路。

**组件**：
- MySQL（OLTP + Outbox 表）
- goroutine 轮询 Outbox（~100ms 间隔）
- Redis Stream（事件总线）
- 3 个 Consumer Group（Redis 状态 / WS 广播 / Data Warehouse 预留）

**限制**：
- 轮询给 MySQL 增加额外读压力（当前并发量下可忽略）
- Redis Stream 的 `maxLen` 裁剪意味着事件不长期留存
- 如果 MySQL 到 Redis Stream 这段挂了，事件延迟增加（取决于轮询间隔）

### 第二步：未来阶段 — 升级为生产级基础设施

**目标**：低延迟、高吞吐、事件可回溯。

**升级内容**：

| 组件 | 第一步（现在） | 第二步（未来） |
|---|---|---|
| CDC | goroutine 轮询 Outbox | Canal / Debezium 监听 binlog |
| 事件总线 | Redis Stream | Kafka Cluster |
| 数据治理 | 预留 C3 接口 | ClickHouse / Hive / S3 Lake |
| Redis 高可用 | 单机 | 哨兵 / Cluster |

**升级方式**：
- **替换 CDC 层**：去掉 `goroutine poll`，启动 Canal/Debezium connector，写入 Kafka
- **替换事件总线**：Kafka Consumer 接到现有 C1/C2/C3 逻辑（接口抽象一致，实现类替换）
- **下游代码不需要动**——消费者拿到的消息格式不变

### 升级兼容性

```
第一步：                   第二步：
MySQL Outbox              MySQL binlog
     ↓                          ↓
goroutine poll           Canal / Debezium
     ↓                          ↓
Redis Stream ──→ C1/C2/C3     Kafka ──→ C1/C2/C3
                                  ↑
                          (接口兼容，消费者不改代码)
```

---

## 三、当前实现优先级

1. **重构出价核心链路**：MySQL 事务写入 + Outbox → Redis Stream → 多消费者
2. **幂等由 MySQL 唯一索引保证**：去掉 Lua 脚本中的幂等键处理
3. **Redis 状态最终由消费者更新**：去掉 Lua 脚本中修改 state/ZSet 的逻辑
4. **断线重连：从 MySQL 拉取全量状态**（Redis 缓存丢了也能恢复）

---

*本文档与 `docs/implementation-plan.md` 的产线追踪保持同步。*
*架构变更需更新本文档并记录到 `docs/rules/deviations-log.md`。*
