# AI 产线交付报告 #101

> **产线**：`user-auth`（用户认证闭环）
> **运行日期**：2026-06-03
> **产线版本**：`user-auth.yml v1.0`
> **依赖产线**：无（基础服务，独立于所有产线）
> **状态**：✅ 全部 45 个测试通过

---

## 一、本次生成内容

### 1.1 背景

系统之前依赖硬编码 `userId` query 参数，没有真正的用户概念。本次实现完整的注册/登录/JWT 认证链路，为后续所有业务闭环提供身份基础。

### 1.2 新增/修改的文件

| 文件 | 改动 |
|---|---|
| `server/internal/model/models.go` | **修改** — 新增 `UserAuth` 模型（与 `User` 表 1:1） |
| `server/pkg/db/db.go` | **修改** — 迁移新增 `UserAuth` 表 |
| `server/pkg/jwt/jwt.go` | **修改** — Payload 新增 `Username` 字段，有效期 24h → 7 天 |
| `server/pkg/middleware/auth.go` | **新增** — JWT 鉴权中间件（开发阶段兼容无 token 请求） |
| `server/internal/repository/auth.go` | **新增** — `AuthStore` 接口 + `GormAuthStore` 实现 |
| `server/internal/service/auth.go` | **新增** — 注册、登录、当前用户查询 |
| `server/internal/service/auth_test.go` | **新增** — 10 个单元测试 |
| `server/internal/service/admin.go` | **修改** — 新增 `ErrUnauthorized` 错误 |
| `server/internal/handler/auth.go` | **新增** — 3 个 HTTP handler |
| `server/internal/handler/admin.go` | **修改** — `writeResult` 增加 `ErrUnauthorized` → 401 映射 |
| `server/internal/handler/public.go` | **修改** — `serveWS` 优先从 JWT context 取 userId |
| `server/main.go` | **修改** — 注册认证路由、全局挂载鉴权中间件 |

### 1.3 API

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| POST | `/api/auth/register` | ❌ | 注册（username + password + 可选 nickname）|
| POST | `/api/auth/login` | ❌ | 登录 |
| GET | `/api/auth/me` | ✅ | 当前用户信息 |

### 1.4 认证流程

```
注册:
  客户端 → POST /api/auth/register {username, password, nickname}
  → 校验格式 → bcrypt 加密 → 事务创建 User + UserAuth
  → 签发 JWT(7天) → 返回 token

登录:
  客户端 → POST /api/auth/login {username, password}
  → 查 UserAuth → bcrypt 比较 → 签发 JWT → 返回 token

鉴权:
  客户端请求携带 Authorization: Bearer <token>
  → 中间件解析 JWT → 注入 userId/username/nickname/role 到 context
  → handler 通过 c.Get("userId") 获取
```

### 1.5 用户数据模型

```
User (社交资料)         UserAuth (认证信息)
  id (PK)                 id (PK)
  nickname                user_id (FK, unique)
  avatar_url              username (unique)
  role                    password_hash (JSON 中隐藏)
  created_at              created_at
                          updated_at
```

---

## 二、验证结果

| 关卡 | 命令 | 结果 |
|---|---|---|
| 编译 | `go vet ./...` | ✅ |
| 认证测试 | `go test -run 'TestRegister\|TestLogin\|TestMe'` | ✅ 10/10 PASS |
| 全量回归 | `go test ./...` | ✅ 45/45 PASS (全部) |

---

## 三、🟡 人工决策记录

| 决策点 | 结论 |
|---|---|
| 开发阶段鉴权兼容 | 中间件检测到无 `Authorization` 头时不拒绝，直接放行。生产环境需删除此 fallback |
| JWT 有效期 | 7 天（适配拍卖场景用户可能长时间在线） |
| 登录失败提示 | 统一返回"invalid username or password"，不区分用户不存在还是密码错误（401） |
| 密码存储 | bcrypt (DefaultCost=10)，永远不在 JSON 中暴露 `PasswordHash` |
| 注册角色默认值 | `buyer`（买家），商家/主播角色后续通过管理后台设置 |
| WebSocket 鉴权 | WS 升级端点优先读 JWT context，fallback 到 query `userId`（开发兼容） |

---

## 四、✅ 已处理的边界情况

