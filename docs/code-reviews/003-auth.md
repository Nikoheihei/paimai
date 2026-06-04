# Review 批次 003-auth：用户认证

> 审查范围：`server/internal/service/auth.go`、`server/pkg/jwt/jwt.go`、`server/pkg/middleware/auth.go`、`server/internal/handler/auth.go`
> 审查日期：2026-06-04（初版）、2026-06-04（复查）

---

### [P0] AuthRequired 中间件无 token 时不拒绝请求 ✅ 已修复

- **文件**：`server/pkg/middleware/auth.go:27-33`
- **状态**：**已修复**。无 token 时直接 `c.AbortWithStatusJSON(401, ...)` + `return`，不再放行：
  ```go
  if tokenString == "" {
      c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
          "code":    401,
          "message": "authorization token is required",
      })
      return
  }
  ```

---

### [P1] JWT Signing Secret 硬编码 ✅ 已修复

- **文件**：`server/pkg/jwt/jwt.go:13-19`
- **状态**：**已修复**。已改为从环境变量 `JWT_SECRET` 读取，未配置时 `panic` 拒绝启动。

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
- **状态**：**已修复**。已改为安全断言 `userID, exists := c.Get("userId")` + `uid, ok := userID.(uint64)`，不存在或类型不匹配时返回 401。

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

---

### 🆕 [P3] `authService.now` 字段定义但从未使用 ✅ 已修复

- **文件**：`server/internal/service/auth.go:124, 162` 及 `server/pkg/jwt/jwt.go:32`
- **状态**：**已修复**。`Register` 和 `Login` 已改为 `jwtpkg.GenerateToken(..., s.now())`，`GenerateToken` 签名改为接收 `now time.Time` 参数，不再内部调用 `time.Now()`。

---

### 🆕 [P3] ParseToken 签名方法校验不够严格 ✅ 已修复

- **文件**：`server/pkg/jwt/jwt.go:52-53`
- **状态**：**已修复**。已改为 `jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))`，仅接受 HS256 签名的 token。

---

### 测试覆盖评估

| 审查重点 | 覆盖情况 |
|---|---|
| JWT 签发与校验逻辑 | ✅ `pkg/jwt` 5 个测试用例（签发解析、过期、无效、空、算法校验）|
| 鉴权中间件（无 token / 无效 / 有效 / 过期） | ✅ `pkg/middleware` 4 个测试用例 |
| 密码 bcrypt 处理 | ⚠️ 间接覆盖（登录测试验证密码比对，但未测试 hash 格式）|
| 注册事务完整性（WithTx） | ✅ 覆盖（重复用户名测试验证唯一约束）|
| Nickname 长度校验 | ✅ `validateRegisterInput` 中校验 >64 |
| JWT 签名方法严格校验 | ✅ `NewParser` + `WithValidMethods(["HS256"])` |
| `now` 字段使用 | ✅ `Register`、`Login` 调用 `s.now()` |
| AuthRequired 无 token 拒绝 | ✅ middleware 测试覆盖（`TestAuthRequired_NoToken`）|
| 登录失败限流 | ❌ 未覆盖 |
| JWT 刷新 / 撤销 | ❌ 未覆盖 |

**测试通过率：9/9（100%）**——jwt 5 个 + middleware 4 个全部 PASS。

---

### 修复记录

| 编号 | 问题 | 状态 |
|---|---|---|
| 1 | [P0] AuthRequired 无 token 不拒绝 | ✅ 已修复 |
| 2 | [P1] JWT Secret 硬编码 | ✅ 已修复 |
| 3 | [P2] Register TOCTOU 竞态 | ❌ 未修复 |
| 4 | [P2] me handler 断言无保护 | ✅ 已修复 |
| 5 | [P2] 登录无失败次数限制 | ❌ 未修复 |
| 6 | [P3] JWT 无刷新与撤销机制 | ❌ 未修复 |
| 7 | [P3] bcrypt 在事务外计算 | ❌ 未修复（影响极小，可暂不修改）|
| 8 | [🆕 P2] Nickname 无长度校验 | ✅ 已修复 |
| 9 | [🆕 P3] `now` 字段未使用 | ✅ 已修复 |
| 10 | [🆕 P3] ParseToken 签名方法校验 | ✅ 已修复 |

**修复率：6/9（67%）**

---

服务端启动必须设 `JWT_SECRET` 环境变量，否则 panic。
