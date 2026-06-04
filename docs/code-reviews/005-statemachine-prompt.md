# Review 批次 005-statemachine：竞拍状态机

> 请 review 以下代码文件和测试文件。
> 关注**逻辑完备性、并发安全、边界遗漏**，不要关注代码风格。
> 已知的偏差记录在 `docs/rules/deviations-log.md`，不要重复报告已记录的问题。

## 核心文件
- `server/internal/statemachine/auction.go`

## 测试文件
- `server/internal/statemachine/auction_test.go`

## 审查重点

- 所有合法/非法状态对的定义完整性
- draft → published → running → sold/failed/cancelled 是否有遗漏
- 状态机测试的边界覆盖


## 输出格式

### [P0/P1/P2/P3] 问题标题
- **文件**：`路径:行号`
- **类型**：逻辑错误 / 并发安全 / 边界遗漏 / 性能隐患
- **描述**：
- **影响面**：
- **建议修复**：

严重等级：
- P0：可能导致数据不一致或资金损失
- P1：特定条件触发的逻辑错误
- P2：并发安全或极端边界遗漏
- P3：代码健壮性或维护性问题
