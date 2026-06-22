# Real-time Auction Master

> A live streaming auction platform powered by **AI Agent-to-Agent (A2A)** architecture with LLM-driven intelligent bidding, human-in-the-loop approval, and event-sourced reliability.

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19+-61DAFB?logo=react)](https://reactjs.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5+-3178C6?logo=typescript)](https://typescriptlang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Table of Contents

- [Features](#features)
- [System Architecture](#system-architecture)
- [Quick Start](#quick-start)
- [Project Structure](#project-structure)
- [Configuration](#configuration)
- [API Endpoints](#api-endpoints)
- [AI Agent System](#ai-agent-system)
- [State Machines](#state-machines)

---

## Features

### 1. Live Auction Full Pipeline
- **Live Room Management**: Sellers create streaming rooms with products (images, description, start price, reserve price, bid increment, cap price)
- **Real-time Bidding**: Buyers join rooms via WebSocket for real-time bid broadcasts and danmaku stream
- **Ranking & Countdown**: Real-time TOP bidder leaderboard with auto-settlement on countdown expiry
- **Payment & Shipping**: Winner confirms address → pays order → seller ships; auto-close on payment timeout

### 2. AI Agent Auto-Bidding
- **Natural Language Agent Creation**: Buyers describe intent in Chinese (e.g., *"Help me bid on jade pendants, max 800, follow others' bids"*)
- **3-Dimensional Strategy Customization**:
  - `trigger`: **lead** (active) / **follow** (wait for others to lead)
  - `pace`: **min_step** (minimum increment) / **reserve** (bid to reserve first)
  - `stopRatio`: budget ratio hard stop (0 = only hard cap, 0.8 = stop at 80% of budget)
- **Resident Runner**: Scans active agents × running auctions every 2 seconds, calls `DecideBid` engine, submits via full safety pipeline
- **Lifecycle Management**: activate / pause / stop; auto-transitions to `stopped_after_win` on winning

### 3. Pact Human Approval Gate
- **Auto Pact Creation**: When agent wins, system auto-generates a Pact protocol locking product snapshot, final price, and budget
- **Five Guard Conditions**: Budget check | Time window valid | Pact not expired | match_status=won | auction_status=sold
- **Payment Gate**: Only approved Pacts pass the payment gateway; timeout/rejection closes order
- **Full Audit Trail**: Every step from creation → parsing → bidding → winning → approval → payment traced by TraceID

### 4. Dual-Channel Intent Parser (LLM + Rule Engine)
- **Three-Level Priority**: Regex rules → Structured JSON fallback → DeepSeek V4 Pro API (semantic understanding fallback)
- **Prompt Engineering**: System prompt defines role (professional auction assistant), output format (JSON schema), few-shot examples
- **Graceful Degradation**: Falls back to rule engine on LLM timeout or parse failure

### 5. Multi-Terminal Clients
- **H5 Mobile** (React + TypeScript): Live room page (video player + product float panel + bid panel + danmaku) → Auction detail → Agent create/list/detail → Pact approval list → Order management → Address management
- **Admin Dashboard**: Product CRUD, auction lifecycle management, order status viewer

### 6. Event Sourcing & Outbox Reliability
- **Outbox Table**: All key business events written to `outbox_events` table, guaranteeing no event loss
- **State Machine Driven Transitions**: Auction (7 states), Order (3 states), Agent (5 states), Pact (4 states) — all governed by unified state machine
- **Optimistic Locking**: Bid operations use version field for concurrency control
- **Idempotency**: Every agent bid carries IdempotencyKey; duplicate requests return cached result

---

## System Architecture

```
┌─────────────────────┐     ┌─────────────────────────┐
│   Web-H5 Mobile     │     │    Admin Dashboard      │
│   React+TS+Vite     │     │    React+Ant Design     │
└────────┬────────────┘     └───────────┬─────────────┘
         │ REST / WS                     │ REST
         ▼                               ▼
┌──────────────────────────────────────────────────────┐
│               Gin API Server                         │
│         Router · JWT Middleware · CORS                │
├──────────────────────────────────────────────────────┤
│  Service Layer          │  AI Agent Engine           │
│  ┌─────────────────┐    │  ┌─────────────────────┐   │
│  │ PublicService   │    │  │ ParseIntent        │   │
│  │ AdminService    │───▶│  │ StrategySkill(3D)  │   │
│  │ AgentService    │    │  │ DecideBid (8-step) │   │
│  │ SettleService   │    │  │ Runner (2s tick)   │   │
│  └─────────────────┘    │  │ Pact Engine        │   │
│  └─────────────────┘    │  └─────────────────────┘   │
│  State Machine Engine   └────────────────────────────┤
│  (Auction/Order/Agent/Pact)                           │
├──────────────────────────────────────────────────────┤
│  Data Layer                                            │
│  MySQL 8.0  │  Redis 7 (Cache · Stream · Locks)       │
├──────────────────────────────────────────────────────┤
│  External Services                                     │
│  DeepSeek V4 Pro API (OpenAI Compatible)              │
└──────────────────────────────────────────────────────┘
```

---

## Quick Start

### Prerequisites

- **Docker & Docker Compose**
- **Go 1.22+** (for local dev without Docker)
- **Node.js 18+** (for frontend dev)
- **MySQL 8.0**, **Redis 7**

### Option A: Docker Compose (Recommended)

```bash
# Clone the repository
git clone https://github.com/Nikoheihei/paimai.git
cd paimai

# Copy and configure environment
cp .env.example .env
# Edit .env with your API keys:
# - AGENT_LLM_API_KEY=your_deepseek_api_key
# - JWT_SECRET=your_jwt_secret

# Start all services
docker-compose up -d
```

Services started:
| Service | Port | Description |
|---------|------|-------------|
| API Server | `:8080` | Go/Gin backend |
| Web H5 | `:5173` | Vite dev server |
| Web Admin | `:5174` | Admin dashboard |
| MySQL | `:3306` | Primary database |
| Redis | `:6379` | Cache & message broker |

### Option B: Local Development

```bash
# Backend
cd server
export MYSQL_DSN="root:rootpassword@tcp(localhost:3306)/paimai?charset=utf8mb4&parseTime=True&loc=Local"
export MYSQL_WRITE_DSN="$MYSQL_DSN"
export MYSQL_READ_DSN="$MYSQL_DSN"
export REDIS_MASTER_ADDR="localhost:6379"
export AGENT_LLM_API_KEY="sk-your-key"
go run main.go

# Frontend - H5
cd web-h5
npm install
npm run dev

# Frontend - Admin
cd web-admin
npm install
npm run dev
```

---

## Project Structure

```
paimai/
├── server/                    # Go backend (Gin framework)
│   ├── main.go                # Entry point: init DB, Redis, routes, background jobs
│   ├── config/                # Configuration loading
│   ├── internal/
│   │   ├── handler/           # HTTP route handlers (public, admin, agent, auth, settle)
│   │   ├── service/           # Business logic layer
│   │   │   ├── public.go      # Live room, bidding, ranking, WS broadcast
│   │   │   ├── admin.go       # Product CRUD, auction lifecycle
│   │   │   └── settle.go      # Auto-settlement, payment timeout
│   │   ├── agent/             # AI Agent core engine
│   │   │   ├── service.go     # Agent CRUD, bid submission, Pact management
│   │   │   ├── parser.go      # Intent parser: LLM + rule dual-channel
│   │   │   ├── policy.go      # DecideBid: 8-step decision engine (3D strategy)
│   │   │   ├── strategy_skill.go  # StrategySkill struct & builders
│   │   │   ├── runner.go      # Resident runner: 2s tick scan & auto-bid
│   │   │   └── store.go       # Agent data access layer
│   │   ├── statemachine/      # State machine definitions
│   │   │   └── auction.go     # Auction/Order/Agent/Pact state transitions
│   │   ├── model/             # Data models (GORM)
│   │   ├── repository/        # Data access (MySQL/Redis)
│   │   ├── session/           # Session management
│   │   ├── websocket/         # WebSocket Hub (gorilla/websocket)
│   │   └── stream/            # Redis Stream pub/sub, Outbox polling
│   └── pkg/                   # Shared packages (db, redis, jwt, middleware)
│
├── web-h5/                    # Mobile H5 frontend (React + TypeScript + Vite)
│   ├── src/
│   │   ├── pages/             # Page components
│   │   │   ├── LiveRoomPage.tsx
│   │   │   ├── AuctionDetailPage.tsx
│   │   │   ├── AgentListPage.tsx
│   │   │   ├── AgentDetailPage.tsx
│   │   │   ├── PactListPage.tsx
│   │   │   └── OrderPage.tsx
│   │   ├── components/        # UI components (VideoPlayer, AuctionPanel, etc.)
│   │   └── store/             # Zustand state management
│   └── package.json
│
├── web-admin/                 # Admin dashboard (React + Ant Design)
├── docs/                      # Design docs, architecture, code reviews
├── e2e/                       # E2E tests
├── docker-compose.yml         # Full stack orchestration
├── start.sh                   # Quick start script
└── README.md                  # This file
```

---

## Configuration

Environment variables (set in `.env` or `docker-compose.yml`):

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MYSQL_DSN` | Yes | - | Legacy MySQL DSN; used as fallback for both read and write |
| `MYSQL_WRITE_DSN` | No | `MYSQL_DSN` | Strong-consistency MySQL writer for commands, transactions, migrations, bidding, settlement, payment, Pact approval |
| `MYSQL_READ_DSN` | No | `MYSQL_WRITE_DSN` | Eventually consistent MySQL reader for initial snapshots, list pages, audit playback, and non-realtime reads |
| `REDIS_MASTER_ADDR` | Yes | - | Redis master address (`host:port`) |
| `REDIS_SLAVE_ADDR` | No | Same as master | Redis slave address |
| `AGENT_LLM_API_KEY` | Yes* | - | DeepSeek V4 Pro API key (*required for Agent features) |
| `JWT_SECRET` | Yes | `change_me_in_production` | JWT signing secret |
| `SERVER_PORT` | No | `8080` | HTTP listen port |
| `AGENT_RUNNER_ENABLED` | No | `true` | Enable auto-bid runner |
| `AGENT_RUNNER_INTERVAL_MS` | No | `2000` | Runner scan interval (ms) |

Read/write separation boundary:

- MySQL read DB serves initial HTTP snapshots only: room lists, room detail, product detail, ordinary order lists, agent lists, and audit playback.
- Realtime auction state still flows through MySQL writer transactions → outbox → Redis Stream → WebSocket.
- Post-write strong-consistency reads stay on the MySQL writer to avoid replica-lag mistakes.

---

## API Endpoints

### Public (No Auth)
| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/rooms` | List all live rooms |
| GET | `/api/rooms/:roomId` | Get room detail |
| GET | `/api/rooms/:roomId/auctions` | List room auctions |
| GET | `/api/auctions/:id` | Get auction detail |
| GET | `/api/auctions/:id/ranking` | Get bid ranking |
| POST | `/api/auctions/:id/bids` | Place a bid |
| GET | `/api/rooms/:roomId/ws` | WebSocket upgrade |

### Auth
| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/auth/register` | Register new user |
| POST | `/api/auth/login` | Login (returns JWT) |
| GET | `/api/auth/me` | Get current user info |

### Buyer Agent (Auth Required)
| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/agent/buyer-agents` | Create buyer agent (NL prompt) |
| GET | `/api/agent/buyer-agents` | List my agents |
| PATCH | `/api/agent/buyer-agents/:id/activate` | Activate agent |
| PATCH | `/api/agent/buyer-agents/:id/pause` | Pause agent |
| POST | `/api/agent/buyer-agents/:id/bids` | Submit agent bid |
| GET | `/api/agent/buyer-agents/:id/audit` | View audit logs |
| GET | `/api/agent/pacts` | List my pacts |
| POST | `/api/agent/pacts/:id/approve` | Approve pact (with address) |
| POST | `/api/agent/pacts/:id/reject` | Reject pact |

### Admin (Auth + Admin Role)
| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/admin/products` | Create product |
| GET/PATCH/DELETE | `/api/admin/products/:id` | Manage product |
| POST | `/api/admin/products/:id/relist` | Relist product as new auction |
| POST | `/api/admin/products/:id/offline` | Take product offline |
| POST | `/api/admin/auctions` | Create auction |
| PATCH | `/api/admin/auctions/:id` | Update auction |
| POST | `/api/admin/auctions/:id/start` | Start auction |
| POST | `/api/admin/auctions/:id/cancel` | Cancel auction |

---

## AI Agent System

### Decision Flow

```
User Input: "帮我拍翡翠挂件，最高800，有人出价再跟，先出到保留价，8成就停"
                              │
                       ┌──────▼──────┐
                       │  ParseIntent │
                       │  (LLM/Rule)  │
                       └──────┬──────┘
                              │
              ┌───────────────▼───────────────┐
              │     StrategySkill (3D)         │
              │ trigger=follow pace=reserve    │
              │ stopRatio=0.8 budget=80000¢    │
              └───────────────┬───────────────┘
                              │
                  ┌───────────▼───────────┐
                  │   DecideBid (8 steps)  │
                  │ 1. Intent match        │
                  │ 2. Already top?         │
                  │ 3. Trigger check        │
                  │ 4. Pace calculation     │
                  │ 5. StopRatio check      │
                  │ 6. Cap price            │
                  │ 7. Budget hard limit    │
                  │ 8. Legality check       │
                  └───────────┬───────────┘
                              │
                    ShouldBid=true
                    AmountCents=target
                              │
                  ┌───────────▼───────────┐
                  │  SubmitBuyerBid        │
                  │  (10 safety checks)    │
                  └───────────┬───────────┘
                              │
                    ┌─────────▼─────────┐
                    │  Won? → CreatePact │
                    │  (human approval)  │
                    └───────────────────┘
```

### 8-Step Decision Engine Details

| Step | Check | Reject Reason |
|------|-------|---------------|
| 0 | Pre-flight: auction running? Strategy decodable? Scope match? requireHumanPay? | Various |
| 1 | Product keyword match (prevents "ghost bidder") | `product does not match intent` |
| 2 | Already highest bidder? | `already highest bidder` |
| 3 | Trigger: follow mode needs existing leader | `follow mode waits until another buyer leads` |
| 4 | Pace: calculate target (min_step vs reserve priority) | N/A (calculation step) |
| 5 | StopRatio: budget ratio threshold | `price reached stop ratio threshold` |
| 6 | Cap price enforcement | target capped |
| 7 | Budget hard constraint | `would exceed budget` |
| 8 | Legal increment check | `no legal increment available` |

---

## State Machines

### Auction States (7)
```
draft → scheduled → running → sold
                        ↘ failed
 draft/scheduled/running → cancelled
 sold → payment_timeout
```

### Order States (3)
```
pending_payment → paid
                → closed
```

### Agent States (5)
```
draft → active ⇄ paused → stopped_after_win
                      ↘ expired
```

### Pact States (4)
```
created → approved (5 guard conditions)
       → rejected
       → expired
```

---

## Tech Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.22+, Gin, GORM |
| Frontend-H5 | React 19, TypeScript, Vite, Zustand |
| Frontend-Admin | React, Ant Design |
| Real-time | gorilla/websocket, Redis Streams |
| Database | MySQL 8.0, Redis 7 |
| AI/LLM | DeepSeek V4 Pro (OpenAI Compatible API) |
| Deployment | Docker Compose, Nginx |
| Auth | JWT (RS256) |

---

## License

This project is licensed under the MIT License.

---

## Acknowledgments

Built for the **Tencent Cloud AI Application Innovation Competition**.

---

# 中文版

# 实时拍卖大师

> 一个基于 **AI Agent-to-Agent (A2A)** 架构的直播拍卖平台，支持 LLM 驱动的智能出价、人工确认支付协议、实时竞价广播，以及事件驱动的可靠结算链路。

[![Go](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/)
[![React](https://img.shields.io/badge/React-19+-61DAFB?logo=react)](https://reactjs.org)
[![TypeScript](https://img.shields.io/badge/TypeScript-5+-3178C6?logo=typescript)](https://typescriptlang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## 功能特性

### 1. 直播拍卖完整链路

- **直播间管理**：卖家创建直播间并配置商品图片、描述、起拍价、保留价、加价幅度和封顶价。
- **实时竞价**：买家通过 WebSocket 进入直播间，实时接收出价和弹幕广播。
- **排名与倒计时**：实时 TOP 出价榜，倒计时结束后自动结算。
- **支付与发货**：中标用户确认地址、支付订单，卖家发货；超时未支付自动关闭。

### 2. AI Agent 自动出价

- **自然语言创建代理**：买家可以用中文描述意图，例如“帮我拍玉坠，最高 800，别人出价后再跟”。
- **三维策略配置**：
  - `trigger`：主动领先 `lead` / 等别人先出价 `follow`
  - `pace`：最低加价 `min_step` / 优先到保留价 `reserve`
  - `stopRatio`：预算比例止损线
- **常驻 Runner**：每 2 秒扫描活跃代理和进行中的拍卖，通过 `DecideBid` 引擎决策并走完整安全链路提交。
- **生命周期管理**：支持 activate / pause / stop；中标后自动进入 `stopped_after_win`。

### 3. Pact 人工确认闸门

- **自动创建 Pact**：代理中标后，系统锁定商品快照、成交价和预算并生成 Pact 协议。
- **五类守护条件**：预算检查、时间窗口、Pact 未过期、match_status=won、auction_status=sold。
- **支付闸门**：只有人工批准的 Pact 才允许进入支付；拒绝或超时则关闭订单。
- **完整审计链路**：从创建、解析、出价、中标、批准到支付，全程通过 TraceID 追踪。

### 4. 双通道意图解析

- **三层优先级**：正则规则、结构化 JSON 兜底、DeepSeek V4 Pro API 语义理解。
- **提示词工程**：系统提示定义专业拍卖助手角色、JSON 输出结构和 few-shot 示例。
- **优雅降级**：LLM 超时或解析失败时回退到规则引擎。

### 5. 多端客户端

- **H5 移动端**：直播间、拍卖详情、代理创建/列表/详情、Pact 审批、订单和地址管理。
- **管理后台**：商品 CRUD、拍卖生命周期管理、订单状态查看。

### 6. 事件溯源与可靠性

- **Outbox 表**：关键业务事件写入 `outbox_events`，降低事件丢失风险。
- **状态机驱动**：拍卖、订单、代理、Pact 都由统一状态机约束。
- **乐观锁**：竞价操作使用版本号处理并发冲突。
- **幂等性**：代理出价携带 IdempotencyKey，重复请求返回缓存结果。

## 快速开始

### 前置依赖

- Docker 与 Docker Compose
- Go 1.22+
- Node.js 18+
- MySQL 8.0 与 Redis 7

### Docker Compose 推荐方式

```bash
git clone https://github.com/Nikoheihei/paimai.git
cd paimai

cp .env.example .env
# 编辑 .env，配置 AGENT_LLM_API_KEY 和 JWT_SECRET

docker-compose up -d
```

启动服务：

| 服务 | 端口 | 说明 |
|------|------|------|
| API Server | `:8080` | Go/Gin 后端 |
| Web H5 | `:5173` | Vite 移动端 |
| Web Admin | `:5174` | 管理后台 |
| MySQL | `:3306` | 主数据库 |
| Redis | `:6379` | 缓存与消息通道 |

### 本地开发

```bash
# Backend
cd server
export MYSQL_DSN="root:rootpassword@tcp(localhost:3306)/paimai?charset=utf8mb4&parseTime=True&loc=Local"
export MYSQL_WRITE_DSN="$MYSQL_DSN"
export MYSQL_READ_DSN="$MYSQL_DSN"
export REDIS_MASTER_ADDR="localhost:6379"
export AGENT_LLM_API_KEY="sk-your-key"
go run main.go

# H5 frontend
cd web-h5
npm install
npm run dev

# Admin frontend
cd web-admin
npm install
npm run dev
```

## 项目结构

```
paimai/
├── server/                    # Go 后端，Gin 框架
│   ├── main.go                # 初始化 DB、Redis、路由和后台任务
│   ├── config/                # 配置加载
│   ├── internal/
│   │   ├── handler/           # HTTP handlers
│   │   ├── service/           # 直播、竞价、结算等业务逻辑
│   │   ├── agent/             # AI Agent 核心引擎
│   │   ├── statemachine/      # 拍卖/订单/代理/Pact 状态机
│   │   ├── model/             # GORM 数据模型
│   │   ├── repository/        # MySQL/Redis 数据访问
│   │   ├── websocket/         # WebSocket Hub
│   │   └── stream/            # Redis Stream 与 Outbox 轮询
│   └── pkg/                   # DB、Redis、JWT、中间件等共享包
├── web-h5/                    # 移动端 React + TypeScript + Vite
├── web-admin/                 # 管理后台 React + Ant Design
├── docs/                      # 设计文档、架构说明、代码审查
├── e2e/                       # 端到端测试
├── docker-compose.yml         # 全栈编排
├── start.sh                   # 快速启动脚本
└── README.md
```

## 配置说明

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `MYSQL_DSN` | 是 | - | MySQL DSN，读写 DSN 的兜底值 |
| `MYSQL_WRITE_DSN` | 否 | `MYSQL_DSN` | 强一致写库，用于交易、竞价、结算、支付和 Pact 审批 |
| `MYSQL_READ_DSN` | 否 | `MYSQL_WRITE_DSN` | 读库，用于列表页、详情快照、审计回放等普通查询 |
| `REDIS_MASTER_ADDR` | 是 | - | Redis 主节点地址 |
| `REDIS_SLAVE_ADDR` | 否 | 同 master | Redis 从节点地址 |
| `AGENT_LLM_API_KEY` | Agent 功能需要 | - | DeepSeek V4 Pro API Key |
| `JWT_SECRET` | 是 | `change_me_in_production` | JWT 签名密钥 |
| `SERVER_PORT` | 否 | `8080` | HTTP 服务端口 |
| `AGENT_RUNNER_ENABLED` | 否 | `true` | 是否启用自动出价 Runner |
| `AGENT_RUNNER_INTERVAL_MS` | 否 | `2000` | Runner 扫描间隔 |

## AI Agent 决策链路

```
User Intent
    |
    v
ParseIntent
    |
    v
StrategySkill
    |
    v
DecideBid
    |
    v
SubmitBid with safety checks
    |
    v
Winning -> Create Pact -> Human Approval
```

核心安全检查包括：拍卖状态、商品匹配、是否已领先、跟拍触发条件、预算比例、封顶价、硬预算约束、合法加价幅度，以及幂等性。

## 状态机

### Auction

```
draft -> scheduled -> running -> sold
                        \-> failed
draft/scheduled/running -> cancelled
sold -> payment_timeout
```

### Order

```
pending_payment -> paid
                -> closed
```

### Agent

```
draft -> active <-> paused -> stopped_after_win
                      \-> expired
```

### Pact

```
created -> approved
       -> rejected
       -> expired
```

## 技术栈

| 层级 | 技术 |
|------|------|
| 后端 | Go 1.22+, Gin, GORM |
| H5 前端 | React 19, TypeScript, Vite, Zustand |
| 管理后台 | React, Ant Design |
| 实时通信 | gorilla/websocket, Redis Streams |
| 数据库 | MySQL 8.0, Redis 7 |
| AI/LLM | DeepSeek V4 Pro, OpenAI-compatible API |
| 部署 | Docker Compose, Nginx |
| 认证 | JWT |

## License

本项目使用 MIT License。

## 致谢

项目为 **腾讯云 AI 应用创新大赛** 构建。
