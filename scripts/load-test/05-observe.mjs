/**
 * 05-observe.mjs — 压测观测与报告脚本
 *
 * 用法:
 *   node scripts/load-test/05-observe.mjs setup.json [results/http-50.json]
 *
 * 功能:
 * 1. 采集服务端指标（拍卖状态、bid 数量、订单数）
 * 2. 采集数据库指标（连接数、慢查询）
 * 3. 汇总 k6 输出结果
 * 4. 输出可读的压测报告
 *
 * 依赖: MySQL/Redis 需要可通过 localhost 访问
 */

import { execSync } from 'child_process'

const BASE = process.env.BASE_URL || 'http://localhost:8080'

async function api(path, options = {}) {
  const headers = { 'Content-Type': 'application/json', ...options.headers }
  const res = await fetch(`${BASE}${path}`, { ...options, headers })
  const body = await res.json()
  return body
}

async function main() {
  const setupPath = process.argv[2] || 'scripts/load-test/results/setup.json'
  const k6ResultPath = process.argv[3]

  const fs = await import('fs')
  const setup = JSON.parse(fs.readFileSync(setupPath, 'utf-8'))

  console.log('')
  console.log('╔══════════════════════════════════════╗')
  console.log('║       竞拍系统压测观测报告           ║')
  console.log('╚══════════════════════════════════════╝')
  console.log(`  时间: ${new Date().toISOString()}`)
  console.log(`  服务: ${BASE}`)
  console.log(`  Buyer 数量: ${setup.buyerCount}`)
  console.log('')

  // ==========================================
  // 1. 拍卖状态
  // ==========================================
  console.log('━━━ 拍卖状态 ━━━')
  try {
    const auction = await api(`/api/auctions/${setup.auction.id}`)
    if (auction.code === 0) {
      const a = auction.data
      console.log(`  状态:       ${a.status}`)
      console.log(`  当前价格:   ${(a.currentPriceCents / 100).toFixed(2)} 元`)
      console.log(`  起拍价:     ${(a.startPriceCents / 100).toFixed(2)} 元`)
      console.log(`  赢家:       ${a.winnerUserId || '无'}`)
      console.log(`  版本号:     ${a.version || 0}`)
    }
  } catch (e) {
    console.log(`  [错误] ${e.message}`)
  }

  // ==========================================
  // 2. 出价统计（通过 admin API）
  // ==========================================
  console.log('')
  console.log('━━━ 出价统计 ━━━')
  try {
    const sellerToken = setup.seller.token
    const bidsRes = await fetch(`${BASE}/api/admin/auctions/${setup.auction.id}/bids`, {
      headers: { Authorization: `Bearer ${sellerToken}` },
    })
    const bidsBody = await bidsRes.json()
    if (bidsBody.code === 0 && Array.isArray(bidsBody.data)) {
      const bids = bidsBody.data
      const accepted = bids.filter(b => b.accepted)
      const rejected = bids.filter(b => !b.accepted)
      console.log(`  总出价数:   ${bids.length}`)
      console.log(`  成功:       ${accepted.length}`)
      console.log(`  被拒:       ${rejected.length}`)

      if (accepted.length > 0) {
        const maxBid = Math.max(...accepted.map(b => b.amountCents))
        const minBid = Math.min(...accepted.map(b => b.amountCents))
        console.log(`  最高出价:   ${(maxBid / 100).toFixed(2)} 元`)
        console.log(`  最低出价:   ${(minBid / 100).toFixed(2)} 元`)
      }

      // 检查价格单调性
      const sorted = accepted.sort((a, b) => a.id - b.id)
      let monotonic = true
      let lastPrice = 0
      for (const b of sorted) {
        if (b.amountCents < lastPrice) {
          monotonic = false
          break
        }
        lastPrice = b.amountCents
      }
      console.log(`  价格单调:   ${monotonic ? 'PASS' : 'FAIL'}`)

      // 唯一 winner 检查
      if (accepted.length > 0) {
        const users = new Set(accepted.map(b => b.userId))
        console.log(`  参与用户:   ${users.size}`)
      }
    }
  } catch (e) {
    console.log(`  [错误] ${e.message}`)
  }

  // ==========================================
  // 3. 排行榜
  // ==========================================
  console.log('')
  console.log('━━━ 排行榜 ━━━')
  try {
    const ranking = await api(`/api/auctions/${setup.auction.id}/ranking?limit=10`)
    if (ranking.code === 0 && ranking.data) {
      ranking.data.slice(0, 5).forEach(r => {
        console.log(`  #${r.rank}  userId=${r.userId}  ${(r.amountCents / 100).toFixed(2)} 元`)
      })
    }
  } catch (e) {
    console.log(`  [错误] ${e.message}`)
  }

  // ==========================================
  // 4. 订单统计
  // ==========================================
  console.log('')
  console.log('━━━ 订单统计 ━━━')
  try {
    const sellerToken = setup.seller.token
    const ordersRes = await fetch(`${BASE}/api/admin/orders`, {
      headers: { Authorization: `Bearer ${sellerToken}` },
    })
    const ordersBody = await ordersRes.json()
    if (ordersBody.code === 0 && Array.isArray(ordersBody.data)) {
      const orders = ordersBody.data
      console.log(`  总订单数:   ${orders.length}`)
      const byStatus = {}
      orders.forEach(o => { byStatus[o.status] = (byStatus[o.status] || 0) + 1 })
      Object.entries(byStatus).forEach(([s, c]) => {
        console.log(`    ${s}: ${c}`)
      })
    }
  } catch (e) {
    console.log(`  [错误] ${e.message}`)
  }

  // ==========================================
  // 5. MySQL 慢查询（需要数据库连接）
  // ==========================================
  console.log('')
  console.log('━━━ 数据库指标 ━━━')
  try {
    // 尝试连接 MySQL 查看慢查询
    const mysqlOutput = execSync(
      `docker exec paimai-mysql mysql -uroot -prootpassword -e "SHOW GLOBAL STATUS LIKE 'Slow_queries'; SHOW GLOBAL STATUS LIKE 'Threads_connected'; SHOW GLOBAL STATUS LIKE 'Questions';" 2>/dev/null || echo "MySQL 不可达"`,
      { encoding: 'utf-8', timeout: 5000 },
    )
    console.log(mysqlOutput.trim().split('\n').map(l => `  ${l}`).join('\n'))
  } catch (e) {
    console.log(`  MySQL 不可达（容器可能未运行）`)
  }

  // ==========================================
  // 6. Redis 指标
  // ==========================================
  console.log('')
  console.log('━━━ Redis 指标 ━━━')
  try {
    const redisOutput = execSync(
      `docker exec paimai-redis-master redis-cli INFO stats 2>/dev/null | grep -E "total_commands|instantaneous_ops|rejected_connections|expired_keys" || echo "Redis 不可达"`,
      { encoding: 'utf-8', timeout: 5000 },
    )
    console.log(redisOutput.trim().split('\n').map(l => `  ${l}`).join('\n'))
  } catch (e) {
    console.log(`  Redis 不可达（容器可能未运行）`)
  }

  // ==========================================
  // 7. k6 结果汇总
  // ==========================================
  if (k6ResultPath) {
    console.log('')
    console.log('━━━ k6 压测结果 ━━━')
    try {
      const k6Result = JSON.parse(fs.readFileSync(k6ResultPath, 'utf-8'))
      const m = k6Result.metrics
      console.log(`  HTTP 请求数:      ${m.http_reqs?.count || '-'}`)
      console.log(`  请求失败率:       ${m.http_req_failed ? (m.http_req_failed.rate * 100).toFixed(2) + '%' : '-'}`)
      console.log(`  平均延迟:         ${m.http_req_duration?.avg ? m.http_req_duration.avg.toFixed(2) + 'ms' : '-'}`)
      console.log(`  P95 延迟:         ${m.http_req_duration ? m.http_req_duration['p(95)'].toFixed(2) + 'ms' : '-'}`)
      console.log(`  P99 延迟:         ${m.http_req_duration ? m.http_req_duration['p(99)'].toFixed(2) + 'ms' : '-'}`)
      console.log(`  出价次数:         ${m.bids_placed?.count || '-'}`)
      console.log(`  出价错误率:       ${m.bid_errors ? (m.bid_errors.rate * 100).toFixed(2) + '%' : '-'}`)
      console.log(`  出价 P95:         ${m.bid_latency ? m.bid_latency['p(95)'].toFixed(2) + 'ms' : '-'}`)
    } catch (e) {
      console.log(`  读取失败: ${e.message}`)
    }
  }

  // ==========================================
  // 8. 健康检查
  // ==========================================
  console.log('')
  console.log('━━━ 服务健康 ━━━')
  try {
    const ping = await fetch(`${BASE}/ping`)
    const pingBody = await ping.json()
    console.log(`  ping:       ${pingBody.message || 'OK'}`)

    const serverTime = await api('/api/server-time')
    console.log(`  服务器时间: ${new Date(serverTime.data?.serverTime || serverTime.serverTime).toISOString()}`)
  } catch (e) {
    console.log(`  [错误] ${e.message}`)
  }

  console.log('')
  console.log('══════════════════════════════════════')
  console.log('  观测完成')
  console.log('══════════════════════════════════════')
}

main().catch(err => {
  console.error('观测失败:', err.message)
  process.exit(1)
})
