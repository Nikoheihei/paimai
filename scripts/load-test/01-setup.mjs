/**
 * 01-setup.mjs — 压测数据准备脚本（多拍卖模式）
 *
 * 用法: node scripts/load-test/01-setup.mjs [auctionCount] [buyersPerAuction]
 *
 * 策略: 创建 N 个独立拍卖，每个拍卖有 M 个买家参与。
 *       这样出价不互相排斥，能测出真实并发处理能力。
 *       同时保留 1 个"竞争拍卖"用于测试一致性（所有买家抢同一个）。
 */

const BASE = process.env.BASE_URL || 'http://localhost:8080'
const AUCTION_COUNT = parseInt(process.argv[2] || '30', 10)
const BUYERS_PER_AUCTION = parseInt(process.argv[3] || '1', 10)

async function api(path, options = {}) {
  const url = `${BASE}${path}`
  const headers = { 'Content-Type': 'application/json', ...options.headers }
  const res = await fetch(url, { ...options, headers })
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

function uid() {
  return `${Math.random().toString(36).slice(2, 8)}`
}

async function main() {
  console.error('=== 压测数据准备（多拍卖模式）===')
  console.error(`AUCTION_COUNT: ${AUCTION_COUNT}`)
  console.error(`BUYERS_PER_AUCTION: ${BUYERS_PER_AUCTION}`)
  console.error(`总用户数: ${AUCTION_COUNT * BUYERS_PER_AUCTION + 1}`)
  console.error('')

  // 1. 注册 seller
  const sellerUser = `slr${uid()}`
  const sellerPass = 'Test123456'
  console.error(`注册 seller: ${sellerUser}`)
  const seller = await api('/api/auth/register', {
    method: 'POST',
    body: JSON.stringify({ username: sellerUser, password: sellerPass, nickname: '压测卖家', role: 'seller' }),
  })
  const sellerToken = seller.token
  console.error(`seller ID: ${seller.userId}`)

  // 2. 创建直播间 + 开播
  console.error('创建直播间...')
  const room = await apiAuth('/api/admin/rooms', sellerToken, {
    method: 'POST',
    body: JSON.stringify({ title: '压测直播间', coverUrl: '' }),
  })
  await apiAuth(`/api/admin/rooms/${room.id}/live`, sellerToken, { method: 'POST' })
  console.error(`room ID: ${room.id}`)

  // 3. 创建商品
  console.error('创建商品...')
  const product = await apiAuth('/api/admin/products', sellerToken, {
    method: 'POST',
    body: JSON.stringify({ name: '压测商品', description: 'test', imageUrl: '' }),
  })
  console.error(`product ID: ${product.id}`)

  // 4. 批量创建拍卖（每个买家组一个拍卖）
  console.error(`创建 ${AUCTION_COUNT} 个拍卖...`)
  const auctions = []
  const now = new Date()
  const endAt = new Date(now.getTime() + 3600_000)

  for (let i = 0; i < AUCTION_COUNT; i++) {
    const auction = await apiAuth('/api/admin/auctions', sellerToken, {
      method: 'POST',
      body: JSON.stringify({
        roomId: room.id,
        productId: product.id,
        mode: 'extension',
        startPriceCents: 0,
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
    auctions.push({ id: auction.id })
    if ((i + 1) % 10 === 0) console.error(`  已创建 ${i + 1}/${AUCTION_COUNT}`)
  }
  console.error(`${auctions.length} 个拍卖已就绪`)

  // 5. 注册买家 + 分配拍卖
  const totalBuyers = AUCTION_COUNT * BUYERS_PER_AUCTION
  console.error(`注册 ${totalBuyers} 个买家...`)
  const auctionBuyerMap = [] // [{ auctionId, buyers: [{userId, token}] }]

  for (let i = 0; i < AUCTION_COUNT; i++) {
    const buyers = []
    for (let j = 0; j < BUYERS_PER_AUCTION; j++) {
      const uname = `b${i}m${j}${uid()}`
      const buyer = await api('/api/auth/register', {
        method: 'POST',
        body: JSON.stringify({ username: uname, password: 'Test123456', nickname: `买家${i}-${j}`, role: 'buyer' }),
      })
      buyers.push({ userId: buyer.userId, token: buyer.token })
    }
    auctionBuyerMap.push({ auctionId: auctions[i].id, buyers })
    if ((i + 1) % 5 === 0) console.error(`  已注册 ${(i + 1) * BUYERS_PER_AUCTION}/${totalBuyers}`)
  }

  // 6. 额外创建 1 个"竞争拍卖"，所有买家都能参与（测试一致性）
  console.error('创建竞争拍卖（一致性测试用）...')
  const raceAuction = await apiAuth('/api/admin/auctions', sellerToken, {
    method: 'POST',
    body: JSON.stringify({
      roomId: room.id,
      productId: product.id,
      mode: 'extension',
      startPriceCents: 0,
      bidIncrementCents: 100,
      capPriceCents: 9999999,
      reservePriceCents: 0,
      startAt: now.toISOString(),
      endAt: endAt.toISOString(),
      extendThresholdSec: 30,
      extendDurationSec: 30,
    }),
  })
  await apiAuth(`/api/admin/auctions/${raceAuction.id}/publish`, sellerToken, { method: 'POST' })
  await apiAuth(`/api/admin/auctions/${raceAuction.id}/start`, sellerToken, { method: 'POST' })
  console.error(`竞争拍卖 ID: ${raceAuction.id}`)

  // 收集所有买家
  const allBuyers = []
  for (const group of auctionBuyerMap) {
    for (const b of group.buyers) {
      allBuyers.push(b)
    }
  }

  // 7. 输出
  const result = {
    baseUrl: BASE,
    seller: { userId: seller.userId, token: sellerToken },
    room: { id: room.id },
    product: { id: product.id },
    auctionGroups: auctionBuyerMap.map(g => ({
      auctionId: g.auctionId,
      buyerCount: g.buyers.length,
      buyers: g.buyers,
    })),
    raceAuction: {
      id: raceAuction.id,
      startPriceCents: 0,
      bidIncrementCents: 100,
      capPriceCents: 9999999,
      mode: 'extension',
    },
    allBuyers,
    stats: {
      totalAuctions: AUCTION_COUNT + 1,
      totalBuyers: totalBuyers,
      independentAuctions: AUCTION_COUNT,
      raceAuctions: 1,
    },
  }

  console.log(JSON.stringify(result))
  console.error('\n=== 数据准备完成 ===')
  console.error(`独立拍卖: ${AUCTION_COUNT} 个`)
  console.error(`竞争拍卖: 1 个`)
  console.error(`总买家: ${totalBuyers} 人`)
  console.error(`roomId: ${room.id}`)
}

main().catch(err => {
  console.error('Setup failed:', err.message)
  process.exit(1)
})
