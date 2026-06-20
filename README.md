# Paimai A2A Auction Commerce

Live auction commerce system with a rule-governed autonomous bidding agent layer.

## What This Project Demonstrates

- Real-time auction bidding with MySQL as the source of truth and Redis as hot state / stream infrastructure.
- Buyer-owned bidding agents that can observe auctions and submit bids only through the existing buyer bid path.
- Human-in-the-loop Pact approval before payment for every agent-assisted win.
- Rule-Governed Memory Agent architecture:
  - Redis-backed short-term session state for the current auction.
  - User-visible bidding rules such as approval thresholds and avoid-keyword rules.
  - Hard BidGuard enforcement before every agent bid.
  - Append-only audit logs and post-auction episode summaries.

## Agent Safety Model

The agent layer is intentionally outside the core auction state machine.

Agents may:

- parse user bidding intent,
- watch matching running auctions,
- decide whether to bid,
- call the existing bid service as the owning buyer,
- generate Pact approval objects after a win,
- summarize completed auction episodes.

Agents may not:

- bid for another buyer,
- let merchant or platform agents bid,
- exceed the user budget,
- continue automatically above the approval threshold,
- approve Pact objects,
- pay or move funds.

Memory is context. BidGuard is enforcement.

## Rule-Governed Memory Agent

The agent runtime uses three practical memory/control layers.

### 1. Short-Term Memory

`agent_session_state:{agent_id}:{auction_id}` is stored in Redis when Redis is available, with a TTL that lasts until the auction ends plus 24 hours.

It records the current auction state, budget constraints, approval threshold, recent agent decisions, plan state, and risk flags.

### 2. User Bidding Rules

`agent_bidding_rules` stores explicit or system-default rules:

- `approval_threshold`: default `0.9`; bids above 90% of budget require user confirmation.
- `avoid_keyword`: blocks products that match user-defined negative keywords.

These rules are visible authorization context, not hidden inferred memory.

### 3. Episode Summary

`agent_episode_summaries` compresses completed agent-assisted auctions into a compact outcome record. A rejected near-budget Pact creates a recommendation, but it does not automatically modify future hard rules.

## Main Services

- `server/internal/agent/service.go`: agent orchestration, Pact gate, audit, guard checks.
- `server/internal/agent/runner.go`: background runner for active buyer agents.
- `server/internal/agent/memory_guard.go`: bidding rules and BidGuard enforcement.
- `server/internal/agent/session_state.go`: Redis/in-memory STM store.
- `server/internal/agent/episode.go`: post-auction episode summaries.

## Run Locally

```bash
docker compose up -d
```

Backend:

```bash
cd server
go run .
```

Buyer H5 frontend:

```bash
cd web-h5
npm install
npm run dev
```

Admin frontend:

```bash
cd web-admin
npm install
npm run dev
```

## Tests

Focused agent tests:

```bash
cd server
go test ./internal/agent
```

Full backend tests:

```bash
cd server
go test ./...
```

Integration tests that require MySQL or Redis will skip when the local containers are unavailable.

## Research-Oriented CV Summary

Built a rule-governed memory architecture for autonomous auction agents, combining Redis-based session working memory, user-defined bidding rules, hard BidGuard enforcement, human approval thresholds, append-only audit traces, Pact-gated payment, and episodic post-auction summaries for safe personalized agent behavior.
