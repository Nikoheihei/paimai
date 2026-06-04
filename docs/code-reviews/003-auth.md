# Review 批次 003-auth：用户认证

> 审查范围：`server/internal/service/auth.go`、`server/pkg/jwt/jwt.go`、`server/pkg/middleware/auth.go`、`server/internal/service/auth_test.go`、`server/internal/handler/auth.go`
> 审查日期：2026-06-04

---

### [P0] AuthRequired 中间件无 token 时不拒绝请求

- **文件**：`server/pkg/middleware/auth.go:31-36`
- **类型**：逻辑错误
- **描述**：当请求未携带 token 时，中间件直接 `c.Next()` 放行，不设置任何身份信息。下游 handler 通过 `c.Get("userId")` 获取到 `nil`，若做 `.(uint64)` 类型断言会 panic（见 `handler/auth.go:53`）。即使不 panic，未认证用户可以操作所有"受保护"的接口（出价、订单、管理等），等同于鉴权完全失效。代码注释标注"开发阶段兼容"，但无任何环境判断或配置开关。
- **影响面**：当前部署即为无鉴权状态，任何人可冒充任何用户操作。
- **建议修复**：移除开发兼容逻辑，无 token 时直接 `response.Error(c, 401, ...)` + `c.Abort()`。如需开发模式，应通过环境变量控制，且默认为严格模式。

---

### [P1] JWT Signing Secret 硬编码

- **文件**：`server/pkg/jwt/jwt.go:11`
- **类型**：逻辑错误
- **描述**：`jwtSecret = []byte("paimai_secret_key_123456")` 硬编码在源码中并提交到版本库。任何能访问代码仓库的人都能伪造任意用户的 JWT token，获取任意权限。代码注释写"生产环境应从配置中读取"但未实现。
- **影响面**：代码泄露即等于全体用户身份泄露。
- **建议修复**：从环境变量或配置文件读取 secret，启动时若未配置则拒绝启动。示例：`jwtSecret = []byte(os.Getenv("JWT_SECRET"))`，为空时 `log.Fatal`。

---

### [P2] Register 用户名查重与创建存在 TOCTOU 竞态

- **文件**：`server/internal/service/auth.go:82-88`
- **类型**：并发安全
- **描述**：注册流程先查询用户名是否存在（第 82 行），再在事务中创建（第 102 行）。两个并发注册相同用户名的请求可同时通过查重检查，先后进入事务。由于事务内 `CreateUserAuth` 受 DB 唯一索引保护，第二个会返回唯一约束冲突错误，但该错误以原始 DB 错误返回给客户端，而非友好的"用户名已存在"提示。
- **影响面**：并发注册场景下用户体验差，且错误码不统一（500 而非 400）。
- **建议修复**：在事务内再次检查用户名唯一性，或将 DB 唯一约束冲突映射为 `ErrInvalidInput: username already exists`。

---

### [P2] me handler 对 userId 断言无保护，可 panic

- **文件**：`server/internal/handler/auth.go:53`
- **类型**：边界遗漏
- **描述**：`userID.(uint64)` 在 `userId` 未设置（nil）时会 panic。虽然正常流程中中间件会设置此值，但与 [P0] 关联——当中间件放行无 token 请求时，`c.Get("userId")` 返回 nil，断言直接 panic 导致 500。
- **影响面**：当前中间件开发模式下，访问 `/api/auth/me` 必定 panic。
- **建议修复**：使用安全断言 `userID, ok := c.Get("userId"); if !ok { response.Error(c, 401, ...); return }`。

---

### [P2] 登录无失败次数限制

- **文件**：`server/internal/service/auth.go:138-155`
- **类型**：边界遗漏
- **描述**：`Login` 对错误密码不做计数和限流，攻击者可无限次尝试暴力破解密码。虽然 bcrypt 计算慢（~100ms/次），但分布式暴力破解仍可在合理时间内破解弱密码。
- **影响面**：账户安全风险。
- **建议修复**：增加基于 IP 或用户名的失败计数，连续失败 N 次后锁定账户或增加延迟。可使用 Redis 计数器实现。

---

### [P3] JWT 无刷新与撤销机制

- **文件**：`server/pkg/jwt/jwt.go:24-41`
- **类型**：代码健壮性
- **描述**：JWT 有效期固定 7 天，无 refresh token 机制，也无 token 撤销（logout）能力。用户修改密码后旧 token 仍然有效，管理员无法强制下线用户。
- **影响面**：7 天内无法使已签发的 token 失效，安全事件响应能力受限。
- **建议修复**：短期可引入 token 黑名单（Redis SET），长期应实现 refresh token 轮换。

---

### [P3] bcrypt 哈希在事务外计算

- **文件**：`server/internal/service/auth.go:90-93`
- **类型**：性能隐患
- **描述**：`bcrypt.GenerateFromPassword`（约 100ms）在事务开启前执行。若事务因用户名重复失败，CPU 开销浪费。虽然不影响正确性，但在高并发注册场景下可优化。
- **影响面**：性能浪费，不影响功能。
- **建议修复**：将哈希计算移入事务内，或在查重失败时提前返回不计算哈希。当前逻辑已是先查重再哈希，性能浪费仅在查重通过但事务提交失败时发生，概率极低，可暂不修改。

---

### 测试覆盖评估

| 审查重点 | 覆盖情况 |
|---|---|
| JWT 签发与校验逻辑 | ❌ 未覆盖（jwt 包无单元测试） |
| 鉴权中间件的白名单路由 | ❌ 未覆盖（middleware 无测试） |
| 密码 bcrypt 处理 | ⚠️ 间接覆盖（登录测试验证密码比对，但未测试 hash 格式） |
| 路由注册顺序（已知偏差 gin-middleware-order） | N/A（本批次不涉及 main.go） |
| 注册事务完整性（WithTx） | ✅ 覆盖（重复用户名测试验证唯一约束） |
