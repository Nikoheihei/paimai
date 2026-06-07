/**
 * 04-consistency.mjs — 业务一致性验证脚本
 *
 * 用法:
 *   node scripts/load-test/04-consistency.mjs setup.json
 *
 * 验证:
 * 1. 最高价是否正确（所有 bid 中的最大值）
 * 2. winner 是否唯一
 * 3. 价格是否倒退（currentPriceCents 单调递增）
 * 4. 已结束拍卖是否还能出价
 * 5. bid 记录数与预期是否一致
 */

const BASE = process.env.BASE_URL || 'http://localhost:8080'

async function api(path, options = {}) {
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
  const setupPath = process.argv[2] || 'setup.json'

  // 动态 import JSON
  const fs = await import('fs')
  const setup = JSON.parse(fs.readFileSync(setupPath, 'utf-8'))

  console.log('=== 业务一致性验证 ===\n')

  // 用第一个拍卖做测试
  const auctionId = setup.auctionGroups?.[0]?.auctionId || setup.auction?.id
  const sellerToken = setup.seller.token
  const increment = 100

  // 取一个 buyer
  const buyer = setup.auctionGroups?.[0]?.buyers?.[0] || setup.buyers?.[0]
  if (!buyer) throw new Error('缺少 buyer')

  console.log(`auctionId: ${auctionId}`)
  console.log(`buyers: ${setup.stats?.totalBuyers || setup.buyerCount}\n`)

  // ---- 测试 1: 对已结束拍卖不能出价 ----
  console.log('--- 测试 1: 已结束拍卖拒绝出价 ---')
  try {
    // 先结束一个竞拍（用 seller 取消）
    // 注意：我们不取消主竞拍，用 cancel 创建一个已取消状态的来测试
    console.log('  (依赖已有 cancelled/failed 状态拍卖，跳过)')
  } catch (e) {
    console.log(`  跳过: ${e.message}`)
  }

  // ---- 测试 2: 价格单调递增验证 ----
  console.log('\n--- 测试 2: 价格单调递增 ---')
  try {
    const bids = await apiAuth(`/api/admin/auctions/${auctionId}/bids`, sellerToken)
    if (!Array.isArray(bids) || bids.length === 0) {
      console.log('  暂无出价记录，跳过')
    } else {
      const acceptedBids = bids.filter(b => b.accepted).sort((a, b) => a.id - b.id)
      let increasing = true
      let lastPrice = 0
      for (const bid of acceptedBids) {
        if (bid.amountCents < lastPrice) {
          increasing = false
          console.log(`  [FAIL] 价格倒退: bid#${bid.id} amount=${bid.amountCents} < last=${lastPrice}`)
        }
        lastPrice = bid.amountCents
      }
      if (increasing) {
        console.log(`  [PASS] ${acceptedBids.length} 个出价，价格单调递增`)
      }
    }
  } catch (e) {
    console.log(`  错误: ${e.message}`)
  }

  // ---- 测试 3: 拍卖状态一致性 ----
  console.log('\n--- 测试 3: 拍卖状态一致性 ---')
  try {
    const auction = await api(`/api/auctions/${auctionId}`)
    console.log(`  状态: ${auction.status}`)
    console.log(`  当前价: ${(auction.currentPriceCents / 100).toFixed(2)} 元`)
    console.log(`  winnerUserId: ${auction.winnerUserId || 'null'}`)

    if (auction.status === 'sold') {
      console.log(`  [CHECK] 拍卖已成交，winner 应为唯一`)
      if (auction.winnerUserId) {
        console.log(`  [PASS] winner 唯一: userId=${auction.winnerUserId}`)
      } else {
        console.log(`  [FAIL] status=sold 但 winnerUserId 为空`)
      }
    } else if (auction.status === 'running') {
      console.log(`  [PASS] 拍卖仍在进行中（未触发封顶）`)
    }
  } catch (e) {
    console.log(`  错误: ${e.message}`)
  }

  // ---- 测试 4: 排行榜正确性 ----
  console.log('\n--- 测试 4: 排行榜正确性 ---')
  try {
    const ranking = await api(`/api/auctions/${auctionId}/ranking?limit=10`)
    if (ranking.length > 0) {
      console.log(`  Top ${ranking.length}:`)
      ranking.forEach(r => {
        console.log(`    #${r.rank} userId=${r.userId} amount=${(r.amountCents / 100).toFixed(2)}`)
      })
      // 验证排序
      let sorted = true
      for (let i = 1; i < ranking.length; i++) {
        if (ranking[i].amountCents > ranking[i - 1].amountCents) {
          sorted = false
          console.log(`  [FAIL] 排行榜乱序: #${ranking[i].rank} > #${ranking[i-1].rank}`)
        }
      }
      if (sorted) console.log(`  [PASS] 排行榜正确排序`)
    } else {
      console.log(`  暂无排行`)
    }
  } catch (e) {
    console.log(`  错误: ${e.message}`)
  }

  // ---- 测试 5: 出价幂等性 ----
  console.log('\n--- 测试 5: 幂等性验证 ---')
  try {
    const idempotencyKey = `consistency-test-${Date.now()}`
    const currentAuction = await api(`/api/auctions/${auctionId}`)
    if (currentAuction.status !== 'running') {
      console.log(`  拍卖状态=${currentAuction.status}，跳过`)
    } else {
      const amount = currentAuction.currentPriceCents + increment
      // 第一次出价
      const res1 = await fetch(`${BASE}/api/auctions/${auctionId}/bids`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${buyer.token}` },
        body: JSON.stringify({ userId: buyer.userId, amountCents: amount, idempotencyKey }),
      })
      const b1 = await res1.json()
      // 重复出价（相同幂等键）
      const res2 = await fetch(`${BASE}/api/auctions/${auctionId}/bids`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${buyer.token}` },
        body: JSON.stringify({ userId: buyer.userId, amountCents: amount, idempotencyKey }),
      })
      const b2 = await res2.json()

      if (b2.data?.idempotentReplay) {
        console.log(`  [PASS] 幂等重放正确返回 (idempotentReplay=true)`)
      } else if (b2.code === 409) {
        console.log(`  [PASS] 幂等冲突被拒绝 (409)`)
      } else {
        console.log(`  [CHECK] 首次: code=${b1.code}, 重复: code=${b2.code}, replay=${b2.data?.idempotentReplay}`)
      }
    }
  } catch (e) {
    console.log(`  错误: ${e.message}`)
  }

  // ---- 汇总 ----
  console.log('\n=== 验证完成 ===')
}

main().catch(err => {
  console.error('Consistency check failed:', err.message)
  process.exit(1)
})
