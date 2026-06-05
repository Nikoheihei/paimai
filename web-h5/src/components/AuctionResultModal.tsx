/**
 * AuctionResultModal — 拍卖结果弹窗（庆祝动画版）
 *
 * 仿抖音/快手成交弹窗：
 * - 彩纸飘落粒子效果
 * - 光效背景
 * - 成交价格大字展示 + 倒计时自动关闭
 */

import { useState, useEffect, useMemo } from 'react'
import type { Auction } from '../shared/types'
import Countdown from './Countdown'

type Props = {
  open: boolean
  auction: Auction | null
  currentUserId: number
  productName?: string
  productImage?: string
  onClose: () => void
}

/** 生成彩纸粒子颜色 */
const CONFETTI_COLORS = ['#fe2c55','#ffc800','#25c778','#4eaaf0','#a855f7','#f97316']

export default function AuctionResultModal({ open, auction, currentUserId, productName, productImage, onClose }: Props) {
  const [countdownSec, setCountdownSec] = useState(8)

  // 生成彩纸粒子数据
  const confetti = useMemo(() =>
    Array.from({ length: 24 }, (_, i) => ({
      id: i,
      color: CONFETTI_COLORS[i % CONFETTI_COLORS.length],
      left: `${Math.random() * 100}%`,
      delay: `${Math.random() * 1.2}s`,
      duration: `${1.5 + Math.random() * 1.5}s`,
      size: 4 + Math.random() * 6,
      rot: Math.random() * 360,
    })), [])

  // 倒计时关闭
  useEffect(() => {
    if (!open) return
    setCountdownSec(8)
  }, [open])

  if (!open || !auction) return null

  const isWinner = auction.winnerUserId === currentUserId
  const finalPrice = (auction.currentPriceCents / 100).toFixed(2)

  return (
    <div className="auction-result-overlay" onClick={onClose}>
      <div className="ar-modal" onClick={e=>e.stopPropagation()}>
        {/* 彩纸层 */}
        <div className="confetti-container">
          {confetti.map(c => (
            <div key={c.id} className="confetti-piece"
              style={{
                left: c.left,
                width: c.size, height: c.size,
                background: c.color,
                animationDelay: c.delay,
                animationDuration: c.duration,
                transform: `rotate(${c.rot}deg)`,
                borderRadius: Math.random()>.5?'50%':'2px',
              }}
            />
          ))}
        </div>

        {/* 内容 */}
        <div style={{ position:'relative', zIndex:1 }}>
          {/* Mascot / Icon */}
          <div className="ar-mascot">
            {isWinner ? '\uD83C\uDF89' : '\uD83D\uDCB0'}
          </div>

          <div className="ar-title">
            {isWinner ? '恭喜您，拍到了！' : '拍卖已结束'}
          </div>

          <div className="ar-subtitle">
            {isWinner ? '请在倒计时内确认订单' : `最终成交价`}
          </div>

          {/* 商品卡片 */}
          <div className="ar-product-card">
            <div className="ar-thumb">{productImage?(
              <img src={productImage} alt="" style={{width:'100%',height:'100%',objectFit:'cover',borderRadius:10}} />
            ):'\uD83D\uDCE6'}</div>
            <div>
              <div className="ar-pname">{productName||`商品 #${auction.productId}`}</div>
              <div className="ar-final-label">成交价</div>
              <div className="ar-final-price">&yen;{finalPrice}</div>
            </div>
          </div>

          {/* 结果文案 */}
          {isWinner ? (
            <div className="ar-winner">
              您以 <strong>&yen;{finalPrice}</strong> 得标！
            </div>
          ) : (
            <div className="ar-loser">
              {auction.winnerUserId ? `用户 ${auction.winnerUserId}` : '流拍'} 以 &yen;{finalPrice}{auction.winnerUserId?' 得标':' 结束'}
            </div>
          )}

          {/* 倒计时 */}
          <div className="ar-countdown-hint">
            <Countdown endAt={new Date(Date.now()+countdownSec*1000).toISOString()}
              onEnd={onClose} />
            {' '}后自动返回
          </div>

          {/* 关闭按钮 */}
          <button className="ar-close-btn" onClick={onClose}>
            {isWinner ? '去支付' : '返回直播间'}
          </button>
        </div>
      </div>
    </div>
  )
}
