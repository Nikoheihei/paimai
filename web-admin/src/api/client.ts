const BASE = '/api'
const TOKEN_KEY = 'paimai_admin_token'

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

async function apiFetch<T = any>(path: string, options: RequestInit = {}): Promise<T> {
  const headers: Record<string, string> = {
    ...authHeaders(),
    ...(options.headers as Record<string, string> || {}),
  }
  if (options.body && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json'
  }
  const res = await fetch(`${BASE}${path}`, { ...options, headers })
  const body = await res.json()
  if (body.code === 401) { clearToken(); window.location.hash = '#/login'; throw new Error('登录已过期') }
  if (body.code !== 0) throw new Error(body.message)
  return body.data as T
}

export type AuthResult = { userId: number; username: string; nickname: string; token: string }
export async function login(username: string, password: string): Promise<AuthResult> {
  const res = await fetch(`${BASE}/auth/login`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password }) })
  const body = await res.json()
  if (body.code !== 0) throw new Error(body.message)
  return body.data
}
export async function register(username: string, password: string, nickname?: string): Promise<AuthResult> {
  const res = await fetch(`${BASE}/auth/register`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ username, password, nickname }) })
  const body = await res.json()
  if (body.code !== 0) throw new Error(body.message)
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
export async function goLive(id: number): Promise<LiveRoom> {
  return apiFetch(`/admin/rooms/${id}/live`, { method: 'POST' })
}
export async function closeRoom(id: number): Promise<CloseRoomResult> {
  return apiFetch(`/admin/rooms/${id}/close`, { method: 'POST' })
}

export type Product = { id: number; sellerId: number; name: string; imageUrl: string; description: string; createdAt: string }
export async function createProduct(name: string, imageUrl?: string, description?: string): Promise<Product> {
  return apiFetch('/admin/products', { method: 'POST', body: JSON.stringify({ sellerId: 0, name, imageUrl: imageUrl || '', description: description || '' }) })
}
export async function listProducts(): Promise<Product[]> {
  return apiFetch('/admin/products')
}
export async function deleteProduct(id: number): Promise<void> {
  return apiFetch(`/admin/products/${id}`, { method: 'DELETE' })
}

export type Auction = { id: number; roomId: number; productId: number; mode: string; startPriceCents: number; currentPriceCents: number; bidIncrementCents: number; capPriceCents: number; reservePriceCents: number | null; startAt: string; endAt: string; status: string; winnerUserId: number | null }
export async function createAuction(roomId: number, productId: number, mode: string, startPriceCents: number, bidIncrementCents: number, capPriceCents: number, startAt?: string, endAt?: string): Promise<Auction> {
  return apiFetch('/admin/auctions', { method: 'POST', body: JSON.stringify({ roomId, productId, mode, startPriceCents, bidIncrementCents, capPriceCents, startAt: startAt || '', endAt: endAt || '' }) })
}
export async function listAuctions(roomId?: number, status?: string): Promise<Auction[]> {
  const params = new URLSearchParams()
  if (roomId) params.set('roomId', String(roomId))
  if (status) params.set('status', status)
  const qs = params.toString()
  return apiFetch(`/admin/auctions${qs ? '?' + qs : ''}`)
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

export type Order = { id: number; auctionId: number; productId: number; buyerId: number; sellerId: number; finalPriceCents: number; status: string; createdAt: string; paidAt: string | null }
export async function listOrders(): Promise<Order[]> {
  return apiFetch('/admin/orders')
}
export async function getOrderDetail(id: number): Promise<Order> {
  return apiFetch(`/admin/orders/${id}`)
}
export async function payOrder(id: number): Promise<Order> {
  return apiFetch(`/admin/orders/${id}/pay`, { method: 'POST' })
}
