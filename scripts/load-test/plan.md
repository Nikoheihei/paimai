# 竞拍系统压测规划

## 测试环境

| 项目 | 配置 |
|---|---|
| 服务端口 | `http://localhost:8080` |
| WebSocket | `ws://localhost:8080/api/rooms/:roomId/ws?token=xxx` |
| MySQL | `localhost:3308` (容器映射) |
| Redis Master | `localhost:6381` (容器映射) |
| 数据库 | `paimai` |

## 压测分层

### 第一层：HTTP 接口压测

测核心 API 的 QPS / 延迟 / 错误率：

| 接口 | 方法 | 说明 |
|---|---|---|
| `/api/auth/login` | POST | 登录获取 token |
| `/api/rooms` | GET | 首页直播间列表 |
| `/api/rooms/:id/auctions` | GET | 直播间竞拍列表 |
| `/api/auctions/:id/bids` | POST | 出价（核心） |
| `/api/auctions/:id/ranking` | GET | 排行榜 |

### 第二层：WebSocket 压测

- 多个虚拟用户同时连接同一个 room 的 WS
- 验证广播延迟、消息丢失、断线重连

### 第三层：业务一致性测试

- 100/500/1000 人对同一 auction 同时出价
- 验证最终最高价正确、winner 唯一、无价格倒退

## 执行顺序

1. `01-setup.mjs` — 注册卖家 + 买家用户，创建房间 + 商品 + 竞拍
2. `02-http-bid.mjs` — HTTP 出价压测（50 → 100 → 300 → 500 并发）
3. `03-ws-connect.mjs` — WebSocket 连接数压测
4. `04-consistency.mjs` — 业务一致性测试

## 观测指标

- QPS / P95 / P99 延迟 / 错误率
- MySQL 连接数 / 慢查询 / 锁等待
- Redis CPU / OPS / 慢命令
- WebSocket 广播延迟
