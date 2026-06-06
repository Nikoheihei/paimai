/**
 * 拍卖系统共享 TypeScript 类型定义
 * web-admin 和 web-h5 共用
 */

// === 认证 ===

export interface AuthResult {
  userId: number
  username: string
  nickname: string
  token: string
}

export interface UserInfo {
  userId: number
  username: string
  nickname: string
  avatarUrl: string
  role: 'buyer' | 'seller' | 'anchor'
}

// === 直播间 ===

export type RoomStatus = 'offline' | 'live' | 'closed'

export interface LiveRoom {
  id: number
  sellerId: number
  title: string
  coverUrl: string
  status: RoomStatus
  createdAt: string
  anchorNickname?: string
  anchorAvatar?: string
}

// === 商品 ===

export interface Product {
  id: number
  sellerId: number
  name: string
  imageUrl: string
  description: string
  createdAt: string
}

// === 竞拍 ===

export type AuctionMode = 'sudden_death' | 'extension'
export type AuctionStatus = 'draft' | 'scheduled' | 'running' | 'sold' | 'failed' | 'cancelled'

export interface Auction {
  id: number
  roomId: number
  productId: number
  mode: AuctionMode
  startPriceCents: number
  currentPriceCents: number
  bidIncrementCents: number
  capPriceCents: number
  reservePriceCents: number | null
  startAt: string
  endAt: string
  extendThresholdSec: number
  extendDurationSec: number
  status: AuctionStatus
  winnerUserId: number | null
  version: number
  cancelReason: string
  productName?: string
  productImage?: string
}

/** 创建竞拍的表单数据 */
export interface AuctionFormInput {
  roomId: number
  productId: number
  mode: AuctionMode
  startPriceCents: number
  bidIncrementCents: number
  capPriceCents: number
  reservePriceCents: number | null
  durationSec: number
  extendThresholdSec: number
  extendDurationSec: number
  startAt: string
  endAt: string
}

// === 出价 ===

export interface BidResult {
  accepted: boolean
  auctionId: number
  userId: number
  amountCents: number
  currentPriceCents: number
  status: AuctionStatus
  endAt: string
  extended: boolean
  sold: boolean
  reserveMet: boolean
  idempotentReplay: boolean
  tooFrequent: boolean
}

export interface RankingItem {
  rank: number
  userId: number
  amountCents: number
}

// === 订单 ===

export type OrderStatus = 'pending_payment' | 'paid' | 'closed'

export interface Order {
  id: number
  auctionId: number
  productId: number
  buyerId: number
  sellerId: number
  finalPriceCents: number
  status: OrderStatus
  createdAt: string
  paidAt: string | null
  productName?: string
  productImage?: string
  sellerNickname?: string
}

// === 收货地址 ===
// TODO: 后端尚未实现地址 CRUD API（Phase 2 补充）
// 接口预留：GET/POST /api/addresses, PUT/DELETE /api/addresses/:id

export interface Address {
  id?: number
  name: string       // 收货人姓名
  phone: string      // 手机号
  province: string   // 省
  city: string       // 市
  district: string   // 区
  detail: string     // 详细地址
  isDefault: boolean // 是否默认
}

// === WebSocket ===

export interface WsMessage {
  type: string
  data: unknown
}
