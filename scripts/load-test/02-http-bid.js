/**
 * 02-http-bid.js — k6 HTTP 出价压测脚本
 *
 * 用法:
 *   k6 run -e SETUP_FILE=setup.json -e VUS=50 -e DURATION=30s scripts/load-test/02-http-bid.js
 *
 * 需要先运行 01-setup.mjs 生成 setup.json
 */

import http from 'k6/http'
import { check, sleep, group } from 'k6'
import { Counter, Trend, Rate } from 'k6/metrics'
import { SharedArray } from 'k6/data'

// 自定义指标
const bidCounter = new Counter('bids_placed')
const bidLatency = new Trend('bid_latency', true)
const bidErrorRate = new Rate('bid_errors')
const rankingLatency = new Trend('ranking_latency', true)
const listLatency = new Trend('list_latency', true)

// 从 setup.json 加载测试数据
const setupData = new SharedArray('setup', function () {
  return JSON.parse(open(__ENV.SETUP_FILE || 'setup.json'))
})

export const options = {
  vus: parseInt(__ENV.VUS || '50'),
  duration: __ENV.DURATION || '30s',
  thresholds: {
    'bid_latency': ['p(95)<500', 'p(99)<1000'],
    'bid_errors': ['rate<0.05'],
    'http_req_failed': ['rate<0.05'],
  },
}

export default function () {
  const data = setupData[0]
  const auctionId = data.auction.id
  const increment = data.auction.bidIncrementCents || 100

  // 每个 VU 分配一个 buyer（循环使用）
  const buyer = data.buyers[__VU % data.buyers.length]
  const token = buyer.token

  const headers = {
    'Content-Type': 'application/json',
    Authorization: `Bearer ${token}`,
  }

  group('竞拍出价链路', () => {
    // 1. 查看竞拍详情（获取当前价格）
    let res = http.get(`${data.baseUrl}/api/auctions/${auctionId}`, { headers })
    check(res, { 'auction detail ok': (r) => r.status === 200 })

    // 2. 计算出价金额
    let currentPrice = 0
    try {
      const body = JSON.parse(res.body)
      currentPrice = body.data?.currentPriceCents || 0
    } catch (_) { /* ignore */ }

    const bidAmount = currentPrice + increment + Math.floor(Math.random() * increment * 3)

    // 3. 出价
    const idempotencyKey = `${__VU}-${__ITER}-${Date.now()}-${Math.random().toString(36).slice(2)}`
    const bidPayload = JSON.stringify({
      userId: buyer.userId,
      amountCents: bidAmount,
      idempotencyKey,
    })

    const bidStart = Date.now()
    res = http.post(`${data.baseUrl}/api/auctions/${auctionId}/bids`, bidPayload, { headers })
    const bidDuration = Date.now() - bidStart
    bidLatency.add(bidDuration)
    bidCounter.add(1)

    const bidOk = check(res, {
      'bid accepted or conflict': (r) => r.status === 200 || r.status === 409,
    })

    if (!bidOk) {
      bidErrorRate.add(1)
    } else {
      bidErrorRate.add(0)
    }

    // 4. 查看排行榜
    const rankStart = Date.now()
    res = http.get(`${data.baseUrl}/api/auctions/${auctionId}/ranking?limit=10`, { headers })
    rankingLatency.add(Date.now() - rankStart)
    check(res, { 'ranking ok': (r) => r.status === 200 })

    // 5. 竞拍列表（模拟用户浏览）
    const listStart = Date.now()
    res = http.get(`${data.baseUrl}/api/rooms/${data.room.id}/auctions?status=running`, { headers })
    listLatency.add(Date.now() - listStart)
    check(res, { 'list ok': (r) => r.status === 200 })
  })

  // 随机间隔，模拟真实用户行为
  sleep(0.1 + Math.random() * 0.4)
}

export function handleSummary(data) {
  const summary = {
    timestamp: new Date().toISOString(),
    config: { vus: options.vus, duration: options.duration },
    metrics: {
      http_reqs: data.metrics.http_reqs?.values?.count || 0,
      http_req_duration: {
        avg: data.metrics.http_req_duration?.values?.avg?.toFixed(2),
        p95: data.metrics.http_req_duration?.values['p(95)']?.toFixed(2),
        p99: data.metrics.http_req_duration?.values['p(99)']?.toFixed(2),
      },
      http_req_failed: data.metrics.http_req_failed?.values?.rate,
      bids_placed: data.metrics.bids_placed?.values?.count || 0,
      bid_latency: {
        avg: data.metrics.bid_latency?.values?.avg?.toFixed(2),
        p95: data.metrics.bid_latency?.values['p(95)']?.toFixed(2),
        p99: data.metrics.bid_latency?.values['p(99)']?.toFixed(2),
      },
      bid_errors: data.metrics.bid_errors?.values?.rate,
      ranking_latency_avg: data.metrics.ranking_latency?.values?.avg?.toFixed(2),
      list_latency_avg: data.metrics.list_latency?.values?.avg?.toFixed(2),
    },
  }
  return {
    'stdout': JSON.stringify(summary, null, 2),
    'results/summary.json': JSON.stringify(summary, null, 2),
  }
}
