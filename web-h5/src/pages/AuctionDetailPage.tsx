/**
 * AuctionDetailPage — 全屏竞拍详情页
 * 从商品浮层点击进入，专注单个竞拍的出价过程。
 */

import { useState, useEffect, useMemo, useCallback, useRef } from 'react'
import { createPortal } from 'react-dom'
import { useWebSocket } from '../hooks/useWebSocket'
import { getToken, getAuction, getRanking, placeBid, payBuyerOrder, listBuyerOrders, type Auction, type RankingItem, type BidResult } from '../api/client'
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
  const [showPayModal, setShowPayModal] = useState(false)
  const [payCountdown, setPayCountdown] = useState(300)
  const [payLoading, setPayLoading] = useState(false)
  const [paidAuctionIds, setPaidAuctionIds] = useState<number[]>([])
  const idemCounter = useRef(0)
  const payTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const payTriggeredRef = useRef<number | null>(null)

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

  const stopPayCountdown = useCallback(() => {
    if (payTimerRef.current) {
      clearInterval(payTimerRef.current)
      payTimerRef.current = null
    }
  }, [])

  const startPayCountdown = useCallback(() => {
    stopPayCountdown()
    setPayCountdown(300)
    payTimerRef.current = setInterval(() => {
      setPayCountdown(prev => {
        if (prev <= 1) {
          stopPayCountdown()
          return 0
        }
        return prev - 1
      })
    }, 1000)
  }, [stopPayCountdown])

  const closePayModal = useCallback(() => {
    setShowPayModal(false)
    stopPayCountdown()
  }, [stopPayCountdown])

  const soldWinnerAuctionId = auction?.status === 'sold' && auction.winnerUserId === userId
    ? auction.id
    : null

  // 成交后自动弹出支付倒计时（每个成交竞拍只触发一次）
  useEffect(() => {
    if (!soldWinnerAuctionId || paidAuctionIds.includes(soldWinnerAuctionId)) return
    if (payTriggeredRef.current === soldWinnerAuctionId) return
    payTriggeredRef.current = soldWinnerAuctionId
    setShowPayModal(true)
    startPayCountdown()
  }, [soldWinnerAuctionId, paidAuctionIds, startPayCountdown])

  useEffect(() => () => stopPayCountdown(), [stopPayCountdown])

  const handlePay = async () => {
    if (!auction) return
    setPayLoading(true)
    try {
      const orders = await listBuyerOrders(auction.id)
      if (orders.length === 0) {
        Toast.error('订单未生成，请稍后重试')
        setPayLoading(false)
        return
      }
      const order = orders[0]
      await payBuyerOrder(order.id)
      setPaidAuctionIds(prev => prev.includes(auction.id) ? prev : [...prev, auction.id])
      Toast.success('支付成功！')
      setShowPayModal(false)
      stopPayCountdown()
      payTriggeredRef.current = auction.id
      window.dispatchEvent(new CustomEvent('order:refresh'))
      load()
    } catch (err: any) {
      Toast.error(err.message || '支付失败')
    } finally {
      setPayLoading(false)
    }
  }

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

  const quickBidAmount = useMemo(() => {
    if (!auction) return 0
    const base = auction.currentPriceCents > 0 ? auction.currentPriceCents : auction.startPriceCents
    return base + auction.bidIncrementCents
  }, [auction])

  if (!auction) return <div className="panel pending">加载中…</div>

  const isRunning = auction.status === 'running'
  const price = auction.currentPriceCents > 0 ? auction.currentPriceCents : auction.startPriceCents

  return (
    <div className="auction-detail-page" style={{ transform: 'scale(0.667)', transformOrigin: 'top center', height: '150%', overflow: 'hidden' }}>
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
            <button className="quick-bid-btn" onClick={() => handleBid(quickBidAmount)}>
              +¥{formatCents(auction!.bidIncrementCents)}
            </button>
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
          {auction.status === 'sold' ? '竞拍已成交' : auction.status === 'failed' ? '竞拍流拍' : '竞拍未开始'}
        </div>
      )}

      {/* 支付弹窗 */}
      {showPayModal && auction?.status === 'sold' && auction.winnerUserId === userId && createPortal(
        <div className="modal-overlay" onClick={closePayModal}>
          <div className="modal-inner" onClick={e => e.stopPropagation()}>
            <div className="modal-title">恭喜成交！</div>
            <div className="modal-body">
              <div style={{ textAlign: 'center', marginBottom: 16 }}>
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>成交价</div>
                <div style={{ fontSize: 32, fontWeight: 800, color: 'var(--primary)' }}>¥{formatCents(auction.currentPriceCents)}</div>
              </div>
              <div style={{ textAlign: 'center', fontSize: 14, color: '#ff4757', fontWeight: 700 }}>
                ⏰ 支付倒计时: {Math.floor(payCountdown / 60)}:{String(payCountdown % 60).padStart(2, '0')}
              </div>
              {payCountdown === 0 && (
                <div style={{ textAlign: 'center', fontSize: 13, color: 'var(--text-muted)', marginTop: 8 }}>
                  订单已关闭
                </div>
              )}
            </div>
            <div className="modal-btn-row">
              <button className="modal-btn modal-cancel" onClick={closePayModal}>稍后再付</button>
              <button
                className="modal-btn modal-confirm"
                disabled={payLoading || payCountdown === 0}
                onClick={handlePay}
              >
                {payLoading ? '支付中...' : '立即支付'}
              </button>
            </div>
          </div>
        </div>,
        document.body,
      )}
    </div>
  )
}
