/**
 * AuctionDetailPage — 全屏竞拍详情页
 * 从商品浮层点击进入，专注单个竞拍的出价过程。
 */

import { useState, useEffect, useMemo, useCallback, useRef } from 'react'
import { useWebSocket } from '../hooks/useWebSocket'
import { getToken, getAuction, getRanking, placeBid, type Auction, type RankingItem, type BidResult } from '../api/client'
import type { WsMessage } from '../hooks/useWebSocket'
import Countdown from '../components/Countdown'
import Toast from '../components/Toast'

function formatCents(c: number) { return (c / 100).toFixed(2) }

function parseUserIdFromToken(): number {
  const token = getToken()
  if (!token) return 0
  try {
    const payload = JSON.parse(atob(token.split('.')[1]))
    return payload.userId || 0
  } catch { return 0 }
}

type Props = {
  auctionId: number
  onBack: () => void
}

export default function AuctionDetailPage({ auctionId, onBack }: Props) {
  const userId = useMemo(() => parseUserIdFromToken(), [])
  const [auction, setAuction] = useState<Auction | null>(null)
  const [ranking, setRanking] = useState<RankingItem[]>([])
  const [bidStatus, setBidStatus] = useState<'idle' | 'sending' | 'ok' | 'fail'>('idle')
  const [customAmount, setCustomAmount] = useState('')
  const [lastMessage, setLastMessage] = useState<WsMessage | null>(null)
  const idemCounter = useRef(0)

  const { connected: _wsConnected } = useWebSocket(auction?.roomId || 0, userId, {
    onMessage: (msg) => setLastMessage(msg),
  })

  const load = useCallback(() => {
    getAuction(auctionId).then(setAuction).catch(() => {})
    getRanking(auctionId, 20).then(setRanking).catch(() => {})
  }, [auctionId])

  useEffect(() => { load() }, [load])

  // WS 消息处理
  useEffect(() => {
    if (!lastMessage || !auction) return
    const msgType = lastMessage.type
    if (msgType === 'bid.accepted' || msgType === 'price.updated' || msgType === 'auction.extended' || msgType === 'auction.ended') {
      load()
    }
  }, [lastMessage, auction, load])

  const handleBid = async (amountCents: number) => {
    if (!auction || bidStatus === 'sending') return
    setBidStatus('sending')
    const idem = `detail-${auctionId}-${userId}-${++idemCounter.current}-${Date.now()}`
    try {
      const res: BidResult = await placeBid(auctionId, userId, amountCents, idem)
      if (res.accepted) {
        setBidStatus('ok')
        Toast.success('出价成功！')
        load()
      } else {
        setBidStatus('fail')
        Toast.error(res.tooFrequent ? '出价太频繁' : '出价被拒绝')
      }
    } catch (err: any) {
      setBidStatus('fail')
      Toast.error(err.message || '出价失败')
    }
  }

  const quickBids = useMemo(() => {
    if (!auction) return []
    const base = auction.currentPriceCents > 0 ? auction.currentPriceCents : auction.startPriceCents
    const inc = auction.bidIncrementCents
    return [
      base + inc,
      base + inc * 2,
      base + inc * 5,
    ].filter(v => v <= auction.capPriceCents || auction.capPriceCents === 0)
  }, [auction])

  if (!auction) return <div className="panel pending">加载中…</div>

  const isRunning = auction.status === 'running'
  const price = auction.currentPriceCents > 0 ? auction.currentPriceCents : auction.startPriceCents

  return (
    <div className="auction-detail-page">
      {/* 顶部导航 */}
      <header className="detail-header">
        <button className="back-btn" onClick={onBack}>←</button>
        <h1>竞拍详情 #{auctionId}</h1>
        <span className={`status-badge-small ${auction.status}`}>{auction.status}</span>
      </header>

      {/* 核心价格区 */}
      <div className="detail-price-section">
        <div className="detail-price-label">{isRunning ? '当前最高价' : '起拍价'}</div>
        <div className="detail-price-value">¥{formatCents(price)}</div>
        {isRunning && auction.endAt && (
          <div className="detail-countdown">
            <Countdown endAt={auction.endAt} />
          </div>
        )}
      </div>

      {/* 出价历史时间线 */}
      <div className="detail-history">
        <h3>出价记录</h3>
        {ranking.length > 0 ? (
          <div className="history-list">
            {ranking.map((item, idx) => (
              <div key={idx} className={`history-item ${item.userId === userId ? 'is-me' : ''}`}>
                <span className="history-rank">#{item.rank}</span>
                <span className="history-user">{item.userId === userId ? '我' : `用户${item.userId}`}</span>
                <span className="history-amount">¥{formatCents(item.amountCents)}</span>
              </div>
            ))}
          </div>
        ) : (
          <p className="empty">暂无出价记录</p>
        )}
      </div>

      {/* 底部出价操作区 */}
      {isRunning && (
        <div className="detail-bid-bar">
          <div className="quick-bids">
            {quickBids.map((amt, i) => (
              <button key={i} className="quick-bid-btn" onClick={() => handleBid(amt)}>
                +¥{formatCents(amt - price)}
              </button>
            ))}
          </div>
          <div className="custom-bid-row">
            <input
              type="number"
              placeholder="自定义金额(元)"
              value={customAmount}
              onChange={e => setCustomAmount(e.target.value)}
              className="custom-bid-input"
            />
            <button
              className="bid-btn-primary"
              disabled={bidStatus === 'sending' || !customAmount}
              onClick={() => handleBid(Math.round(parseFloat(customAmount) * 100))}
            >
              {bidStatus === 'sending' ? '出价中...' : '出价'}
            </button>
          </div>
        </div>
      )}

      {!isRunning && (
        <div className="detail-ended-notice">
          {auction.status === 'sold' ? '🎉 竞拍已成交' : auction.status === 'failed' ? '❌ 竞拍流拍' : '⏸ 竞拍未开始'}
        </div>
      )}
    </div>
  )
}
