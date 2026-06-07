/**
 * AuctionDetailPage — 全屏竞拍详情页
 * 从商品浮层点击进入，专注单个竞拍的出价过程。
 */

import { useState, useEffect, useMemo, useCallback, useRef } from 'react'
import { createPortal } from 'react-dom'
import { useWebSocket } from '../hooks/useWebSocket'
import { getToken, getAuction, getRanking, placeBid, payBuyerOrder, listBuyerOrders, listAddresses, type Auction, type RankingItem, type BidResult, type Address, type Order } from '../api/client'
import type { WsMessage } from '../hooks/useWebSocket'
import Countdown from '../components/Countdown'
import Toast from '../components/Toast'
import { formatPaymentCountdown, paymentDeadlineMs, PAYMENT_WINDOW_SECONDS, remainingPaymentSeconds } from '../utils/paymentDeadline'

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
  const [payCountdown, setPayCountdown] = useState(PAYMENT_WINDOW_SECONDS)
  const [payOrder, setPayOrder] = useState<Order | null>(null)
  const [payLoading, setPayLoading] = useState(false)
  const [paidAuctionIds, setPaidAuctionIds] = useState<number[]>([])
  const [addresses, setAddresses] = useState<Address[]>([])
  const [selectedAddrId, setSelectedAddrId] = useState<number | null>(null)
  const idemCounter = useRef(0)
  const payTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const payTriggeredRef = useRef<number | null>(null)
  const payDeadlineRef = useRef(0)

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
    payDeadlineRef.current = 0
    setPayCountdown(0)
  }, [])

  const startPayCountdown = useCallback((deadlineMs: number) => {
    stopPayCountdown()
    payDeadlineRef.current = deadlineMs

    const tick = () => {
      const remaining = remainingPaymentSeconds(payDeadlineRef.current)
      setPayCountdown(remaining)
      if (remaining <= 0) {
        if (payTimerRef.current) clearInterval(payTimerRef.current)
        payTimerRef.current = null
        payDeadlineRef.current = 0
      }
    }
    tick()
    if (remainingPaymentSeconds(deadlineMs) > 0) {
      payTimerRef.current = setInterval(tick, 1000)
    }
  }, [stopPayCountdown])

  const loadPaymentOrder = useCallback(async (targetAuctionId: number): Promise<Order | null> => {
    const orders = await listBuyerOrders(targetAuctionId)
    return orders.find(order => order.auctionId === targetAuctionId) || orders[0] || null
  }, [])

  const closePayModal = useCallback(() => {
    setShowPayModal(false)
  }, [])

  const soldWinnerAuctionId = auction?.status === 'sold' && auction.winnerUserId === userId
    ? auction.id
    : null

  // 成交后自动弹出支付倒计时（每个成交竞拍只触发一次）
  useEffect(() => {
    if (!soldWinnerAuctionId || paidAuctionIds.includes(soldWinnerAuctionId)) return
    if (payTriggeredRef.current === soldWinnerAuctionId && payOrder?.auctionId === soldWinnerAuctionId) return

    let cancelled = false
    loadPaymentOrder(soldWinnerAuctionId)
      .then(order => {
        if (cancelled) return
        if (!order) {
          Toast.error('订单未生成，请稍后重试')
          return
        }
        if (order.status === 'paid') {
          setPayOrder(order)
          payTriggeredRef.current = soldWinnerAuctionId
          setPaidAuctionIds(prev => prev.includes(soldWinnerAuctionId) ? prev : [...prev, soldWinnerAuctionId])
          stopPayCountdown()
          return
        }
        setPayOrder(order)
        payTriggeredRef.current = soldWinnerAuctionId
        startPayCountdown(paymentDeadlineMs(order.createdAt))
        setShowPayModal(true)
      })
      .catch((err: any) => {
        if (!cancelled) Toast.error(err.message || '加载订单失败')
      })

    return () => { cancelled = true }
  }, [loadPaymentOrder, paidAuctionIds, payOrder?.auctionId, soldWinnerAuctionId, startPayCountdown, stopPayCountdown])

  useEffect(() => () => stopPayCountdown(), [stopPayCountdown])

  useEffect(() => {
    if (!showPayModal) return
    listAddresses()
      .then(list => {
        setAddresses(list)
        if (selectedAddrId && !list.some(addr => addr.id === selectedAddrId)) {
          setSelectedAddrId(null)
        }
      })
      .catch(() => {
        setAddresses([])
        setSelectedAddrId(null)
      })
  }, [showPayModal, selectedAddrId])

  const selectedAddress = useMemo(
    () => addresses.find(addr => addr.id === selectedAddrId) || null,
    [addresses, selectedAddrId],
  )

  const openPayModal = useCallback(async () => {
    if (!auction || auction.status !== 'sold' || auction.winnerUserId !== userId || paidAuctionIds.includes(auction.id)) return
    try {
      const order = payOrder?.auctionId === auction.id ? payOrder : await loadPaymentOrder(auction.id)
      if (!order) {
        Toast.error('订单未生成，请稍后重试')
        return
      }
      if (order.status === 'paid') {
        setPayOrder(order)
        setPaidAuctionIds(prev => prev.includes(auction.id) ? prev : [...prev, auction.id])
        Toast.success('支付成功！')
        return
      }
      setPayOrder(order)
      startPayCountdown(paymentDeadlineMs(order.createdAt))
      setShowPayModal(true)
    } catch (err: any) {
      Toast.error(err.message || '加载订单失败')
    }
  }, [auction, loadPaymentOrder, paidAuctionIds, payOrder, startPayCountdown, userId])

  const handlePay = async () => {
    if (!auction) return
    if (!selectedAddress) {
      Toast.error('请先选择收货地址')
      return
    }
    if (payCountdown === 0) {
      Toast.error('支付已超时')
      return
    }
    setPayLoading(true)
    try {
      const order = payOrder?.auctionId === auction.id ? payOrder : await loadPaymentOrder(auction.id)
      if (!order) {
        Toast.error('订单未生成，请稍后重试')
        setPayLoading(false)
        return
      }
      if (order.status === 'paid') {
        setPayOrder(order)
        setPaidAuctionIds(prev => prev.includes(auction.id) ? prev : [...prev, auction.id])
        Toast.success('支付成功！')
        setShowPayModal(false)
        stopPayCountdown()
        return
      }
      const snapshot = `${selectedAddress.name} ${selectedAddress.phone} ${selectedAddress.province}${selectedAddress.city}${selectedAddress.district}${selectedAddress.detail}`
      const paidOrder = await payBuyerOrder(order.id, selectedAddress.id, snapshot)
      setPayOrder(paidOrder)
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
  const isCurrentPaid = paidAuctionIds.includes(auction.id) || payOrder?.status === 'paid'

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
          <div>{auction.status === 'sold' ? '竞拍已成交' : auction.status === 'failed' ? '竞拍流拍' : '竞拍未开始'}</div>
          {auction.status === 'sold' && auction.winnerUserId === userId && (
            <button
              className="bid-btn-primary detail-pay-btn"
              disabled={isCurrentPaid}
              onClick={openPayModal}
            >
              {isCurrentPaid ? '支付成功' : '立即支付'}
            </button>
          )}
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
                ⏰ 支付倒计时: {formatPaymentCountdown(payCountdown)}
              </div>
              <div style={{ marginTop: 12 }}>
                <div style={{ fontSize: 12, color: 'var(--text-muted)', marginBottom: 8 }}>选择收货地址</div>
                {addresses.length === 0 ? (
                  <div style={{
                    padding: 10,
                    borderRadius: 8,
                    border: '1px solid rgba(255,152,0,.2)',
                    background: 'rgba(255,152,0,.08)',
                    color: '#ffa726',
                    fontSize: 12,
                    textAlign: 'center',
                  }}>
                    暂无地址，请先到地址页添加
                  </div>
                ) : (
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8, maxHeight: 148, overflowY: 'auto' }}>
                    {addresses.map(addr => (
                      <button
                        key={addr.id}
                        type="button"
                        onClick={() => setSelectedAddrId(addr.id)}
                        style={{
                          textAlign: 'left',
                          padding: 10,
                          borderRadius: 8,
                          border: selectedAddrId === addr.id ? '1px solid var(--green)' : '1px solid var(--glass-border)',
                          background: selectedAddrId === addr.id ? 'rgba(37,199,120,.12)' : 'rgba(255,255,255,.04)',
                          color: 'var(--text-primary)',
                          cursor: 'pointer',
                        }}
                      >
                        <div style={{ fontSize: 13, fontWeight: 700 }}>{addr.name} {addr.phone}</div>
                        <div style={{ marginTop: 3, fontSize: 12, color: 'var(--text-secondary)' }}>
                          {addr.province}{addr.city}{addr.district}{addr.detail}
                        </div>
                      </button>
                    ))}
                  </div>
                )}
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
                disabled={payLoading || payCountdown === 0 || !selectedAddress}
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
