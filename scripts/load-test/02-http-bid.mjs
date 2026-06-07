/**
 * 02-http-bid.mjs — HTTP 出价压测（多拍卖模式）
 *
 * 用法:
 *   node scripts/load-test/02-http-bid.mjs <setup.json> [并发数] [持续时间秒] [--race]
 *
 * 默认: 每个并发分配到独立拍卖，出价不冲突 → 测真实 QPS
 * --race: 所有并发抢同一个竞争拍卖 → 测一致性
 */

const BASE = process.env.BASE_URL || 'http://localhost:8080'

async function main() {
  const setupPath = process.argv[2]
  const CONCURRENCY = parseInt(process.argv[3] || '50')
  const DURATION_SEC = parseInt(process.argv[4] || '20')
  const isRace = process.argv.includes('--race')

  if (!setupPath) {
    console.error('用法: node 02-http-bid.mjs <setup.json> [并发数] [持续时间秒] [--race]')
    process.exit(1)
  }

  const fs = await import('fs')
  const setup = JSON.parse(fs.readFileSync(setupPath, 'utf-8'))

  const roomId = setup.room.id
  const increment = 100 // 固定 1 元加价

  // 选择拍卖模式
  let auctionAssignments
  if (isRace) {
    // 竞争模式：所有人抢同一个
    const raceId = setup.raceAuction?.id
    if (!raceId) throw new Error('setup.json 缺少 raceAuction')
    auctionAssignments = Array.from({ length: CONCURRENCY }, (_, i) => ({
      auctionId: raceId,
      buyer: setup.allBuyers[i % setup.allBuyers.length],
    }))
    console.log(`竞争模式: ${CONCURRENCY} 人抢 auctionId=${raceId}`)
  } else {
    // 独立模式：每人一个拍卖
    const groups = setup.auctionGroups
    if (!groups || groups.length === 0) throw new Error('setup.json 缺少 auctionGroups')
    auctionAssignments = Array.from({ length: CONCURRENCY }, (_, i) => {
      const group = groups[i % groups.length]
      return {
        auctionId: group.auctionId,
        buyer: group.buyers[i % group.buyers.length],
      }
    })
    console.log(`独立模式: ${CONCURRENCY} 并发, ${groups.length} 个拍卖, 出价不冲突`)
  }

  console.log(`持续时间: ${DURATION_SEC}s`)
  console.log('')

  // 统计
  let totalRequests = 0
  let bidSuccess = 0
  let bidRejected = 0
  let errorCount = 0
  const latencies = []
  let startTime = Date.now()

  async function bidRound(assignment, iter) {
    const { auctionId, buyer } = assignment
    const headers = {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${buyer.token}`,
    }

    try {
      // 1. 获取当前价格
      const t0 = Date.now()
      const auctionRes = await fetch(`${BASE}/api/auctions/${auctionId}`, { headers })
      totalRequests++
      let currentPrice = 0
      try {
        const body = await auctionRes.json()
        currentPrice = body.data?.currentPriceCents || 0
      } catch (_) { /* ignore */ }

      if (auctionRes.status !== 200) { errorCount++; return }

      // 2. 出价（必须符合步长：currentPrice + N*increment，且大于当前价）
      const steps = 1 + Math.floor(Math.random() * 5) // 1~5 倍步长
      const bidAmount = currentPrice + steps * increment
      const idemKey = `load-${auctionId}-${buyer.userId}-${iter}-${Date.now()}`
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

      // 3. 排行榜（抽样，每 10 轮一次）
      if (iter % 10 === 0) {
        await fetch(`${BASE}/api/auctions/${auctionId}/ranking?limit=5`, { headers })
        totalRequests++
      }
    } catch (e) {
      errorCount++
    }
  }

  // 并发 worker
  async function runWorker(workerId) {
    const assignment = auctionAssignments[workerId]
    let iter = 0
    while (Date.now() - startTime < DURATION_SEC * 1000) {
      await bidRound(assignment, iter++)
      await new Promise(r => setTimeout(r, 50 + Math.random() * 100))
    }
  }

  const workers = []
  for (let i = 0; i < CONCURRENCY; i++) {
    workers.push(runWorker(i))
  }

  // 进度
  const progressInterval = setInterval(() => {
    const elapsed = ((Date.now() - startTime) / 1000).toFixed(1)
    const qps = (totalRequests / Math.max(1, (Date.now() - startTime) / 1000)).toFixed(1)
    const totalBids = bidSuccess + bidRejected
    const acceptRate = totalBids > 0 ? ((bidSuccess / totalBids) * 100).toFixed(1) : '0'
    process.stderr.write(
      `\r  [${elapsed}s] 请求: ${totalRequests} | QPS: ${qps} | 出价成功: ${bidSuccess} | 被拒: ${bidRejected} | 通过率: ${acceptRate}% | 错误: ${errorCount}`,
    )
  }, 1000)

  await Promise.all(workers)
  clearInterval(progressInterval)

  const totalMs = Date.now() - startTime
  latencies.sort((a, b) => a - b)
  const totalBids = bidSuccess + bidRejected

  const result = {
    mode: isRace ? 'race' : 'independent',
    config: { concurrency: CONCURRENCY, durationSec: DURATION_SEC, auctionsUsed: new Set(auctionAssignments.map(a => a.auctionId)).size },
    summary: {
      totalRequests,
      bidSuccess,
      bidRejected,
      totalBids,
      acceptRate: totalBids > 0 ? (bidSuccess / totalBids * 100).toFixed(1) + '%' : '0%',
      errorCount,
      errorRate: totalRequests > 0 ? (errorCount / totalRequests * 100).toFixed(2) + '%' : '0%',
      avgQps: (totalRequests / (totalMs / 1000)).toFixed(1),
      durationMs: totalMs,
    },
    latency: {
      avg: latencies.length > 0 ? latencies.reduce((a, b) => a + b, 0) / latencies.length : 0,
      p50: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.5)] : 0,
      p95: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.95)] : 0,
      p99: latencies.length > 0 ? latencies[Math.floor(latencies.length * 0.99)] : 0,
      min: latencies.length > 0 ? latencies[0] : 0,
      max: latencies.length > 0 ? latencies[latencies.length - 1] : 0,
    },
  }

  console.log('')
  console.log('')
  console.log(JSON.stringify(result, null, 2))

  const suffix = isRace ? 'race' : `indie-${CONCURRENCY}`
  const resultPath = setupPath.replace('setup.json', `http-${suffix}.json`)
  fs.writeFileSync(resultPath, JSON.stringify(result, null, 2))
  console.log(`结果: ${resultPath}`)
}

main().catch(err => {
  console.error('压测失败:', err.message)
  process.exit(1)
})
