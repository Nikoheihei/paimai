# Review 批次 005-statemachine：竞拍状态机

> 审查范围：`server/internal/statemachine/auction.go`、`server/internal/statemachine/auction_test.go`
> 审查日期：2026-06-04

---

### [P2] 终态（sold/failed/cancelled）无外发事件定义，但 TransitionTable 缺少显式空表

- **文件**：`server/internal/statemachine/auction.go:36-50`
- **类型**：边界遗漏
- **描述**：`TransitionTable` 中 `StateDraft`、`StateScheduled`、`StateRunning` 各自定义了允许的事件映射，但终态 `StateSold`、`StateFailed`、`StateCancelled` 完全没有条目。当前行为正确——`Transition` 查不到对应 state 时返回 `ErrInvalidTransition`。但隐式依赖"不存在 = 不允许"缺乏自文档性，且若有人误添加终态的出站迁移（如 sold → running），无任何代码防御。
- **影响面**：当前无功能影响，但状态机完整性依赖隐式约定。
- **建议修复**：为终态添加显式空映射，增强可读性和防御性：
  ```go
  StateSold:      {},
  StateFailed:    {},
  StateCancelled: {},
  ```

---

### [P2] 测试未覆盖终态的所有非法迁移

- **文件**：`server/internal/statemachine/auction_test.go`
- **类型**：边界遗漏
- **描述**：测试只验证了 `Sold → Start`、`Cancelled → Publish` 两种非法迁移。缺少以下场景：
  - `Sold → SettleSold`（重复结算）
  - `Failed → Start`（重新开始）
  - `Cancelled → Cancel`（重复取消）
  - `Running → Publish`（重复发布）
  
  虽然状态机实现正确会拒绝这些迁移，但测试覆盖不够全面。
- **影响面**：测试信心不足，无法防止未来修改引入回归。
- **建议修复**：增加终态对每个事件的非法迁移测试，确保所有终态无出站事件。

---

### [P3] Machine 非线程安全，但当前用法安全

- **文件**：`server/internal/statemachine/auction.go:53-55`
- **类型**：代码健壮性
- **描述**：`Machine` 的 `currentState` 字段无同步保护。若同一 Machine 实例被多个 goroutine 共享调用 `Transition`，存在数据竞态。当前使用模式是每次调用 `transition()` 时新建 Machine，不存在共享问题，但 Machine 类型本身无文档约束此用法。
- **影响面**：当前无影响，但若未来缓存 Machine 实例会引入竞态。
- **建议修复**：在 Machine 文档注释中明确"不可并发使用，应每次创建新实例"，或为 `Transition` 加锁。

---

### [P3] 缺少 draft → running 快捷迁移

- **文件**：`server/internal/statemachine/auction.go:36-50`
- **类型**：边界遗漏
- **描述**：当前状态机要求 `draft → scheduled → running` 两步启动。若业务需要"草稿直接开拍"（跳过发布阶段），需修改状态机。这不是 bug，而是设计约束。当前 AdminService 严格遵循两步流程，Lua 脚本只接受 `running` 状态出价，约束一致。
- **影响面**：无功能影响，仅灵活性受限。
- **建议修复**：无需修改，记录为设计决策。

---

### 状态机完整性审计

| 当前状态 | 可触发事件 | 目标状态 | 测试覆盖 |
|---|---|---|---|
| draft | publish | scheduled | ✅ |
| draft | cancel | cancelled | ✅ |
| scheduled | start | running | ✅ |
| scheduled | cancel | cancelled | ✅ |
| running | settle_sold | sold | ✅ |
| running | settle_failed | failed | ✅ |
| running | cancel | cancelled | ✅ |
| sold | *(none)* | — | ⚠️ 仅测1个非法迁移 |
| failed | *(none)* | — | ❌ 未测非法迁移 |
| cancelled | *(none)* | — | ⚠️ 仅测1个非法迁移 |

状态机定义完整，无遗漏状态或迁移。终态无出站事件符合业务语义。
