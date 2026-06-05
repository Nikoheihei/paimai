/**
 * StatusBadge — 状态标签组件
 * 通用颜色映射：竞拍状态 / 订单状态 / 房间状态
 */

import type { AuctionStatus, OrderStatus, RoomStatus } from '../shared/types'

type Props = {
  status: AuctionStatus | OrderStatus | RoomStatus
  size?: 'small' | 'default'
}

const AUCTION_MAP: Record<AuctionStatus, { label: string; className: string }> = {
  draft:       { label: '草稿',     className: 'badge-gray' },
  scheduled:   { label: '待开始',    className: 'badge-blue' },
  running:     { label: '竞拍中',   className: 'badge-red' },
  sold:        { label: '已成交',    className: 'badge-green' },
  failed:      { label: '流拍',      className: 'badge-gray' },
  cancelled:   { label: '已取消',    className: 'badge-gray' },
}

const ORDER_MAP: Record<OrderStatus, { label: string; className: string }> = {
  pending_payment: { label: '待付款', className: 'badge-orange' },
  paid:            { label: '已支付', className: 'badge-green' },
  closed:          { label: '已关闭', className: 'badge-gray' },
}

const ROOM_MAP: Record<RoomStatus, { label: string; className: string }> = {
  offline: { label: '未开播', className: 'badge-gray' },
  live:    { label: '直播中', className: 'badge-red' },
  closed:  { label: '已结束', className: 'badge-gray' },
}

export default function StatusBadge({ status, size = 'default' }: Props) {
  let info: { label: string; className: string }

  if (status in AUCTION_MAP) {
    info = AUCTION_MAP[status as AuctionStatus]
  } else if (status in ORDER_MAP) {
    info = ORDER_MAP[status as OrderStatus]
  } else if (status in ROOM_MAP) {
    info = ROOM_MAP[status as RoomStatus]
  } else {
    info = { label: String(status), className: 'badge-gray' }
  }

  return (
    <span className={`status-badge ${info.className} ${size === 'small' ? 'badge-sm' : ''}`}>
      {info.label}
    </span>
  )
}
