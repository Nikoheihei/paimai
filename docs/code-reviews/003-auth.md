# Review 批次 003-auth：用户认证

> 审查范围：`server/internal/service/auth.go`、`server/pkg/jwt/jwt.go`、`server/pkg/middleware/auth.go`、`server/internal/service/auth_test.go`、`server/internal/handler/auth.go`
> 审查日期：2026-06-04

---

### [P0] AuthRequired 中间件无 token 时不拒绝请求 ❌ 未修复

- **文件**：`server/pkg/middleware/auth.go:31-35`
- **类型**：逻辑错误
- **描述**：当请求未携带 token 时，中间件直接 `c.Next()` 放行，不设置任何身份信息。下游 handler 通过 `c.Get("userId")` 获取到 `nil`，若做 `.(uint64)` 类型断言会 panic（见 `handler/auth.go:53`）。即使不 panic，未认证用户可以操作所有"受保护"的接口（出价、订单、管理等），等同于鉴权完全失效。代码注释标注"开发阶段兼容"，但无任何环境判断或配置开关。
- **影响面**：当前部署即为无鉴权状态，任何人可冒充任何用户操作。
- **建议修复**：移除开发兼容逻辑，无 token 时直接 `response.Error(c, 401, ...)` + `c.Abort()`。如需开发模式，应通过环境变量控制，且默认为严格模式。

---

### [P1] JWT Signing Secret 硬编码 ✅ 已修复

- **文件**：`server/pkg/jwt/jwt.go:13-19`
- **状态**：**已修复**（二次审查确认）。已改为从环境变量 `JWT_SECRET` 读取，未配置时 `panic` 拒绝启动。

---

### [P2] Register 用户名查重与创建存在 TOCTOU 竞态 ❌ 未修复

- **文件**：`server/internal/service/auth.go:82-88`
- **类型**：并发安全
- **描述**：注册流程先查询用户名是否存在（第 82 行），再在事务中创建（第 102 行）。两个并发注册相同用户名的请求可同时通过查重检查，先后进入事务。由于事务内 `CreateUserAuth` 受 DB 唯一索引保护，第二个会返回唯一约束冲突错误，但该错误以原始 DB 错误返回给客户端，而非友好的"用户名已存在"提示。
- **影响面**：并发注册场景下用户体验差，且错误码不统一（500 而非 400）。
- **建议修复**：在事务内再次检查用户名唯一性，或将 DB 唯一约束冲突映射为 `ErrInvalidInput: username already exists`。

---

### [P2] me handler 对 userId 断言无保护，可 panic ✅ 已修复

- **文件**：`server/internal/handler/auth.go:54-63`
- **状态**：**已修复**（二次审查确认）。已改为安全断言 `userID, exists := c.Get("userId")` + `uid, ok := userID.(uint64)`，不存在或类型不匹配时返回 401。

---

### [P2] 登录无失败次数限制 ❌ 未修复

- **文件**：`server/internal/service/auth.go:138-155`
- **类型**：边界遗漏
- **描述**：`Login` 对错误密码不做计数和限流，攻击者可无限次尝试暴力破解密码。虽然 bcrypt 计算慢（~100ms/次），但分布式暴力破解仍可在合理时间内破解弱密码。
- **影响面**：账户安全风险。
- **建议修复**：增加基于 IP 或用户名的失败计数，连续失败 N 次后锁定账户或增加延迟。可使用 Redis 计数器实现。

---

### [P3] JWT 无刷新与撤销机制 ❌ 未修复

- **文件**：`server/pkg/jwt/jwt.go:30-48`
- **类型**：代码健壮性
- **描述**：JWT 有效期固定 7 天，无 refresh token 机制，也无 token 撤销（logout）能力。用户修改密码后旧 token 仍然有效，管理员无法强制下线用户。
- **影响面**：7 天内无法使已签发的 token 失效，安全事件响应能力受限。
- **建议修复**：短期可引入 token 黑名单（Redis SET），长期应实现 refresh token 轮换。

---

### [P3] bcrypt 哈希在事务外计算 ❌ 未修复

- **文件**：`server/internal/service/auth.go:90-93`
- **类型**：性能隐患
- **描述**：`bcrypt.GenerateFromPassword`（约 100ms）在事务开启前执行。若事务因用户名重复失败，CPU 开销浪费。虽然不影响正确性，但在高并发注册场景下可优化。
- **影响面**：性能浪费，不影响功能。
- **建议修复**：将哈希计算移入事务内，或在查重失败时提前返回不计算哈希。当前逻辑已是先查重再哈希，性能浪费仅在查重通过但事务提交失败时发生，概率极低，可暂不修改。

