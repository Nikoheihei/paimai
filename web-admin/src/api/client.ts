const BASE = '/api'
const TOKEN_KEY = 'paimai_admin_token'

type ApiEnvelope<T> = {
  code: number | string
  message?: string
  data: T
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY)
}
export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}
export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}
export function isLoggedIn(): boolean {
  return !!getToken()
}

function authHeaders(): Record<string, string> {
  const token = getToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function parseApiResponse<T>(res: Response): Promise<ApiEnvelope<T>> {
  const text = await res.text()
  if (!text.trim()) {
    throw new Error(`服务返回空响应（HTTP ${res.status}）`)
  }

  try {
    return JSON.parse(text) as ApiEnvelope<T>
  } catch {
    const preview = text.replace(/\s+/g, ' ').slice(0, 80)
    const isHtml = text.trimStart().startsWith('<')
    throw new Error(
      isHtml
        ? `服务返回了 HTML 页面（HTTP ${res.status}），请确认后端服务和 /api 代理正常`
        : `服务返回非 JSON 响应（HTTP ${res.status}）：${preview}`,
    )
  }
}

async function apiFetch<T = any>(path: string, options: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    ...authHeaders(),
    ...(options.headers as Record<string, string> || {}),
  }
  if (options.body && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json'
  }
  const res = await fetch(`${BASE}${path}`, { ...options, headers })
  const body = await parseApiResponse<T>(res)
  if (body.code === 401) { clearToken(); window.location.hash = '#/login'; throw new Error('登录已过期') }
  if (body.code !== 0) throw new Error(body.message || `请求失败（code ${body.code}）`)
  return body.data as T
}

export type AuthResult = { userId: number; username: string; nickname: string; token: string }
export async function login(username: string, password: string): Promise<AuthResult> {
  const res = await fetch(`${BASE}/auth/login`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password }) })
  const body = await parseApiResponse<AuthResult>(res)
  if (body.code !== 0) throw new Error(body.message || `请求失败（code ${body.code}）`)
  return body.data
}
export async function register(username: string, password: string, nickname?: string): Promise<AuthResult> {
  const res = await fetch(`${BASE}/auth/register`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password, nickname, role: 'seller' }) })
  const body = await parseApiResponse<AuthResult>(res)
  if (body.code !== 0) throw new Error(body.message || `请求失败（code ${body.code}）`)
  return body.data
}

export type LiveRoom = { id: number; sellerId: number; title: string; coverUrl: string; status: string; createdAt: string }
export type CloseRoomResult = { roomId: number; status: string; settled: number }
export async function createRoom(title: string, coverUrl?: string): Promise<LiveRoom> {
  return apiFetch('/admin/rooms', { method: 'POST', body: JSON.stringify({ title, coverUrl: coverUrl || '' }) })
}
export async function listRooms(): Promise<LiveRoom[]> {
  return apiFetch('/admin/rooms')
}
export async function getRoom(id: number): Promise<LiveRoom> {
  return apiFetch(`/admin/rooms/${id}`)
}
export async function updateRoom(id: number, title: string, coverUrl?: string): Promise<LiveRoom> {
  return apiFetch(`/admin/rooms/${id}`, { method: 'PATCH', body: JSON.stringify({ title, coverUrl: coverUrl || '' }) })
}
export async function deleteRoom(id: number): Promise<void> {
  return apiFetch(`/admin/rooms/${id}`, { method: 'DELETE' })
}
export async function goLive(id: number): Promise<LiveRoom> {
  return apiFetch(`/admin/rooms/${id}/live`, { method: 'POST' })
}
export async function closeRoom(id: number): Promise<CloseRoomResult> {
  return apiFetch(`/admin/rooms/${id}/close`, { method: 'POST' })
}

export type Product = { id: number; sellerId: number; name: string; imageUrl: string; description: string; status?: 'available' | 'locked' | 'offline'; createdAt: string }
export async function createProduct(name: string, imageUrl?: string, description?: string): Promise<Product> {
  return apiFetch('/admin/products', { method: 'POST', body: JSON.stringify({ sellerId: 0, name, imageUrl: imageUrl || '', description: description || '' }) })
}
export async function listProducts(): Promise<Product[]> {
  return apiFetch('/admin/products')
}
export async function updateProduct(id: number, name: string, imageUrl?: string, description?: string): Promise<Product> {
  return apiFetch(`/admin/products/${id}`, { method: 'PATCH', body: JSON.stringify({ name, imageUrl: imageUrl || '', description: description || '' }) })
}
export async function deleteProduct(id: number): Promise<void> {
  return apiFetch(`/admin/products/${id}`, { method: 'DELETE' })
}
export async function offlineProduct(id: number): Promise<Product> {
  return apiFetch(`/admin/products/${id}/offline`, { method: 'POST' })
}
export async function listAuctionBids(auctionId: number): Promise<{ id: number; auctionId: number; userId: number; amountCents: number; accepted: boolean; createdAt: string }[]> {
  return apiFetch(`/admin/auctions/${auctionId}/bids`)
}
export async function getRoomStats(roomId: number): Promise<{ roomId: number; onlineCount: number }> {
  return apiFetch(`/admin/rooms/${roomId}/stats`)
}

