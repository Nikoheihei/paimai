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
| `MYSQL_DSN` | Yes | - | MySQL connection string |
| `REDIS_MASTER_ADDR` | Yes | - | Redis master address (`host:port`) |
| `REDIS_SLAVE_ADDR` | No | Same as master | Redis slave address |
| `AGENT_LLM_API_KEY` | Yes* | - | DeepSeek V4 Pro API key (*required for Agent features) |
| `JWT_SECRET` | Yes | `change_me_in_production` | JWT signing secret |
| `SERVER_PORT` | No | `8080` | HTTP listen port |
| `AGENT_RUNNER_ENABLED` | No | `true` | Enable auto-bid runner |
| `AGENT_RUNNER_INTERVAL_MS` | No | `2000` | Runner scan interval (ms) |

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
