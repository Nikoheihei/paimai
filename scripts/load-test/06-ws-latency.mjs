/**
 * 06-ws-latency.mjs — WebSocket 端到端延迟压测
 *
 * 用法:
 *   node scripts/load-test/06-ws-latency.mjs <rooms> <auctionsPerRoom> <users> <durationSec>
 *
 * 功能:
 *   1. 建立指定数量的 WS 连接（每个用户连接一个随机房间）
 *   2. 统计基于 serverSentAt 的广播延迟 P50/P95/P99/max
 *   3. 同时执行 HTTP 出价压测（可选，通过 --no-bid 禁用）
 */

import WebSocket from 'ws'

const BASE_HTTP = process.env.BASE_URL || 'http://localhost:8080'
const BASE_WS = process.env.WS_URL || 'ws://localhost:8080'

function uid() { return `${Math.random().toString(36).slice(2, 8)}` }
function randInt(min, max) { return Math.floor(Math.random() * (max - min + 1)) + min }

async function apiJson(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...options.headers }
  const res = await fetch(`${BASE_HTTP}${path}`, { ...options, headers })
  const body = await res.json()
  if (body.code !== 0) throw new Error(`${path}: ${body.message || `HTTP ${res.status}`}`)
  return body.data
}

async function apiAuth(path, token, options = {}) {
  const headers = { 'Content-Type': 'application/json', Authorization: `Bearer ${token}`, ...options.headers }
  const res = await fetch(`${BASE_HTTP}${path}`, { ...options, headers })
  const body = await res.json()
  if (body.code !== 0) throw new Error(`${path}: ${body.message || `HTTP ${res.status}`}`)
  return body.data
}

