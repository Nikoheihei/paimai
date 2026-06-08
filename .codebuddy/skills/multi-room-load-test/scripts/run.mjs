/**
 * run.mjs — 多房间多商品多拍卖压测（含 WebSocket 模拟）
 *
 * 用法:
 *   node run.mjs <rooms> <auctionsPerRoom> <users> <durationSec>
 *
 * 每个用户:
 *   1. 建立 WebSocket 连接到对应 roomId（通过 token 认证）
 *   2. 循环 HTTP 出价
 *   3. 监听 WS 消息，记录广播延迟
 *   4. 测试结束后断开
 */

import { WebSocket } from 'ws'

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const WS_BASE = BASE.replace('http://', 'ws://').replace('https://', 'wss://')

function uid() { return `${Math.random().toString(36).slice(2, 8)}` }
function randInt(min, max) { return Math.floor(Math.random() * (max - min + 1)) + min }

async function apiJson(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...options.headers }
  const res = await fetch(`${BASE}${path}`, { ...options, headers })
  const body = await res.json()
  if (body.code !== 0) throw new Error(`${path}: ${body.message || `HTTP ${res.status}`}`)
  return body.data
}

async function apiAuth(path, token, options = {}) {
  const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, ...options.headers }
  const res = await fetch(`${BASE}${path}`, { ...options, headers })
  const body = await res.json()
  if (body.code !== 0) throw new Error(`${path}: ${body.message || `HTTP ${res.status}`}`)
  return body.data
}