export type Auction = { id: number; roomId: number; productId: number; mode: string; startPriceCents: number; currentPriceCents: number; bidIncrementCents: number; capPriceCents: number; reservePriceCents: number | null; extendThresholdSec?: number; extendDurationSec?: number; startAt: string; endAt: string; status: string; winnerUserId: number | null }
type AuctionPayload = {
  roomId: number
  productId: number
  mode: string
  startPriceCents: number
  bidIncrementCents: number
  capPriceCents: number
  reservePriceCents: number | null
  extendThresholdSec: number
  extendDurationSec: number
  startAt: string | null
  endAt: string | null
}
function makeAuctionPayload(
  roomId: number,
  productId: number,
  mode: string,
  startPriceCents: number,
  bidIncrementCents: number,
  capPriceCents: number,
  reservePriceCents?: number | null,
  extendThresholdSec?: number,
  extendDurationSec?: number,
  startAt?: string,
  endAt?: string,
): AuctionPayload {
  return {
    roomId,
    productId,
    mode,
    startPriceCents,
    bidIncrementCents,
    capPriceCents,
    reservePriceCents: reservePriceCents ?? null,
    extendThresholdSec: extendThresholdSec || 0,
    extendDurationSec: extendDurationSec || 0,
    startAt: startAt || null,
    endAt: endAt || null,
  }
}
export async function createAuction(
  roomId: number,
  productId: number,
  mode: string,
  startPriceCents: number,
  bidIncrementCents: number,
  capPriceCents: number,
  reservePriceCents?: number | null,
  extendThresholdSec?: number,
  extendDurationSec?: number,
  startAt?: string,
  endAt?: string,
): Promise<Auction> {
  return apiFetch('/admin/auctions', {
    method: 'POST',
    body: JSON.stringify(makeAuctionPayload(roomId, productId, mode, startPriceCents, bidIncrementCents, capPriceCents, reservePriceCents, extendThresholdSec, extendDurationSec, startAt, endAt)),
  })
}
export async function relistProduct(
  productId: number,
  roomId: number,
  mode: string,
  startPriceCents: number,
  bidIncrementCents: number,
  capPriceCents: number,
  reservePriceCents?: number | null,
  extendThresholdSec?: number,
  extendDurationSec?: number,
  startAt?: string,
  endAt?: string,
): Promise<Auction> {
  return apiFetch(`/admin/products/${productId}/relist`, {
    method: 'POST',
    body: JSON.stringify(makeAuctionPayload(roomId, productId, mode, startPriceCents, bidIncrementCents, capPriceCents, reservePriceCents, extendThresholdSec, extendDurationSec, startAt, endAt)),
  })
}
export async function listAuctions(roomId?: number, status?: string): Promise<Auction[]> {
  const params = new URLSearchParams()
  if (roomId) params.set('roomId', String(roomId))
  if (status) params.set('status', status)
  const qs = params.toString()
  return apiFetch(`/admin/auctions${qs ? '?' + qs : ''}`)
}
export async function updateAuction(id: number, input: Partial<{
  mode: string
  startPriceCents: number
  bidIncrementCents: number
  capPriceCents: number
  reservePriceCents: number | null
  clearReservePrice: boolean
  extendThresholdSec: number
  extendDurationSec: number
  startAt: string
  endAt: string
}>): Promise<Auction> {
  return apiFetch(`/admin/auctions/${id}`, { method: 'PATCH', body: JSON.stringify(input) })
}
export async function publishAuction(id: number): Promise<Auction> {
  return apiFetch(`/admin/auctions/${id}/publish`, { method: 'POST' })
}
export async function startAuction(id: number, durationSec?: number): Promise<Auction> {
  return apiFetch(`/admin/auctions/${id}/start`, { method: 'POST', body: JSON.stringify({ durationSec: durationSec || 300 }) })
}
export async function cancelAuction(id: number, reason?: string): Promise<Auction> {
  return apiFetch(`/admin/auctions/${id}/cancel`, { method: 'POST', body: JSON.stringify({ reason: reason || '' }) })
}
export async function settleAuction(id: number): Promise<any> {
  return apiFetch(`/admin/auctions/${id}/settle`, { method: 'POST' })
}

export type Order = { id: number; auctionId: number; productId: number; buyerId: number; sellerId: number; finalPriceCents: number; status: string; addressId?: number | null; addressSnapshot?: string; createdAt: string; paidAt: string | null }
export async function listOrders(): Promise<Order[]> {
  return apiFetch('/admin/orders')
}
export async function getOrderDetail(id: number): Promise<Order> {
  return apiFetch(`/admin/orders/${id}`)
}
export async function payOrder(id: number): Promise<Order> {
  return apiFetch(`/admin/orders/${id}/pay`, { method: 'POST' })
}

/** 上传图片，返回 URL */
export async function uploadImage(file: File): Promise<string> {
  const formData = new FormData()
  formData.append('file', file)
  const token = getToken()
  const res = await fetch(`${BASE}/upload`, {
    method: 'POST',
    headers: token ? { Authorization: `Bearer ${token}` } : {},
    body: formData,
  })
  const body = await parseApiResponse<{ url: string }>(res)
  if (body.code !== 0) throw new Error(body.message || '上传失败')
  return body.data.url
}
