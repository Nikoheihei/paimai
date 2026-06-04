# Code Review 记录

> CodeBuddy 负责代码审查，Codex 负责修复。
> 按模块分批 review，每批一个独立文件。

## 批次划分

| 批次 | 模块 | 文件 | 状态 |
|---|---|---|---|
| 001 | 出价核心链路 | `public.go`, `public_test.go` | ⏳ 待 review |
| 002 | 结算与订单 | `settle.go`, `settle_test.go`, `admin.go` | ⏳ 待 review |
| 003 | 用户认证 | `auth.go`, `auth_test.go`, `jwt.go`, `middleware/auth.go` | ⏳ 待 review |
| 004 | WebSocket & Stream | `hub.go`, `client.go`, `consumer.go` | ⏳ 待 review |
| 005 | 竞拍状态机 | `auction.go`, `auction_test.go` | ⏳ 待 review |
| 006 | 服务端入口与路由 | `main.go`, `handler/*.go` | ⏳ 待 review |
| 007 | 前端 H5 | `web-h5/src/pages/*.tsx`, `hooks/` | ⏳ 待 review |
| 008 | 前端管理后台 | `web-admin/src/pages/*.tsx` | ⏳ 待 review |
| 009 | 部署与脚本 | `Dockerfile`, `init-demo.sh`, `docker-compose.yml` | ⏳ 待 review |

## Review 输出格式

每批 review 文件按以下格式输出：

```
## [批次号] 模块名称

### [P0] 问题标题
- 文件：`路径:行号`
- 类型：逻辑错误 / 并发安全 / 边界遗漏 / 性能隐患
- 描述：一句话说明
- 影响面：
- 建议修复：

### [P1] ...
...
```