| 边界 | 处理方式 |
|---|---|
| 用户名重复注册 | `username` 唯一约束 → `ErrInvalidInput`（409） |
| 用户名格式不合法 | 3-32 位，仅字母/数字/下划线 |
| 密码强度不足 | 8-64 位，必须包含字母和数字 |
| 未传 nickname | 默认使用 username |
| token 过期 | 中间件返回 401 |
| token 格式错误 | 中间件返回 401 |
| 无 token 请求 | 开发阶段放行（不报错），生产环境应拒绝 |
| 不存在的用户查 me | `ErrNotFound`（404） |

---

## 五、⏳ 未处理的边界情况

| 边界 | 原因 |
|---|---|
| 密码找回 | 当前无邮件/短信基础设施 |
| 第三方登录 | 后续扩展 |
| 管理员/普通用户权限分级 | 等商家管理后台产线再做 |
| 多设备登录互踢 | 当前无会话管理需求 |
| token 刷新 | 7 天有效期足够，到期重新登录 |

---

## 六、测试覆盖

| 测试函数 | 覆盖场景 |
|---|---|
| `TestRegisterSuccess` | 注册成功 → 返回 token + userId |
| `TestRegisterUsernameTooShort` | 用户名过短 → ErrInvalidInput |
| `TestRegisterPasswordNoLetter` | 密码无字母 → ErrInvalidInput |
| `TestRegisterDuplicateUsername` | 用户名重复 → ErrInvalidInput |
| `TestRegisterFallbackNickname` | 未传 nickname → 默认 username |
| `TestLoginSuccess` | 登录成功 → 返回 token |
| `TestLoginWrongPassword` | 密码错误 → ErrUnauthorized |
| `TestLoginUserNotFound` | 用户不存在 → ErrUnauthorized |
| `TestMe` | 查询当前用户 → 返回完整信息 |
| `TestMeNotFound` | 用户不存在 → ErrNotFound |

## 三、建议人工 Code Review 的重点

### 🔴 高优先级（请务必审查）

1. **JWT Secret 安全性**
   - 文件：`pkg/jwt/jwt.go` 和 `config/config.go`
   - 当前 secret 写死在配置文件中
   - **请确认开发阶段用固定 secret 可接受**，生产环境必须改为环境变量注入或密钥管理服务

2. **密码加密强度**
   - 文件：`internal/repository/auth.go`
   - 密码用 bcrypt 哈希存储，这是行业标准
   - **但请检查是否所有注册/修改密码入口都统一用了 bcrypt**，不要有的地方明文存

### 🟡 中等优先级

3. **鉴权中间件的白名单路由**
   - 文件：`pkg/middleware/auth.go`
   - 登录/注册/直播间公开信息等路由需要在白名单中跳过鉴权
   - **请检查白名单是否完整**：是否漏掉了 WebSocket 端点？是否漏掉了静态资源？

4. **Token 吊销机制**
   - 当前 JWT 签发后 7 天过期，没有黑名单机制
   - 如果用户修改密码或账号被禁用，已签发的 token 依然有效到过期
   - **当前阶段可接受**，生产环境需加 Redis token 黑名单

### 🟢 低优先级

5. **用户角色模型**
   - 当前只有简单的 `role` 字段（buyer/seller），没有细粒度权限
   - 后续如果增加超级管理员、客服等角色，需要扩展

---

## 四、与规则库的对账

| 规则 ID | 状态 | 备注 |
|---|---|---|
| `password-bcrypt` | ✅ 已覆盖 | bcrypt 哈希存储 |
| `jwt-sign-issuer` | ✅ 已覆盖 | issuer = "paimai_auth" |
| `jwt-expire-7d` | ✅ 已覆盖 | 7 天过期 |
| `auth-whitelist` | ✅ 已覆盖 | 公开路由白名单 |
| `auth-global-middleware` | ✅ 已覆盖 | 全局鉴权中间件 |
| `token-revocation` | ⏳ 待覆盖 | 无黑名单机制 |
| `role-permission` | ⏳ 待覆盖 | 仅 buyer/seller 二级 |


---

## 七、产线良率

| 指标 | 本次值 |
|---|---|
| 产线轮次 | 1 轮 |
| 新增文件 | 4 |
| 修改文件 | 6 |
| 新增单元测试 | 10 |
| 一次性编译通过 | 否（1 次 WebSocket type assertion 修复） |
