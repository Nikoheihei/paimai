/**
 * 02-stress.mjs — 竞拍压测（逐步加压找崩溃点）
 *
 * 用法:
 *   单拍卖模式:  node 02-stress.mjs setup.json --single --users 100 --duration 20
 *   多拍卖模式:  node 02-stress.mjs setup.json --multi  --auctions 50 --usersPer 5 --duration 20
 *
 * 出价规则: 严格按步长递增 (currentPrice + N*100)，不会因步长被拒
 */

const BASE = process.env.BASE_URL || 'http://localhost:8080'

async function main() {
  const args = process.argv.slice(2)
  const setupPath = args[0]
  const isSingle = args.includes('--single')
  const isMulti = args.includes('--multi')
  const DURATION = parseInt(args[args.indexOf('--duration') + 1] || '15')

  if (!setupPath || (!isSingle && !isMulti)) {
    console.error('用法: node 02-stress.mjs <setup.json> --single --users N --duration S')
    console.error('      node 02-stress.mjs <setup.json> --multi --auctions N --usersPer M --duration S')
    process.exit(1)
  }

  const fs = await import('fs')
  const setup = JSON.parse(fs.readFileSync(setupPath, 'utf-8'))

  let CONCURRENCY, MODE, AUCTIONS
  if (isSingle) {
    CONCURRENCY = parseInt(args[args.indexOf('--users') + 1] || '50')
    MODE = `单拍卖 ${CONCURRENCY}人抢`
    AUCTIONS = 1
  } else {
    const auctionCount = parseInt(args[args.indexOf('--auctions') + 1] || '50')
    const usersPer = parseInt(args[args.indexOf('--usersPer') + 1] || '3')
    CONCURRENCY = auctionCount * usersPer
    MODE = `${auctionCount}拍卖 × ${usersPer}人/拍卖 = ${CONCURRENCY}并发`
    AUCTIONS = auctionCount
  }

  console.log(`模式: ${MODE}`)
  console.log(`持续时间: ${DURATION}s`)
  console.log('')

  // 构建拍卖-用户映射
  const assignments = []
  if (isSingle) {
    const raceId = setup.raceAuction?.id
    if (!raceId) throw new Error('缺少 raceAuction')
    for (let i = 0; i < CONCURRENCY; i++) {
      assignments.push({
        auctionId: raceId,
        buyer: setup.allBuyers[i % setup.allBuyers.length],
      })
    }
  } else {
    const groups = setup.auctionGroups
    if (!groups || groups.length < AUCTIONS) throw new Error(`只有 ${groups?.length || 0} 个拍卖，需要 ${AUCTIONS}`)
    for (let a = 0; a < AUCTIONS; a++) {
      const usersPer = CONCURRENCY / AUCTIONS
      for (let u = 0; u < usersPer; u++) {
        assignments.push({
          auctionId: groups[a].auctionId,
          buyer: groups[a].buyers[u % groups[a].buyers.length],
        })
      }
    }
  }

  // 统计
  let totalRequests = 0
  let bidSuccess = 0
  let bidRejected = 0
  let errorCount = 0
  let maxQps = 0
  let minQps = Infinity
  const latencies = []
  const startTime = Date.now()
  let lastQpsTime = startTime
  let lastQpsCount = 0

  async function bidRound(assignment, iter) {
    const { auctionId, buyer } = assignment
    const headers = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${buyer.token}`,
    }

    try {
      // 获取当前价格
      const auctionRes = await fetch(`${BASE}/api/auctions/${auctionId}`, { headers })
      totalRequests++
      let currentPrice = 0
      let auctionStatus = ''
      try {
        const body = await auctionRes.json()
        currentPrice = body.data?.currentPriceCents || 0
        auctionStatus = body.data?.status || ''
      } catch (_) { /* ignore */ }

      if (auctionRes.status !== 200) { errorCount++; return }
      if (auctionStatus === 'sold') { return } // 已成交，跳过

      // 出价（严格按步长：+100 到 +500）
      const steps = 1 + Math.floor(Math.random() * 5)
      const bidAmount = currentPrice + steps * 100
      const idemKey = `s-${auctionId}-${buyer.userId}-${iter}-${Date.now()}`
      const tBid = Date.now()
      const bidRes = await fetch(`${BASE}/api/auctions/${auctionId}/bids`, {
        method: 'POST',
        headers,
        body: JSON.stringify({ userId: buyer.userId, amountCents: bidAmount, idempotencyKey: idemKey }),
      })
      totalRequests++
      const bidMs = Date.now() - tBid
      latencies.push(bidMs)

      if (bidRes.status === 200) {
        const body = await bidRes.json()
        if (body.data?.accepted) bidSuccess++
        else bidRejected++
      } else if (bidRes.status === 409) {
        bidRejected++
      } else {
        errorCount++
      }
    } catch (e) {
      errorCount++
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

  // 启动
  const workers = []
  for (let i = 0; i < CONCURRENCY; i++) {
    workers.push(runWorker(i))
  }

  // QPS 监控
  const monitorInterval = setInterval(() => {
    const now = Date.now()
    const intervalMs = now - lastQpsTime
    const intervalCount = totalRequests - lastQpsCount
    const instantQps = Math.round(intervalCount / (intervalMs / 1000))
    if (instantQps > maxQps) maxQps = instantQps
    if (instantQps < minQps && totalRequests > 100) minQps = instantQps
    lastQpsTime = now
    lastQpsCount = totalRequests

    const elapsed = ((now - startTime) / 1000).toFixed(1)
    const totalBids = bidSuccess + bidRejected
    const acceptRate = totalBids > 0 ? (bidSuccess / totalBids * 100).toFixed(1) : '0'
    const avgQps = Math.round(totalRequests / Math.max(1, (now - startTime) / 1000))
    process.stderr.write(
      `\r  [${elapsed}s] 总请求:${totalRequests} QPS:${avgQps}(瞬:${instantQps}) 成功:${bidSuccess} 拒:${bidRejected} 通过率:${acceptRate}% 错:${errorCount}`,
    )
  }, 1000)

  await Promise.all(workers)
  clearInterval(monitorInterval)

  const totalMs = Date.now() - startTime
  latencies.sort((a, b) => a - b)
  const totalBids = bidSuccess + bidRejected

  // 汇总
  const result = {
    mode: isSingle ? 'single' : 'multi',
    config: { concurrency: CONCURRENCY, auctions: AUCTIONS, durationSec: DURATION },
    summary: {
      totalRequests,
      bidSuccess,
      bidRejected,
      acceptRate: totalBids > 0 ? (bidSuccess / totalBids * 100).toFixed(1) + '%' : '0%',
      errorCount,
      errorRate: totalRequests > 0 ? (errorCount / totalRequests * 100).toFixed(2) + '%' : '0%',
      avgQps: Math.round(totalRequests / (totalMs / 1000)),
      peakQps: maxQps,
      minQps: minQps === Infinity ? 0 : minQps,
      durationMs: totalMs,
    },
    latency: {
      avg: latencies.length > 0 ? Math.round(latencies.reduce((a, b) => a + b, 0) / latencies.length) : 0,
      p50: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.5)] : 0,
      p95: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.95)] : 0,
      p99: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.99)] : 0,
      max: latencies.length > 0 ? latencies[latencies.length - 1] : 0,
    },
  }

  console.log('')
  console.log('')
  console.log(JSON.stringify(result, null, 2))

  const label = isSingle ? `single-${CONCURRENCY}` : `multi-${AUCTIONS}x${CONCURRENCY / AUCTIONS}`
  const resultPath = setupPath.replace('setup.json', `stress-${label}.json`)
  fs.writeFileSync(resultPath, JSON.stringify(result, null, 2))
}

main().catch(err => { console.error('压测失败:', err.message); process.exit(1) })
