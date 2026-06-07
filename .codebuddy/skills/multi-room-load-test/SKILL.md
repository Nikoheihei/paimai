---
name: multi-room-load-test
description: |
  This skill should be used when the user wants to run multi-room, multi-product, multi-auction load tests on the paimai auction system.
  It provides a script that cleans test data, creates N rooms × M products × K auctions, registers U users, and runs a stress test with detailed metrics.
  Triggers include: "多房间压测", "多拍卖压测", "multi-room load test", "多房间多商品", "stress test multiple auctions", "跑压测".
---

# Multi-Room Multi-Auction Load Test Skill

## Purpose

Run realistic load tests against the paimai auction system by simulating multiple rooms, products, and auctions simultaneously — instead of hammering a single auction ID.

## When to Use

Use this skill whenever the user asks to run a multi-room / multi-auction stress test. Do NOT write a new script from scratch; use the bundled `scripts/run.mjs`.

## How to Use

### Step 1: Run the stress test

Execute the bundled script with the desired parameters:

```bash
node .codebuddy/skills/multi-room-load-test/scripts/run.mjs <rooms> <auctionsPerRoom> <users> <durationSec>
```

Parameters:
- `rooms`: Number of live rooms to create (e.g., 50, 100, 200)
- `auctionsPerRoom`: Number of auctions per room (e.g., 3)
- `users`: Total simulated buyers (e.g., 500, 1000, 2000)
- `durationSec`: Test duration in seconds (e.g., 20)

Example:
```bash
node .codebuddy/skills/multi-room-load-test/scripts/run.mjs 200 3 2000 20
```

The script will:
1. Clean test data (bids, orders, outbox, auctions, products, rooms) + flush Redis
2. Register 1 seller + all buyer users (batch of 20 concurrent)
3. Create rooms, products, and auctions (all with high cap price 9,999,999 cents to avoid early termination)
4. Run the stress test with users randomly picking auctions to bid on (100-500ms intervals)
5. Output a JSON report with HTTP metrics, bid metrics, latency percentiles, and infrastructure stats

### Step 2: Run diagnostics alongside (optional)

To monitor Consumer Lag, Stream length, Outbox backlog, MySQL threads, Redis OPS, and goroutines during the test, run the diagnose script in parallel:

```bash
# Terminal 1: Start diagnose collector (runs 30s)
node .codebuddy/skills/multi-room-load-test/scripts/diagnose.mjs > diagnose.csv

# Terminal 2: Start the stress test while diagnose is running
node .codebuddy/skills/multi-room-load-test/scripts/run.mjs 200 3 2000 20
```

### Test Ladder (recommended)

| Rooms | Auctions/Room | Total Auctions | Users | Users/Auction |
|-------|--------------|----------------|-------|---------------|
| 50    | 3            | 150            | 500   | ~3.3          |
| 100   | 3            | 300            | 1000  | ~3.3          |
| 150   | 3            | 450            | 1500  | ~3.3          |
| 200   | 3            | 600            | 2000  | ~3.3          |

### Key Metrics

The report includes:
- HTTP: total requests, 2xx rate, 4xx rate, system error rate (5xx+timeout), avg QPS
- Bid: total bids, accepted, accepted/sec, business accept rate, top reject reasons
- Latency: avg, P50, P95, P99, max
- Infrastructure: MySQL questions/threads/slow queries, Redis ops/rejected connections

### Architecture Notes

- MySQL is the single Truth Source
- Redis is cache + pre-filter only (price comparison in Lua script rejects low bids before hitting MySQL)
- Outbox → Redis Stream → Consumer updates Redis state + broadcasts WebSocket
- Optimistic lock conflicts return HTTP 409 (not 500)
- Outbox poller: 100ms interval, 500 batch size
- Consumer: 100ms poll interval, 500 batch size
