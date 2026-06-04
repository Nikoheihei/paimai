# Review 批次 001-bid-core：出价核心链路

> 请 review 以下代码文件和测试文件。
> 关注**逻辑完备性、并发安全、边界遗漏**，不要关注代码风格。
> 已知的偏差记录在 `docs/rules/deviations-log.md`，不要重复报告已记录的问题。

## 核心文件
- `server/internal/service/public.go`

## 测试文件
- `server/internal/service/public_test.go`

## 审查重点

- Redis Lua 脚本的原子性（bidScript 字符串）
- validateBidInput 的边界校验
- runBidScript 到 toBidResult 的协议对齐（数组长度 10）
- persistAcceptedBid 中结算调用时序
- 异步 goroutine 处理
- 频率限制逻辑
- 幂等重放逻辑

## 上下文参考
- `server/internal/model/models.go`
- `server/internal/statemachine/auction.go`


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
