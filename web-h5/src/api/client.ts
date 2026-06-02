/**
 * API 客户端 — 竞拍系统 REST 接口封装
 */

const BASE = '/api'

export type Auction = {
  id: number
  roomId: number
  productId: number
  mode: string
  startPriceCents: number
  currentPriceCents: number
  bidIncrementCents: number
  capPriceCents: number
  reservePriceCents: number | null
  startAt: string
  endAt: string
  status: string
  winnerUserId: number | null
}

export type BidResult = {
  accepted: boolean
  auctionId: number
  userId: number
  amountCents: number
  currentPriceCents: number
  status: string
  endAt: string
  extended: boolean
  sold: boolean
  reserveMet: boolean
  idempotentReplay: boolean
  tooFrequent: boolean
}

export type LiveRoom = {
  id: number
  title: string
  sellerId: number
  coverUrl: string
  status: string
}

export type RankingItem = {
  rank: number
  userId: number
  amountCents: number
}

export async function getRoom(roomId: number): Promise<LiveRoom> {
  const res = await fetch(`${BASE}/rooms/${roomId}`)
  const body = await res.json()
  if (body.code !== 0) throw new Error(body.message)
  return body.data
}

export async function getRoomAuctions(roomId: number, status?: string): Promise<Auction[]> {
  const params = status ? `?status=${status}` : ''
  const res = await fetch(`${BASE}/rooms/${roomId}/auctions${params}`)
  const body = await res.json()
  if (body.code !== 0) throw new Error(body.message)
  return body.data
}

export async function getAuction(id: number): Promise<Auction> {
  const res = await fetch(`${BASE}/auctions/${id}`)
  const body = await res.json()
  if (body.code !== 0) throw new Error(body.message)
  return body.data
}

export async function getRanking(auctionId: number, limit = 10): Promise<RankingItem[]> {
  const res = await fetch(`${BASE}/auctions/${auctionId}/ranking?limit=${limit}`)
  const body = await res.json()
  if (body.code !== 0) throw new Error(body.message)
  return body.data
}

export async function placeBid(auctionId: number, userId: number, amountCents: number, idempotencyKey: string): Promise<BidResult> {
  const res = await fetch(`${BASE}/auctions/${auctionId}/bids`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ userId, amountCents, idempotencyKey }),
  })
  const body = await res.json()
  if (body.code !== 0) {
    if (body.code === 409) throw new Error(body.message)
    throw new Error(body.message)
  }
  return body.data
}
