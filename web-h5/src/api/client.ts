/**
 * API 客户端 — 竞拍系统 REST 接口封装
 *
 * 所有请求自动携带 JWT token（localStorage 中读取）。
 */

const BASE = '/api'

// --- Token 管理 ---

const TOKEN_KEY = 'paimai_token'

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

async function parseApiData<T>(res: Response, expiredMessage = '登录已过期'): Promise<T> {
  const body = await parseApiResponse<T>(res)
  if (body.code === 401) {
    clearToken()
    throw new Error(expiredMessage)
  }
  if (body.code !== 0) {
    throw new Error(body.message || `请求失败（code ${body.code}）`)
  }
  return body.data
}

// --- 认证 API ---

export type AuthResult = {
  userId: number
  username: string
  nickname: string
  token: string
}

export type MeResult = {
  userId: number
  username: string
  nickname: string
  avatarUrl: string
  role: string
}

export async function register(username: string, password: string, nickname?: string): Promise<AuthResult> {
  const res = await fetch(`${BASE}/auth/register`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password, nickname, role: 'buyer' }),
  })
  return parseApiData<AuthResult>(res)
}

export async function login(username: string, password: string): Promise<AuthResult> {
  const res = await fetch(`${BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  })
  return parseApiData<AuthResult>(res)
}

export async function getMe(): Promise<MeResult> {
  const res = await fetch(`${BASE}/auth/me`, {
    headers: authHeaders(),
  })
  return parseApiData<MeResult>(res, '登录已过期，请重新登录')
}

// --- 通用请求工具 ---

function authHeaders(): Record<string, string> {
  const token = getToken()
  if (token) {
    return { Authorization: `Bearer ${token}` }
  }
  return {}
}

async function apiFetch(path: string, options: RequestInit = {}): Promise<any> {
  const headers: Record<string, string> = {
    ...authHeaders(),
    ...(options.headers as Record<string, string> || {}),
  }
  if (options.body && typeof options.body === 'string' && !headers['Content-Type']) {
    headers['Content-Type'] = 'application/json'
  }
  const res = await fetch(`${BASE}${path}`, { ...options, headers })
  return parseApiData(res)
}

// --- 业务 API ---

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
  productName?: string
  productImage?: string
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
  anchorNickname?: string
  anchorAvatar?: string
}

export type RankingItem = {
  rank: number
  userId: number
  amountCents: number
}

export type Order = {
  id: number
  auctionId: number
  productId: number
  buyerId: number
  sellerId: number
  finalPriceCents: number
  status: string
  addressId: number | null
  addressSnapshot: string
  createdAt: string
  paidAt: string | null
  productName?: string
  productImage?: string
  sellerNickname?: string
}

export async function listBuyerOrders(auctionId?: number): Promise<Order[]> {
  const params = auctionId ? `?auctionId=${auctionId}` : ''
  return apiFetch(`/orders${params}`)
}

export async function payBuyerOrder(orderId: number, addressId?: number, addressSnapshot?: string): Promise<Order> {
  return apiFetch(`/orders/${orderId}/pay`, {
    method: 'POST',
    body: JSON.stringify({ addressId, addressSnapshot }),
  })
}

export async function getBuyerOrder(orderId: number): Promise<Order> {
  return apiFetch(`/orders/${orderId}`)
}

export async function getRoom(roomId: number): Promise<LiveRoom> {
  return apiFetch(`/rooms/${roomId}`)
}

export async function getRoomAuctions(roomId: number, status?: string): Promise<Auction[]> {
  const params = status ? `?status=${status}` : ''
  return apiFetch(`/rooms/${roomId}/auctions${params}`)
}

export async function getAuction(id: number): Promise<Auction> {
  return apiFetch(`/auctions/${id}`)
}

export async function getRanking(auctionId: number, limit = 10): Promise<RankingItem[]> {
  return apiFetch(`/auctions/${auctionId}/ranking?limit=${limit}`)
}

export async function placeBid(auctionId: number, userId: number, amountCents: number, idempotencyKey: string): Promise<BidResult> {
  return apiFetch(`/auctions/${auctionId}/bids`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ userId, amountCents, idempotencyKey }),
  })
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
  const body = await parseApiData<{ url: string }>(res)
  return body.url
}

// --- 地址管理 API ---

export type Address = {
  id: number
  userId: number
  name: string
  phone: string
  province: string
  city: string
  district: string
  detail: string
  isDefault: boolean
}

export async function listAddresses(): Promise<Address[]> {
  const list = await apiFetch('/addresses')
  return Array.isArray(list) ? list : []
}
export async function createAddress(input: Omit<Address, 'id' | 'userId'>): Promise<Address> {
  return apiFetch('/addresses', { method: 'POST', body: JSON.stringify(input) })
}
export async function updateAddress(id: number, input: Omit<Address, 'id' | 'userId'>): Promise<Address> {
  return apiFetch(`/addresses/${id}`, { method: 'PUT', body: JSON.stringify(input) })
}
export async function deleteAddress(id: number): Promise<void> {
  return apiFetch(`/addresses/${id}`, { method: 'DELETE' })
}