---

### 🆕 [P2] Nickname 无长度校验，可传入超长字符串 ✅ 已修复

- **文件**：`server/internal/service/auth.go:209-211`
- **状态**：**已修复**。`validateRegisterInput` 已增加 `if len(input.Nickname) > 64` 校验，超过 64 字符返回 `ErrInvalidInput`。
- **类型**：边界遗漏
- **描述**：当用户传入非空 Nickname 时，代码不做任何长度校验。Username 有 3-32 位的正则校验（第 65 行），但 Nickname 完全无限制。恶意用户可构造数万字符的 Nickname 请求，直接写入数据库，可能导致 DB 写入异常或存储膨胀。
- **影响面**：存储资源可被恶意消耗，可能触发数据库字段长度截断错误。
- **建议修复**：在 `validateRegisterInput` 中增加 Nickname 校验，限制长度 1-64 字符。

---

### 🆕 [P3] `authService.now` 字段定义但从未使用 ✅ 已修复

- **文件**：`server/internal/service/auth.go:124, 162` 及 `server/pkg/jwt/jwt.go:32`
- **状态**：**已修复**。`Register` 和 `Login` 已改为 `jwtpkg.GenerateToken(..., s.now())`，`GenerateToken` 签名改为接收 `now time.Time` 参数，不再内部调用 `time.Now()`。
- **类型**：代码健壮性（死代码）
- **描述**：`AuthService` 结构体定义了 `now func() time.Time` 字段，`NewAuthService` 中初始化为 `time.Now`，但在 `Register`、`Login`、`Me` 三个方法中均未引用。JWT 签发时（`jwt.go:33`）直接调用 `time.Now()` 而非注入的 `s.now()`，该字段完全无作用。
- **影响面**：增加代码理解成本；若未来想做时间相关的测试 mock，本应使用此字段注入固定时间。
- **建议修复**：要么在 JWT 生成及相关逻辑中使用 `s.now()` 替代 `time.Now()`，要么删除此字段和初始化代码。

---

### 🆕 [P3] ParseToken 签名方法校验不够严格 ✅ 已修复

- **文件**：`server/pkg/jwt/jwt.go:52`
- **状态**：**已修复**。已改为 `jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))`，仅接受 HS256 签名的 token。
- **类型**：逻辑错误
- **描述**：`ParseToken` 的 Keyfunc 仅校验方法是否为 `*jwt.SigningMethodHMAC`（任意 HMAC 变体：HS256/HS384/HS512 均可通过），但 `GenerateToken` 固定使用 `jwt.SigningMethodHS256`。若攻击者用同一 secret 签发 HS384 或 HS512 token，解析方不会拒绝。虽然实战利用价值有限（需持有 secret），但违背了「只接受预期算法」的安全原则。
- **影响面**：轻微安全削弱；若未来算法升级，可能与旧 token 产生兼容性隐忧。
- **建议修复**：在 Keyfunc 中增加 `token.Method.Alg() != "HS256"` 的校验，或使用 `jwt.WithValidMethods([]string{"HS256"})` 配合 `jwt.NewParser()`。

---

---
- **补充 2026-06-04**：已补充 jwt 包单元测试（5 个用例）和 middleware 单元测试（4 个用例），全部通过。

### 测试覆盖评估

| 审查重点 | 覆盖情况 |
|---|---|
| JWT 签发与校验逻辑 | ✅ 已覆盖（pkg/jwt 5 个测试用例：签发解析、过期、无效、空、算法校验） |
| 鉴权中间件的白名单路由 | ✅ 已覆盖（pkg/middleware 4 个测试用例：无 token、无效、有效、过期） |
| 密码 bcrypt 处理 | ⚠️ 间接覆盖（登录测试验证密码比对，但未测试 hash 格式） |
| 路由注册顺序（已知偏差 gin-middleware-order） | N/A（本批次不涉及 main.go） |
| 注册事务完整性（WithTx） | ✅ 覆盖（重复用户名测试验证唯一约束） |
| Nickname 长度校验 | ✅ 已实现（validateRegisterInput 中校验 >64） |
| JWT 签名方法严格校验 | ✅ 已实现（NewParser + WithValidMethods） |
| `now` 字段使用 | ✅ 已使用（Register、Login 调用 s.now()） |


服务端启动必须设 JWT_SECRET 环境变量，否则 panic。