async function main() {
  const ROOMS = parseInt(process.argv[2] || '1')
  const AUCTIONS_PER_ROOM = parseInt(process.argv[3] || '1')
  const USERS = parseInt(process.argv[4] || '500')
  const DURATION = parseInt(process.argv[5] || '20')
  const NO_BID = process.argv.includes('--no-bid')

  const TOTAL_AUCTIONS = ROOMS * AUCTIONS_PER_ROOM

  console.log('')
  console.log(`╔══════════════════════════════════════════════════════╗`)
  console.log(`║  WS 端到端延迟压测                                      ║`)
  console.log(`║  房间:${ROOMS}  拍卖/房间:${AUCTIONS_PER_ROOM}  总拍卖:${TOTAL_AUCTIONS}  用户:${USERS}  时长:${DURATION}s  ║`)
  console.log(`╚══════════════════════════════════════════════════════╝`)
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

  // ====== 阶段 2: 注册 seller + 所有用户 ======
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
        body: JSON.stringify({
          username: `u${i + j}${uid()}`,
          password: 'Test123456',
          nickname: `用户${i + j}`,
          role: 'buyer',
        }),
      }))
    }
    const results = await Promise.all(batch)
    results.forEach(r => users.push({ userId: r.userId, token: r.token }))
    if (i % 100 === 0) process.stderr.write(`\r  已注册 ${Math.min(i + batchSize, USERS)}/${USERS}`)
  }
  console.log(`\r  完成: ${USERS} 用户`)

  // ====== 阶段 3: 创建房间和拍卖 ======
  console.log(`[3/5] 创建 ${ROOMS} 房间 × ${AUCTIONS_PER_ROOM} 拍卖...`)
  const now = new Date()
  const endAt = new Date(now.getTime() + 7200_000)
  const rooms = []
  const auctions = []

  for (let r = 0; r < ROOMS; r++) {
    const room = await apiAuth('/api/admin/rooms', sellerToken, {
      method: 'POST',
      body: JSON.stringify({ title: `房间${r + 1}`, coverUrl: '' }),
    })
    await apiAuth(`/api/admin/rooms/${room.id}/live`, sellerToken, { method: 'POST' })
    rooms.push({ roomId: room.id, token: sellerToken })

    const product = await apiAuth('/api/admin/products', sellerToken, {
      method: 'POST',
      body: JSON.stringify({ name: `商品R${r + 1}`, description: 'test', imageUrl: '' }),
    })

    for (let a = 0; a < AUCTIONS_PER_ROOM; a++) {
      const auction = await apiAuth('/api/admin/auctions', sellerToken, {
        method: 'POST',
        body: JSON.stringify({
          roomId: room.id,
          productId: product.id,
          mode: 'extension',
          startPriceCents: randInt(0, 5000),
          bidIncrementCents: 100,
          capPriceCents: 9999999,
          reservePriceCents: 0,
          startAt: now.toISOString(),
          endAt: endAt.toISOString(),
          extendThresholdSec: 30,
          extendDurationSec: 30,
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
  console.log(`[4/5] 建立 ${USERS} 个 WS 连接...`)
  const wsLatencies = []      // 所有 WS 消息延迟样本
  const wsMsgsPerClient = []  // 每个 client 收到的消息数
  let wsConnected = 0
  let wsErrors = 0

  await new Promise((resolve) => {
    let resolved = false
    const check = () => {
      if (wsConnected + wsErrors >= USERS && !resolved) {
        resolved = true
        resolve()
      }
    }

    for (let i = 0; i < USERS; i++) {
      const room = rooms[i % rooms.length]
      const user = users[i]
      const wsUrl = `${BASE_WS}/api/rooms/${room.roomId}/ws?token=${user.token}`
      const ws = new WebSocket(wsUrl)
      let msgCount = 0

      ws.on('open', () => {
        wsConnected++
        check()
      })

      ws.on('message', (data) => {
        try {
          const msg = JSON.parse(data.toString())
          if (msg.serverSentAt) {
            const delay = Date.now() - msg.serverSentAt
            if (delay >= 0 && delay < 60000) { // 过滤异常值
              wsLatencies.push(delay)
              msgCount++
            }
          }
        } catch (_) {}
      })

      ws.on('error', (err) => {
        wsErrors++
        check()
      })

      ws.on('close', () => {
        wsMsgsPerClient.push(msgCount)
      })

      // 超时兜底
      setTimeout(() => {
        if (ws.readyState !== WebSocket.OPEN) {
          wsErrors++
          check()
        }
      }, 5000)
    }
  })
  console.log(`  完成: 成功 ${wsConnected}, 失败 ${wsErrors}`)

  // ====== 阶段 5: 压测 ======
  console.log(`[5/5] 压测 ${DURATION}s...`)
  console.log('')

  let httpTotal = 0, http2xx = 0, http4xx = 0, http5xx = 0, httpTimeout = 0
  let bidAccepted = 0, bidRejected = 0, bidSystemError = 0
  const rejectReasons = {}
  const httpLatencies = []
  const startTime = Date.now()

  async function bidRound(user, iter) {
    const a = auctions[randInt(0, auctions.length - 1)]
    const auctionId = a.auctionId
    const headers = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${user.token}`,
    }

    let res
    try { res = await fetch(`${BASE_HTTP}/api/auctions/${auctionId}`, { headers }) }
    catch (_) { httpTotal++; httpTimeout++; return }
    httpTotal++
    if (res.status >= 200 && res.status < 300) http2xx++
    else if (res.status >= 500) { http5xx++; return }
    else { http4xx++; return }

    let cp = 0, status = ''
    try { const b = await res.json(); cp = b.data?.currentPriceCents || 0; status = b.data?.status || '' } catch (_) { return }
    if (status !== 'running') return

    const steps = 1 + Math.floor(Math.random() * 5)
    const amount = cp + steps * 100
    const idemKey = `ws-${auctionId}-${user.userId}-${iter}-${Date.now()}`
    const tBid = Date.now()
    try {
      res = await fetch(`${BASE_HTTP}/api/auctions/${auctionId}/bids`, {
        method: 'POST', headers,
        body: JSON.stringify({ userId: user.userId, amountCents: amount, idempotencyKey: idemKey }),
      })
    } catch (_) { httpTotal++; httpTimeout++; bidSystemError++; return }
    httpTotal++
    const ms = Date.now() - tBid
    httpLatencies.push(ms)

    if (res.status >= 200 && res.status < 300) {
      http2xx++
      const body = await res.json()
      if (body.data?.accepted) bidAccepted++
      else { bidRejected++; const r = body.message || '?'; rejectReasons[r] = (rejectReasons[r] || 0) + 1 }
    } else if (res.status === 409) {
      http4xx++; bidRejected++
      try { const b = await res.json(); const r = b.message || '409'; rejectReasons[r] = (rejectReasons[r] || 0) + 1 } catch (_) {}
    } else if (res.status >= 500) {
      http5xx++; bidSystemError++
    } else { http4xx++; bidRejected++ }
  }

  async function worker(i) {
    const user = users[i]
    let iter = 0
    while (Date.now() - startTime < DURATION * 1000) {
      if (!NO_BID) await bidRound(user, iter++)
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
    const wsCount = wsLatencies.length
    const wsAvg = wsCount > 0 ? Math.round(wsLatencies.reduce((a, b) => a + b, 0) / wsCount) : 0
    process.stderr.write(
      `\r  [${elapsed}s] HTTP:${httpTotal} QPS:${avgQps}(瞬${instantQps}) | 出价成功:${bidAccepted} acc:${ar}% | WS消息:${wsCount} 延迟avg:${wsAvg}ms`,
    )
  }, 1000)

  await Promise.all(workers)
  clearInterval(monitor)

  const totalMs = Date.now() - startTime
  httpLatencies.sort((a, b) => a - b)
  wsLatencies.sort((a, b) => a - b)
  const totalBids = bidAccepted + bidRejected

  // 基础设施指标
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

  // WS 服务端指标
  let wsStats = {}
  try {
    const res = await fetch(`${BASE_HTTP}/api/ws-stats`)
    const body = await res.json()
    if (body.code === 0) wsStats = body.data
  } catch (_) {}

  const report = {
    timestamp: new Date().toISOString(),
    config: { rooms: ROOMS, auctionsPerRoom: AUCTIONS_PER_ROOM, totalAuctions: TOTAL_AUCTIONS, users: USERS, durationSec: DURATION },
    http: {
      total: httpTotal,
      '2xx_rate': httpTotal > 0 ? (http2xx / httpTotal * 100).toFixed(1) + '%' : '0%',
      '4xx_rate': httpTotal > 0 ? (http4xx / httpTotal * 100).toFixed(1) + '%' : '0%',
      system_error_rate: httpTotal > 0 ? ((http5xx + httpTimeout) / httpTotal * 100).toFixed(2) + '%' : '0%',
      avgQps: Math.round(httpTotal / (totalMs / 1000)),
      durationMs: totalMs,
    },
    bid: {
      total: totalBids,
      accepted: bidAccepted,
      accepted_per_sec: (bidAccepted / (totalMs / 1000)).toFixed(1),
      rejected: bidRejected,
      business_accept_rate: totalBids > 0 ? (bidAccepted / totalBids * 100).toFixed(1) + '%' : '0%',
      system_error: bidSystemError,
      top_reject_reasons: Object.entries(rejectReasons).sort((a, b) => b[1] - a[1]).slice(0, 10).map(([k, v]) => ({ reason: k, count: v })),
    },
    http_latency: {
      avg: httpLatencies.length > 0 ? Math.round(httpLatencies.reduce((a, b) => a + b, 0) / httpLatencies.length) : 0,
      p50: httpLatencies.length > 0 ? httpLatencies[Math.floor(httpLatencies.length * 0.5)] : 0,
      p95: httpLatencies.length > 0 ? httpLatencies[Math.floor(httpLatencies.length * 0.95)] : 0,
      p99: httpLatencies.length > 0 ? httpLatencies[Math.floor(httpLatencies.length * 0.99)] : 0,
      max: httpLatencies.length > 0 ? httpLatencies[httpLatencies.length - 1] : 0,
    },
    ws: {
      connected: wsConnected,
      errors: wsErrors,
      total_messages: wsLatencies.length,
      msgs_per_client_avg: wsMsgsPerClient.length > 0 ? Math.round(wsMsgsPerClient.reduce((a, b) => a + b, 0) / wsMsgsPerClient.length) : 0,
      msgs_per_client_min: wsMsgsPerClient.length > 0 ? Math.min(...wsMsgsPerClient) : 0,
      msgs_per_client_max: wsMsgsPerClient.length > 0 ? Math.max(...wsMsgsPerClient) : 0,
      latency: {
        avg: wsLatencies.length > 0 ? Math.round(wsLatencies.reduce((a, b) => a + b, 0) / wsLatencies.length) : 0,
        p50: wsLatencies.length > 0 ? wsLatencies[Math.floor(wsLatencies.length * 0.5)] : 0,
        p95: wsLatencies.length > 0 ? wsLatencies[Math.floor(wsLatencies.length * 0.95)] : 0,
        p99: wsLatencies.length > 0 ? wsLatencies[Math.floor(wsLatencies.length * 0.99)] : 0,
        max: wsLatencies.length > 0 ? wsLatencies[wsLatencies.length - 1] : 0,
      },
    },
    ws_server: wsStats,
    infrastructure: { mysql: mysqlStats, redis: redisStats },
  }

  console.log('')
  console.log('')
  console.log(JSON.stringify(report, null, 2))

  const label = `ws-latency-${ROOMS}r-${TOTAL_AUCTIONS}a-${USERS}u`
  const fs = await import('fs')
  fs.mkdirSync('scripts/load-test/results', { recursive: true })
  fs.writeFileSync(`scripts/load-test/results/${label}.json`, JSON.stringify(report, null, 2))
}

main().catch(err => { console.error('\n失败:', err.message); process.exit(1) })
