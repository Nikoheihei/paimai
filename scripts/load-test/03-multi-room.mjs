/**
 * 03-multi-room.mjs — 多房间多商品多拍卖压测脚本
 *
 * 用法:
 *   node scripts/load-test/03-multi-room.mjs <rooms> <auctionsPerRoom> <users> <durationSec>
 *
 * 策略:
 *   1. 先清理 + 注册所有用户（耗时最长）
 *   2. 再快速创建拍卖（从创建到压测开始间隔短，避免拍卖过期）
 *   3. 用户随机选拍卖出价
 */

const BASE = process.env.BASE_URL || 'http://localhost:8080'

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
  console.log(`╔══════════════════════════════════════════════════════╗`)
  console.log(`║  多房间多拍卖压测                                      ║`)
  console.log(`║  房间:${ROOMS}  拍卖/房间:${AUCTIONS_PER_ROOM}  总拍卖:${TOTAL_AUCTIONS}  用户:${USERS}  时长:${DURATION}s  ║`)
  console.log(`╚══════════════════════════════════════════════════════╝`)
  console.log('')

  // ====== 阶段 1: 清理 ======
  console.log('[1/4] 清理...')
  try {
    const { execSync } = await import('child_process')
    execSync('docker exec paimai-redis-master redis-cli FLUSHDB 2>/dev/null', { timeout: 5000 })
    for (const t of ['bids', 'orders', 'outbox_events', 'auctions', 'products', 'live_rooms']) {
      execSync(`docker exec paimai-mysql mysql -uroot -prootpassword paimai -e "DELETE FROM ${t}" 2>/dev/null`, { timeout: 5000 })
    }
    console.log('  OK')
  } catch (_) { console.log('  跳过') }

  // ====== 阶段 2: 注册 seller + 所有用户（最耗时） ======
  console.log(`[2/4] 注册 1 seller + ${USERS} 用户...`)
  const seller = await apiJson('/api/auth/register', {
    method: 'POST',
    body: JSON.stringify({ username: `slr${uid()}`, password: 'Test123456', nickname: '卖家', role: 'seller' }),
  })
  const sellerToken = seller.token

  const users = []
  // 并发注册（每次 20 个并发）
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

  // ====== 阶段 3: 快速创建拍卖（在用户注册完成后） ======
  console.log(`[3/4] 创建 ${ROOMS} 房间 × ${AUCTIONS_PER_ROOM} 拍卖...`)
  const now = new Date()
  const endAt = new Date(now.getTime() + 7200_000)
  const auctions = []

  // 批量并发创建
  for (let r = 0; r < ROOMS; r++) {
    const room = await apiAuth('/api/admin/rooms', sellerToken, {
      method: 'POST',
      body: JSON.stringify({ title: `房间${r + 1}`, coverUrl: '' }),
    })
    await apiAuth(`/api/admin/rooms/${room.id}/live`, sellerToken, { method: 'POST' })

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

  // ====== 阶段 4: 压测 ======
  console.log(`[4/4] 压测 ${USERS} 用户 ${DURATION}s...`)
  console.log('')

  let httpTotal = 0, http2xx = 0, http4xx = 0, http5xx = 0, httpTimeout = 0
  let bidAccepted = 0, bidRejected = 0, bidSystemError = 0
  const rejectReasons = {}
  const latencies = []
  const startTime = Date.now()

  async function bidRound(user, iter) {
    const a = auctions[randInt(0, auctions.length - 1)]
    const auctionId = a.auctionId
    const headers = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${user.token}`,
    }

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
      await bidRound(user, iter++)
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
    process.stderr.write(
      `\r  [${elapsed}s] HTTP:${httpTotal} QPS:${avgQps}(瞬${instantQps}) | 2xx:${httpTotal>0?(http2xx/httpTotal*100).toFixed(1):'0'}% | 5xx+TO:${httpTotal>0?((http5xx+httpTimeout)/httpTotal*100).toFixed(1):'0'}% | 出价成功:${bidAccepted} acc:${ar}%`,
    )
  }, 1000)

  await Promise.all(workers)
  clearInterval(monitor)

  const totalMs = Date.now() - startTime
  latencies.sort((a, b) => a - b)
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
      top_reject_reasons: Object.entries(rejectReasons).sort((a, b) => b[1] - a[1]).slice(0, 10).map(([k, v]) => ({ reason: k, count: v })),
    },
    latency: {
      avg: latencies.length > 0 ? Math.round(latencies.reduce((a, b) => a + b, 0) / latencies.length) : 0,
      p50: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.5)] : 0,
      p95: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.95)] : 0,
      p99: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.99)] : 0,
      max: latencies.length > 0 ? latencies[latencies.length - 1] : 0,
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
