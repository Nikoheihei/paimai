# Review 批次 003-auth：用户认证

> 请 review 以下代码文件和测试文件。
> 关注**逻辑完备性、并发安全、边界遗漏**，不要关注代码风格。
> 已知的偏差记录在 `docs/rules/deviations-log.md`，不要重复报告已记录的问题。

## 核心文件
- `server/internal/service/auth.go`
- `server/pkg/jwt/jwt.go`
- `server/pkg/middleware/auth.go`

## 测试文件
- `server/internal/service/auth_test.go`

## 审查重点

- JWT 签发与校验逻辑
- 鉴权中间件的白名单路由
- 密码 bcrypt 处理
- 路由注册顺序（已知偏差：gin-middleware-order）
- 注册事务完整性（已知修复：WithTx）

## 上下文参考
- `server/internal/handler/auth.go`


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
