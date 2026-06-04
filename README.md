# 实时竞拍大师（Paimai）

直播电商场景的实时竞拍系统，支持多直播间、实时出价、自动结算全流程。

## 技术栈

| 层 | 技术 |
|---|---|
| 后端 | Go 1.22 + Gin + GORM + MySQL 8.0 |
| 缓存 | Redis 7（主从架构）|
| 实时推送 | Redis Stream → WebSocket |
| 前端 H5 | React 19 + TypeScript + Vite |
| 管理后台 | React 19 + TypeScript + Vite |
| 部署 | Docker Compose |

## 快速启动

**依赖：** [Docker Desktop](https://www.docker.com/products/docker-desktop/)

```bash
# 一键启动（构建镜像 + 启动服务 + 初始化数据）
./start.sh
```

启动后访问：

- **H5 直播间**：http://localhost:5173
- **管理后台**：http://localhost:5174
- **API**：http://localhost:8080/ping

演示账号：`demo` / `demo123456`

### 手动启动（不依赖 Docker）

```bash
# 1. 只启动 MySQL + Redis
docker compose up -d mysql redis-master redis-slave

# 2. 启动后端
cd server && go run .

# 3. 启动 H5 前端（新终端）
cd web-h5 && npm run dev

# 4. 启动管理后台（新终端）
cd web-admin && npm run dev
```

### 停止服务

```bash
docker compose down        # 停止并删除容器
docker compose down -v     # 停止并删除容器 + 数据卷（重置数据）
```

## 项目结构

```
paimai/
├── server/                 # Go 后端
│   ├── main.go
│   ├── config/             # 配置
│   ├── internal/
│   │   ├── handler/        # HTTP 控制器
│   │   ├── model/          # 数据模型
│   │   ├── repository/     # 数据访问层
│   │   ├── service/        # 业务逻辑
│   │   ├── statemachine/   # 竞拍状态机
│   │   ├── stream/         # Redis Stream 事件
│   │   └── websocket/      # WebSocket Hub
│   └── pkg/                # 通用工具
│       ├── db/
│       ├── jwt/
│       ├── middleware/
│       ├── redis/
│       └── response/
├── web-h5/                 # H5 前端（买家端）
│   └── src/
│       ├── api/            # API 客户端
│       ├── components/     # 组件
│       ├── hooks/          # Hook（useWebSocket）
│       └── pages/          # 页面
├── web-admin/              # PC 管理后台（商家端）
│   └── src/
│       ├── api/
│       └── pages/
├── scripts/
│   ├── init-demo.sh        # 初始化演示数据
│   └── reset-auction.sh    # 重置竞拍
├── docs/
│   ├── ai-dev-paradigm-4321.md
│   ├── implementation-plan.md
│   └── pipeline-reports/   # AI 产线交付报告
└── docker-compose.yml
```

## 核心 API

### 认证

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/auth/register` | 注册 |
| POST | `/api/auth/login` | 登录 |
| GET | `/api/auth/me` | 当前用户信息 |

### 用户端

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/rooms/:roomId` | 直播间详情 |
| GET | `/api/rooms/:roomId/auctions` | 竞拍列表 |
| GET | `/api/auctions/:id` | 竞拍详情 |
| GET | `/api/auctions/:id/ranking` | 排行榜 |
| POST | `/api/auctions/:id/bids` | 出价 |
| GET | `/api/rooms/:roomId/ws` | WebSocket 连接 |
| GET | `/api/orders` | 我的订单 |
| POST | `/api/orders/:id/pay` | 模拟支付 |

### 管理端

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/admin/rooms` | 创建直播间 |
| POST | `/api/admin/rooms/:id/live` | 开播 |
| POST | `/api/admin/rooms/:id/close` | 关播 |
| POST | `/api/admin/products` | 创建商品 |
| POST | `/api/admin/auctions` | 创建竞拍 |
| POST | `/api/admin/auctions/:id/publish` | 发布竞拍 |
| POST | `/api/admin/auctions/:id/start` | 开始竞拍 |
| POST | `/api/admin/auctions/:id/settle` | 结算竞拍 |
| POST | `/api/admin/orders/:id/pay` | 模拟支付 |

## 开发模式的主要决策

- **金额单位**：所有金额以「分」为单位，避免浮点精度问题
- **用户认证**：JWT（7 天有效期），开发阶段兼容无 token 请求
- **出价一致性**：Redis Lua 脚本原子判定 + 幂等键
- **实时推送**：出价 → Redis Stream → WebSocket Hub → 房间广播
- **结算触发**：出价时自动结算已过期竞拍 + 关播时批量结算

## 测试

```bash
# 后端单元测试
cd server && go test ./...

# 前端构建验证
cd web-h5 && npm run build
cd web-admin && npm run build
```

集成测试（需要 Docker MySQL + Redis）：

```bash
cd server && go test -tags=integration ./...
```

## 产线报告

项目采用「AI 为中心的分治产线」开发范式，每轮交付均生成产线报告：

- `docs/pipeline-reports/` — 按编号排列的交付报告

## License

MIT
