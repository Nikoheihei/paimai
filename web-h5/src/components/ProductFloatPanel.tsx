/**
 * ProductFloatPanel — 右侧悬浮商品浮层
 * 展示直播间内所有竞拍商品，支持 Tab 分类切换和选中切换竞拍
 */

import { useState } from 'react'
import type { Auction } from '../shared/types'
import type { AuctionStatus } from '../shared/types'

type TabKey = 'all' | 'running' | 'scheduled' | 'ended'

const TABS: { key: TabKey; label: string; filter?: AuctionStatus | AuctionStatus[] }[] = [
  { key: 'all', label: '全部' },
  { key: 'running', label: '在拍', filter: 'running' },
  { key: 'scheduled', label: '即将开拍', filter: 'scheduled' },
  { key: 'ended', label: '已结束', filter: ['sold', 'failed', 'cancelled', 'payment_timeout'] },
]

type Props = {
  auctions: Auction[]
  activeAuctionId: number | null
  onSelect: (auctionId: number) => void
  productNames?: Record<number, string>   // productId -> name
  productImages?: Record<number, string>  // productId -> imageUrl
}

export default function ProductFloatPanel({
  auctions,
  activeAuctionId,
  onSelect,
  productNames = {},
  productImages = {},
}: Props) {
  const [collapsed, setCollapsed] = useState(false)
  const [tab, setTab] = useState<TabKey>('all')

  const tabConfig = TABS.find(t => t.key === tab) || TABS[0]
  let filtered = auctions

  if (tabConfig.filter) {
    if (Array.isArray(tabConfig.filter)) {
      filtered = filtered.filter(a => tabConfig.filter!.includes(a.status))
    } else {
      filtered = auctions.filter(a => a.status === tabConfig.filter)
    }
  }

  // 价格文案规则
  function priceLabel(a: Auction): string {
    switch (a.status) {
      case 'sold':
      case 'payment_timeout':
        return '落槌价'
      case 'running':  return a.currentPriceCents > 0 ? '当前最高价' : '起拍价'
      case 'scheduled':return '起拍价'
      default:         return '-'
    }
  }

  // 价格值：成交显示最终价，否则显示起拍价或当前价
  function priceCents(a: Auction): number {
    return (a.status === 'sold' || a.status === 'payment_timeout') ? a.currentPriceCents
      : a.currentPriceCents > 0 ? a.currentPriceCents
      : a.startPriceCents
  }

  // 状态文案
  function statusText(a: Auction): string {
    switch (a.status) {
      case 'running':   return '竞拍中'
      case 'scheduled': return '即将开拍'
      case 'sold':      return '已成交'
      case 'payment_timeout': return '支付超时'
      case 'failed':    return '流拍'
      case 'cancelled': return '已取消'
      default:          return '草稿'
    }
  }

  return (
    <div className={`product-float-panel ${collapsed ? 'collapsed' : ''}`}>
      {/* 收起/展开 toggle */}
      <div className="product-float-toggle" onClick={() => setCollapsed(!collapsed)}>
        {collapsed ? '◀' : '▶'}
      </div>

      {/* Tab 切换 */}
      <div className="pfp-tabs">
        {TABS.map(t => (
          <div
            key={t.key}
            className={`pfp-tab ${tab === t.key ? 'active' : ''}`}
            onClick={() => setTab(t.key)}
          >
            {t.label}
          </div>
        ))}
      </div>

      {/* 商品列表 */}
      <div className="pfp-list">
        {filtered.length === 0 ? (
          <div className="pfp-empty">暂无{TABS.find(t => t.key === tab)?.label}的商品</div>
        ) : (
          filtered.map(a => {
            const isActive = a.id === activeAuctionId
            return (
              <div
                key={a.id}
                className={`pfp-item ${isActive ? 'active' : ''}`}
                onClick={() => onSelect(a.id)}
              >
                {/* 商品缩略图 */}
                {(productImages[a.productId]) ? (
                  <img className="pfp-item-img" src={productImages[a.productId]} alt="" />
                ) : (
                  <div className="pfp-item-img" style={{ display:'flex', alignItems:'center', justifyContent:'center', color:'#444', fontSize:11 }}>
                    &#128206;
                  </div>
                )}
                <div className="pfp-item-name">{productNames[a.productId] || `商品 #${a.productId}`}</div>
                <div className="pfp-item-status">{statusText(a)}</div>
                {a.status !== 'draft' && a.status !== 'cancelled' && (
                  <div className="pfp-item-price">{priceLabel(a)} &yen;{(priceCents(a) / 100).toFixed(2)}</div>
                )}
                {a.status === 'scheduled' && (
                  <div style={{ fontSize: 10, color: '#ff9800', marginTop: 2 }}>
                    &#128276; 提醒我
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