async function main() {
  const ROOMS = parseInt(process.argv[2] || '50')
  const AUCTIONS_PER_ROOM = parseInt(process.argv[3] || '3')
  const USERS = parseInt(process.argv[4] || '500')
  const DURATION = parseInt(process.argv[5] || '20')

  const TOTAL_AUCTIONS = ROOMS * AUCTIONS_PER_ROOM

  console.log('')
  console.log(`╔══════════════════════════════════════════════════════════════╗`)
  console.log(`║  多房间多拍卖压测 (含 WebSocket)                               ║`)
  console.log(`║  房间:${ROOMS}  拍卖/房间:${AUCTIONS_PER_ROOM}  总拍卖:${TOTAL_AUCTIONS}  用户:${USERS}  时长:${DURATION}s  ║`)
  console.log(`╚══════════════════════════════════════════════════════════════╝`)
  console.log('')

  // ====== 阶段 1: 清理 ======
  console.log('[1/5] 清理...')
  try {
    const { execSync } = await import('child_process')
    execSync('docker exec paimai-redis-master redis-cli FLUSHDB 2>/dev/null', { timeout: 5000 })
    for (const t of ['bids', 'orders', 'outbox_events', 'auctions', 'products', 'live_rooms']) {
      execSync(`docker exec paimai-mysql mysql -uroot -prootpassword paimai -e "DELETE FROM ${t}" 2>/dev/null`, { timeout: 5000 })
    }
    console.log('  OK')
  } catch (_) { console.log('  跳过') }

  // ====== 阶段 2: 注册 ======
  console.log(`[2/5] 注册 1 seller + ${USERS} 用户...`)
  const seller = await apiJson('/api/auth/register', {
    method: 'POST',
    body: JSON.stringify({ username: `slr${uid()}`, password: 'Test123456', nickname: '卖家', role: 'seller' }),
  })
  const sellerToken = seller.token

  const users = []
  const batchSize = 20
  for (let i = 0; i < USERS; i += batchSize) {
    const batch = []
    for (let j = 0; j < batchSize && i + j < USERS; j++) {
      batch.push(apiJson('/api/auth/register', {
        method: 'POST',
        body: JSON.stringify({ username: `u${i + j}${uid()}`, password: 'Test123456', nickname: `用户${i + j}`, role: 'buyer' }),
      }))
    }
    const results = await Promise.all(batch)
    results.forEach(r => users.push({ userId: r.userId, token: r.token }))
    if (i % 100 === 0) process.stderr.write(`\r  已注册 ${Math.min(i + batchSize, USERS)}/${USERS}`)
  }
  console.log(`\r  完成: ${USERS} 用户`)

  // ====== 阶段 3: 创建拍卖 ======
  console.log(`[3/5] 创建 ${ROOMS} 房间 × ${AUCTIONS_PER_ROOM} 拍卖...`)
  const now = new Date()
  const endAt = new Date(now.getTime() + 7200_000)
  const auctions = []

  for (let r = 0; r < ROOMS; r++) {
    const room = await apiAuth('/api/admin/rooms', sellerToken, {
      method: 'POST', body: JSON.stringify({ title: `房间${r + 1}`, coverUrl: '' }),
    })
    await apiAuth(`/api/admin/rooms/${room.id}/live`, sellerToken, { method: 'POST' })
    for (let a = 0; a < AUCTIONS_PER_ROOM; a++) {
      // 每个拍卖独立商品，避免 product locked
      const product = await apiAuth('/api/admin/products', sellerToken, {
        method: 'POST', body: JSON.stringify({ name: `商品R${r + 1}A${a + 1}`, description: 'test', imageUrl: '' }),
      })
      const auction = await apiAuth('/api/admin/auctions', sellerToken, {
        method: 'POST',
        body: JSON.stringify({
          roomId: room.id, productId: product.id, mode: 'extension',
          startPriceCents: randInt(0, 5000), bidIncrementCents: 100, capPriceCents: 9999999,
          reservePriceCents: 0, startAt: now.toISOString(), endAt: endAt.toISOString(),
          extendThresholdSec: 30, extendDurationSec: 30,
        }),
      })
      await apiAuth(`/api/admin/auctions/${auction.id}/publish`, sellerToken, { method: 'POST' })
      await apiAuth(`/api/admin/auctions/${auction.id}/start`, sellerToken, { method: 'POST' })
      auctions.push({ auctionId: auction.id, roomId: room.id })
    }
    if ((r + 1) % 20 === 0) process.stderr.write(`\r  已创建 ${r + 1}/${ROOMS}`)
  }
  console.log(`\r  完成: ${TOTAL_AUCTIONS} 拍卖`)

  // ====== 阶段 4: 建立 WebSocket 连接 ======
  console.log(`[4/5] 建立 ${USERS} 个 WebSocket 连接...`)

  // 按 roomId 分组拍卖
  const roomAuctions = new Map() // roomId -> [auction]
  for (const a of auctions) {
    if (!roomAuctions.has(a.roomId)) roomAuctions.set(a.roomId, [])
    roomAuctions.get(a.roomId).push(a)
  }

  // 每个用户分配一个 roomId，同时记录该房间内的拍卖列表
  const userRoomMap = users.map((_, i) => auctions[i % auctions.length].roomId)
  const userAuctions = users.map((_, i) => roomAuctions.get(userRoomMap[i]) || [auctions[i % auctions.length]])

  // WS 指标
  let wsConnected = 0, wsFailed = 0, wsDisconnected = 0
  let wsMsgReceived = 0
  let wsMissingTimestamp = 0
  const wsLatencies = [] // 广播延迟: WS消息到达时间 - serverSentAt

  // 建立 WebSocket 连接
  const wsSockets = []
  for (let i = 0; i < USERS; i++) {
    const user = users[i]
    const roomId = userRoomMap[i]
    const wsUrl = `${WS_BASE}/api/rooms/${roomId}/ws?token=${user.token}`

    try {
      await new Promise((resolve, reject) => {
        const ws = new WebSocket(wsUrl)
        const timeout = setTimeout(() => { ws.close(); reject(new Error('timeout')) }, 5000)

        ws.on('open', () => {
          clearTimeout(timeout)
          wsConnected++
          ws.on('message', (data) => {
            wsMsgReceived++
            try {
              const msg = JSON.parse(data.toString())
              // 优先读取 serverSentAt / eventSentAt / sentAt
              const sentAt = msg.serverSentAt || msg.eventSentAt || msg.sentAt
              if (sentAt && (msg.type === 'bid.accepted' || msg.type === 'auction.updated')) {
                const delay = Date.now() - sentAt
                if (delay >= 0 && delay < 60000) {
                  wsLatencies.push(delay)
                }
              } else if (msg.type === 'bid.accepted' || msg.type === 'auction.updated') {
                wsMissingTimestamp++
              }
            } catch (_) {}
          })
          ws.on('close', () => { wsDisconnected++ })
          ws.on('error', () => { wsFailed++; wsDisconnected++ })
          resolve()
        })

        ws.on('error', () => { clearTimeout(timeout); wsFailed++; reject(new Error('connect failed')) })
        wsSockets.push(ws)
      })
    } catch {
      wsFailed++
    }
    if ((i + 1) % 200 === 0) process.stderr.write(`\r  已连接 ${i + 1}/${USERS} (成功:${wsConnected} 失败:${wsFailed})`)
  }
  console.log(`\r  完成: ${wsConnected} 连接成功, ${wsFailed} 失败`)

  // ====== 阶段 5: 压测 ======
  console.log(`[5/5] 压测 ${USERS} 用户 ${DURATION}s...`)
  console.log('')

  let httpTotal = 0, http2xx = 0, http4xx = 0, http5xx = 0, httpTimeout = 0
  let bidAccepted = 0, bidRejected = 0, bidSystemError = 0
  const rejectReasons = {}
  const rejectByCode = {
    BID_TOO_LOW: 0,
    BID_TOO_FREQUENT: 0,
    IN_FLIGHT: 0,
    AUCTION_NOT_RUNNING: 0,
    AUCTION_ENDED: 0,
    AUCTION_CACHE_MISSING: 0,
    BID_STEP_INVALID: 0,
    INVALID_RULE: 0,
    OPTIMISTIC_LOCK: 0,
    MYSQL_TOO_LOW: 0,
    MYSQL_NOT_RUNNING: 0,
    MYSQL_ENDED: 0,
    IDEMPOTENT_REPLAY: 0,
    UNKNOWN: 0,
  }
  const auctionStats = new Map() // auctionId -> { requests, accepted, rejected, conflicts }
  const latencies = []
  const startTime = Date.now()

  async function bidRound(userIdx, iter) {
    const user = users[userIdx]
    // 从用户所属房间的拍卖中随机选
    const myAuctions = userAuctions[userIdx] || auctions
    const a = myAuctions[randInt(0, myAuctions.length - 1)]
    const auctionId = a.auctionId
    const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${user.token}` }

    // 初始化 auction 统计
    if (!auctionStats.has(auctionId)) {
      auctionStats.set(auctionId, { requests: 0, accepted: 0, rejected: 0, conflicts: 0 })
    }
    const aStat = auctionStats.get(auctionId)
    aStat.requests++

    // 1. 获取拍卖详情
    let res
    try { res = await fetch(`${BASE}/api/auctions/${auctionId}`, { headers }) }
    catch (_) { httpTotal++; httpTimeout++; return }
    httpTotal++
    if (res.status >= 200 && res.status < 300) http2xx++
    else if (res.status >= 500) { http5xx++; return }
    else { http4xx++; return }

    let cp = 0, status = ''
    try { const b = await res.json(); cp = b.data?.currentPriceCents || 0; status = b.data?.status || '' } catch (_) { return }
    if (status !== 'running') return

    // 2. 出价
    const steps = 1 + Math.floor(Math.random() * 5)
    const amount = cp + steps * 100
    const idemKey = `mr-${auctionId}-${user.userId}-${iter}-${Date.now()}`
    const tBid = Date.now()

    try {
      res = await fetch(`${BASE}/api/auctions/${auctionId}/bids`, {
        method: 'POST', headers,
        body: JSON.stringify({ userId: user.userId, amountCents: amount, idempotencyKey: idemKey }),
      })
    } catch (_) { httpTotal++; httpTimeout++; bidSystemError++; return }
    httpTotal++
    const ms = Date.now() - tBid
    latencies.push(ms)

    if (res.status >= 200 && res.status < 300) {
      http2xx++
      const body = await res.json()
      if (body.data?.accepted) {
        bidAccepted++
        aStat.accepted++
      } else {
        bidRejected++
        aStat.rejected++
        const r = body.message || '?'
        const code = body.data?.rejectCode || 'UNKNOWN'
        rejectReasons[r] = (rejectReasons[r] || 0) + 1
        rejectByCode[code] = (rejectByCode[code] || 0) + 1
      }
    } else if (res.status === 409) {
      http4xx++; bidRejected++
      aStat.rejected++
      try {
        const b = await res.json()
        const r = b.message || '409'
        const code = b.data?.rejectCode || 'UNKNOWN'
        rejectReasons[r] = (rejectReasons[r] || 0) + 1
        rejectByCode[code] = (rejectByCode[code] || 0) + 1
        if (code === 'OPTIMISTIC_LOCK') aStat.conflicts++
      } catch (_) {
        rejectByCode.UNKNOWN++
      }
    } else if (res.status >= 500) {
      http5xx++; bidSystemError++
    } else {
      http4xx++; bidRejected++
      aStat.rejected++
    }
  }

  async function worker(i) {
    let iter = 0
    while (Date.now() - startTime < DURATION * 1000) {
      await bidRound(i, iter++)
      await new Promise(r => setTimeout(r, 100 + Math.random() * 400))
    }
  }

  const workers = []
  for (let i = 0; i < USERS; i++) workers.push(worker(i))

  let lastCount = 0, lastTime = startTime
  const monitor = setInterval(() => {
    const now = Date.now()
    const elapsed = ((now - startTime) / 1000).toFixed(1)
    const avgQps = Math.round(httpTotal / Math.max(1, (now - startTime) / 1000))
    const instantQps = Math.round((httpTotal - lastCount) / ((now - lastTime) / 1000))
    lastTime = now; lastCount = httpTotal
    const tb = bidAccepted + bidRejected
    const ar = tb > 0 ? (bidAccepted / tb * 100).toFixed(1) : '0'
    const wsAvg = wsLatencies.length > 0 ? Math.round(wsLatencies.reduce((a, b) => a + b, 0) / wsLatencies.length) : 0
    process.stderr.write(
      `\r  [${elapsed}s] HTTP:${httpTotal} QPS:${avgQps}(瞬${instantQps}) | WS:${wsConnected}连/${wsMsgReceived}msg 延迟avg:${wsAvg}ms | 2xx:${httpTotal>0?(http2xx/httpTotal*100).toFixed(1):'0'}% | 出价成功:${bidAccepted} acc:${ar}%`,
    )
  }, 1000)

  await Promise.all(workers)
  clearInterval(monitor)

  // 关闭所有 WS 连接
  for (const ws of wsSockets) {
    try { ws.close() } catch (_) {}
  }

  const totalMs = Date.now() - startTime
  latencies.sort((a, b) => a - b)
  wsLatencies.sort((a, b) => a - b)
  const totalBids = bidAccepted + bidRejected

  // 基础设施
  let mysqlStats = {}, redisStats = {}
  try {
    const { execSync } = await import('child_process')
    const mOut = execSync(
      "docker exec paimai-mysql mysql -uroot -prootpassword -N -e \"SELECT 'questions',VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Questions' UNION SELECT 'threads',VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Threads_connected' UNION SELECT 'slow',VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Slow_queries';\" 2>/dev/null",
      { encoding: 'utf-8', timeout: 5000 },
    )
    mOut.trim().split('\n').forEach(l => { const [k, v] = l.split('\t'); if (k) mysqlStats[k] = parseInt(v) })
    const rOut = execSync(
      "docker exec paimai-redis-master redis-cli INFO stats 2>/dev/null | grep -E 'total_commands|instantaneous_ops|rejected'",
      { encoding: 'utf-8', timeout: 5000 },
    )
    rOut.trim().split('\n').forEach(l => { const [k, v] = l.split(':'); if (k) redisStats[k] = v?.trim() })
  } catch (_) {}

  const report = {
    timestamp: new Date().toISOString(),
    config: { rooms: ROOMS, auctionsPerRoom: AUCTIONS_PER_ROOM, totalAuctions: TOTAL_AUCTIONS, users: USERS, durationSec: DURATION },
    http: {
      total: httpTotal, '2xx_rate': httpTotal > 0 ? (http2xx / httpTotal * 100).toFixed(1) + '%' : '0%',
      '4xx_rate': httpTotal > 0 ? (http4xx / httpTotal * 100).toFixed(1) + '%' : '0%',
      system_error_rate: httpTotal > 0 ? ((http5xx + httpTimeout) / httpTotal * 100).toFixed(2) + '%' : '0%',
      avgQps: Math.round(httpTotal / (totalMs / 1000)), durationMs: totalMs,
    },
    bid: {
      total: totalBids, accepted: bidAccepted, accepted_per_sec: (bidAccepted / (totalMs / 1000)).toFixed(1),
      rejected: bidRejected, business_accept_rate: totalBids > 0 ? (bidAccepted / totalBids * 100).toFixed(1) + '%' : '0%',
      system_error: bidSystemError,
      reject_breakdown: rejectByCode,
      top_reject_reasons: Object.entries(rejectReasons).sort((a, b) => b[1] - a[1]).slice(0, 10).map(([k, v]) => ({ reason: k, count: v })),
    },
    auction_distribution: {
      total_auctions: auctionStats.size,
      requests_per_auction: (() => {
        const arr = Array.from(auctionStats.values()).map(s => s.requests)
        arr.sort((a, b) => a - b)
        return { min: arr[0] || 0, p50: arr[Math.floor(arr.length * 0.5)] || 0, p95: arr[Math.floor(arr.length * 0.95)] || 0, max: arr[arr.length - 1] || 0, avg: arr.length > 0 ? Math.round(arr.reduce((a, b) => a + b, 0) / arr.length) : 0 }
      })(),
      accepted_per_auction: (() => {
        const arr = Array.from(auctionStats.values()).map(s => s.accepted)
        arr.sort((a, b) => a - b)
        return { min: arr[0] || 0, p50: arr[Math.floor(arr.length * 0.5)] || 0, p95: arr[Math.floor(arr.length * 0.95)] || 0, max: arr[arr.length - 1] || 0, avg: arr.length > 0 ? Math.round(arr.reduce((a, b) => a + b, 0) / arr.length) : 0 }
      })(),
      conflicts_per_auction: (() => {
        const arr = Array.from(auctionStats.values()).map(s => s.conflicts)
        arr.sort((a, b) => a - b)
        return { min: arr[0] || 0, p50: arr[Math.floor(arr.length * 0.5)] || 0, p95: arr[Math.floor(arr.length * 0.95)] || 0, max: arr[arr.length - 1] || 0, avg: arr.length > 0 ? Math.round(arr.reduce((a, b) => a + b, 0) / arr.length) : 0 }
      })(),
    },
    latency: {
      avg: latencies.length > 0 ? Math.round(latencies.reduce((a, b) => a + b, 0) / latencies.length) : 0,
      p50: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.5)] : 0,
      p95: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.95)] : 0,
      p99: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.99)] : 0,
      max: latencies.length > 0 ? latencies[latencies.length - 1] : 0,
    },
    websocket: {
      connected: wsConnected,
      failed: wsFailed,
      disconnected: wsDisconnected,
      connect_rate: USERS > 0 ? (wsConnected / USERS * 100).toFixed(1) + '%' : '0%',
      messages_received: wsMsgReceived,
      missing_timestamp: wsMissingTimestamp,
      broadcast_latency: {
        avg: wsLatencies.length > 0 ? Math.round(wsLatencies.reduce((a, b) => a + b, 0) / wsLatencies.length) : 0,
        p50: wsLatencies.length > 0 ? wsLatencies[Math.floor(wsLatencies.length * 0.5)] : 0,
        p95: wsLatencies.length > 0 ? wsLatencies[Math.floor(wsLatencies.length * 0.95)] : 0,
        p99: wsLatencies.length > 0 ? wsLatencies[Math.floor(wsLatencies.length * 0.99)] : 0,
        max: wsLatencies.length > 0 ? wsLatencies[wsLatencies.length - 1] : 0,
      },
    },
    infrastructure: { mysql: mysqlStats, redis: redisStats },
  }

  console.log('')
  console.log('')
  console.log(JSON.stringify(report, null, 2))

  const label = `multi-${ROOMS}r-${TOTAL_AUCTIONS}a-${USERS}u`
  const fs = await import('fs')
  fs.mkdirSync('scripts/load-test/results', { recursive: true })
  fs.writeFileSync(`scripts/load-test/results/${label}.json`, JSON.stringify(report, null, 2))
}

main().catch(err => { console.error('\n失败:', err.message); process.exit(1) })
