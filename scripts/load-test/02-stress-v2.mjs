/**
 * 02-stress-v2.mjs — 竞拍压测（精准指标版）
 *
 * 指标拆分:
 *   - HTTP 请求成功率 (2xx / 总请求)
 *   - 出价接受率 (accepted / 有效出价)
 *   - 系统错误率 (5xx + timeout / 总请求)
 *   - bidSuccess/s (每秒成功出价数)
 *   - reject reason 分布
 */

const BASE = process.env.BASE_URL || 'http://localhost:8080'

async function main() {
  const args = process.argv.slice(2)
  const setupPath = args[0]
  const isSingle = args.includes('--single')
  const isMulti = args.includes('--multi')
  const DURATION = parseInt(args[args.indexOf('--duration') + 1] || '12')

  if (!setupPath || (!isSingle && !isMulti)) {
    console.error('用法: node 02-stress-v2.mjs <setup.json> --single --users N --duration S')
    console.error('      node 02-stress-v2.mjs <setup.json> --multi --auctions N --usersPer M --duration S')
    process.exit(1)
  }

  const fs = await import('fs')
  const setup = JSON.parse(fs.readFileSync(setupPath, 'utf-8'))

  let CONCURRENCY, AUCTIONS, MODE_LABEL
  if (isSingle) {
    CONCURRENCY = parseInt(args[args.indexOf('--users') + 1] || '50')
    AUCTIONS = 1
    MODE_LABEL = `单拍卖 ${CONCURRENCY}人`
  } else {
    const auctionCount = parseInt(args[args.indexOf('--auctions') + 1] || '100')
    const usersPer = parseInt(args[args.indexOf('--usersPer') + 1] || '3')
    CONCURRENCY = auctionCount * usersPer
    AUCTIONS = auctionCount
    MODE_LABEL = `${auctionCount}拍卖 × ${usersPer}人 = ${CONCURRENCY}并发`
  }

  // 构建分配
  const assignments = []
  if (isSingle) {
    const raceId = setup.raceAuction?.id
    if (!raceId) throw new Error('缺少 raceAuction')
    for (let i = 0; i < CONCURRENCY; i++) {
      assignments.push({ auctionId: raceId, buyer: setup.allBuyers[i % setup.allBuyers.length] })
    }
  } else {
    const groups = setup.auctionGroups
    if (!groups || groups.length < AUCTIONS) throw new Error(`只有 ${groups?.length || 0} 个拍卖，需要 ${AUCTIONS}`)
    for (let a = 0; a < AUCTIONS; a++) {
      const up = CONCURRENCY / AUCTIONS
      for (let u = 0; u < up; u++) {
        assignments.push({ auctionId: groups[a].auctionId, buyer: groups[a].buyers[u % groups[a].buyers.length] })
      }
    }
  }

  // ========== 指标 ==========
  let httpTotal = 0        // 总 HTTP 请求数
  let http2xx = 0          // 2xx
  let http4xx = 0          // 4xx (业务拒绝)
  let http5xx = 0          // 5xx (系统错误)
  let httpTimeout = 0      // 超时/连接失败

  let bidAccepted = 0      // 出价成功
  let bidRejected = 0      // 出价被拒(4xx)
  let bidSystemError = 0   // 出价系统错误(5xx/timeout)

  // reject reason 分布
  const rejectReasons = {}

  const latencies = []
  const bidLatencies = []
  const startTime = Date.now()

  async function bidRound(assignment, iter) {
    const { auctionId, buyer } = assignment
    const headers = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${buyer.token}`,
    }

    // --- 1. 查询拍卖详情 ---
    let auctionRes
    try {
      auctionRes = await fetch(`${BASE}/api/auctions/${auctionId}`, { headers })
    } catch (e) {
      httpTotal++
      httpTimeout++
      return
    }
    httpTotal++

    if (auctionRes.status >= 200 && auctionRes.status < 300) {
      http2xx++
    } else if (auctionRes.status >= 500) {
      http5xx++
      return
    } else {
      http4xx++
      return
    }

    let currentPrice = 0
    let auctionStatus = ''
    try {
      const body = await auctionRes.json()
      currentPrice = body.data?.currentPriceCents || 0
      auctionStatus = body.data?.status || ''
    } catch (_) {
      return
    }

    // 拍卖已结束，跳过出价
    if (auctionStatus === 'sold' || auctionStatus === 'failed' || auctionStatus === 'cancelled') return

    // --- 2. 出价 ---
    const steps = 1 + Math.floor(Math.random() * 5)
    const bidAmount = currentPrice + steps * 100
    const idemKey = `v2-${auctionId}-${buyer.userId}-${iter}-${Date.now()}`

    let bidRes
    const tBid = Date.now()
    try {
      bidRes = await fetch(`${BASE}/api/auctions/${auctionId}/bids`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ userId: buyer.userId, amountCents: bidAmount, idempotencyKey: idemKey }),
      })
    } catch (e) {
      httpTotal++
      httpTimeout++
      bidSystemError++
      return
    }
    httpTotal++
    const bidMs = Date.now() - tBid
    bidLatencies.push(bidMs)
    latencies.push(bidMs)

    if (bidRes.status >= 200 && bidRes.status < 300) {
      http2xx++
      const body = await bidRes.json()
      if (body.data?.accepted) {
        bidAccepted++
      } else {
        bidRejected++
        const reason = body.message || 'unknown_reject'
        rejectReasons[reason] = (rejectReasons[reason] || 0) + 1
      }
    } else if (bidRes.status === 409) {
      // 409 = 业务拒绝 (价格不够/步长/已结束等)
      http4xx++
      bidRejected++
      try {
        const body = await bidRes.json()
        const reason = body.message || '409_unknown'
        rejectReasons[reason] = (rejectReasons[reason] || 0) + 1
      } catch (_) {
        rejectReasons['409_parse_error'] = (rejectReasons['409_parse_error'] || 0) + 1
      }
    } else if (bidRes.status >= 500) {
      http5xx++
      bidSystemError++
    } else {
      http4xx++
      bidRejected++
    }
  }

  async function runWorker(workerId) {
    const assignment = assignments[workerId]
    let iter = 0
    while (Date.now() - startTime < DURATION * 1000) {
      await bidRound(assignment, iter++)
      await new Promise(r => setTimeout(r, 30 + Math.random() * 70))
    }
  }

  const workers = []
  for (let i = 0; i < CONCURRENCY; i++) workers.push(runWorker(i))

  // 进度
  let lastQpsCount = 0, lastQpsTime = startTime
  const monitor = setInterval(() => {
    const now = Date.now()
    const elapsed = ((now - startTime) / 1000).toFixed(1)
    const avgQps = Math.round(httpTotal / Math.max(1, (now - startTime) / 1000))
    const intervalMs = now - lastQpsTime
    const instantQps = Math.round((httpTotal - lastQpsCount) / (intervalMs / 1000))
    lastQpsTime = now; lastQpsCount = httpTotal
    const httpOk = httpTotal > 0 ? ((http2xx / httpTotal) * 100).toFixed(1) : '0'
    const sysErr = httpTotal > 0 ? (((http5xx + httpTimeout) / httpTotal) * 100).toFixed(1) : '0'
    const totalBids = bidAccepted + bidRejected
    const accRate = totalBids > 0 ? ((bidAccepted / totalBids) * 100).toFixed(1) : '0'
    process.stderr.write(
      `\r  [${elapsed}s] HTTP:${httpTotal} QPS:${avgQps}(瞬${instantQps}) | 2xx:${httpOk}% | 5xx+TO:${sysErr}% | 出价:${bidAccepted} acc:${accRate}% | sysErr:${bidSystemError}`,
    )
  }, 1000)

  await Promise.all(workers)
  clearInterval(monitor)

  const totalMs = Date.now() - startTime
  latencies.sort((a, b) => a - b)
  bidLatencies.sort((a, b) => a - b)
  const totalBids = bidAccepted + bidRejected

  // 排序 reject reasons
  const sortedReasons = Object.entries(rejectReasons)
    .sort((a, b) => b[1] - a[1])
    .reduce((o, [k, v]) => ({ ...o, [k]: v }), {})

  const result = {
    mode: isSingle ? 'single' : 'multi',
    label: MODE_LABEL,
    config: { concurrency: CONCURRENCY, auctions: AUCTIONS, durationSec: DURATION },
    http: {
      total: httpTotal,
      '2xx': http2xx,
      '2xx_rate': httpTotal > 0 ? (http2xx / httpTotal * 100).toFixed(1) + '%' : '0%',
      '4xx': http4xx,
      '4xx_rate': httpTotal > 0 ? (http4xx / httpTotal * 100).toFixed(1) + '%' : '0%',
      '5xx': http5xx,
      '5xx_rate': httpTotal > 0 ? (http5xx / httpTotal * 100).toFixed(1) + '%' : '0%',
      timeout: httpTimeout,
      timeout_rate: httpTotal > 0 ? (httpTimeout / httpTotal * 100).toFixed(1) + '%' : '0%',
      system_error_rate: httpTotal > 0 ? ((http5xx + httpTimeout) / httpTotal * 100).toFixed(1) + '%' : '0%',
      avgQps: Math.round(httpTotal / (totalMs / 1000)),
    },
    bid: {
      total: totalBids,
      accepted: bidAccepted,
      accepted_per_sec: (bidAccepted / (totalMs / 1000)).toFixed(1),
      rejected: bidRejected,
      business_accept_rate: totalBids > 0 ? (bidAccepted / totalBids * 100).toFixed(1) + '%' : '0%',
      system_error: bidSystemError,
      reject_reasons: sortedReasons,
    },
    latency: {
      all: {
        avg: latencies.length > 0 ? Math.round(latencies.reduce((a, b) => a + b, 0) / latencies.length) : 0,
        p50: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.5)] : 0,
        p95: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.95)] : 0,
        p99: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.99)] : 0,
        max: latencies.length > 0 ? latencies[latencies.length - 1] : 0,
      },
    },
  }

  console.log('')
  console.log(JSON.stringify(result, null, 2))

  const label = isSingle ? `s-${CONCURRENCY}` : `m-${AUCTIONS}x${CONCURRENCY / AUCTIONS}`
  fs.writeFileSync(setupPath.replace('setup.json', `stress-v2-${label}.json`), JSON.stringify(result, null, 2))
}

main().catch(err => { console.error('压测失败:', err.message); process.exit(1) })
