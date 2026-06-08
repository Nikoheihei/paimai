/**
 * AuctionPanel — 竞拍主面板（沉浸式直播风格）
 *
 * 仿抖音/快手直播间的出价卡片：
 * - 毛玻璃背景，浮在视频上方
 * - 商品信息 + 价格大字展示 + 快捷出价 + 排行榜
 * - 出价成功动效 / 被超越提醒 / 延时提示
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import type { Auction, RankingItem } from '../shared/types'
import { getAuction as apiGetAuction, getRanking, getRoomAuctions as apiGetRoomAuctions, placeBid, payBuyerOrder, listBuyerOrders, type Order } from '../api/client'
import { formatPaymentCountdown, paymentDeadlineMs, PAYMENT_WINDOW_SECONDS, remainingPaymentSeconds } from '../utils/paymentDeadline'

/** 将 API 返回的 Auction 转换为完整的 shared types Auction */
function fetchAuction(id: number): Promise<Auction> {
  return apiGetAuction(id).then(a => a as unknown as Auction)
}
function fetchRoomAuctions(roomId: number, status?: string): Promise<Auction[]> {
  return apiGetRoomAuctions(roomId, status).then(list => list as unknown as Auction[])
}
import type { WsMessage } from '../hooks/useWebSocket'
import StatusBadge from './StatusBadge'
import Countdown from './Countdown'

type Props = {
  roomId: number
  userId: number
  wsMessage: WsMessage | null
  connected: boolean
  activeAuctionId?: number | null
  productName?: string
  productImage?: string
  paidAuctionIds?: number[]
  onPaid?: (auctionId: number) => void
  selectedAddressId?: number | null
  selectedAddress?: { id: number; name: string; phone: string; province: string; city: string; district: string; detail: string } | null
  onAuctionEnd?: (auction: Auction) => void
  onOutbid?: () => void
  onBidSuccess?: () => void
}

function fmt(cents: number): string { return (cents / 100).toFixed(2) }

function priceLabel(a: Auction): string {
  switch (a.status) {
    case 'sold':      return '落槌价'
    case 'payment_timeout': return '失效成交价'
    case 'running':   return a.currentPriceCents > 0 ? '当前最高价' : '起拍价'
    case 'scheduled': return '起拍价'
    default:          return '-'
  }
}

function priceCents(a: Auction): number {
  return (a.status === 'sold' || a.status === 'payment_timeout') ? a.currentPriceCents
    : a.currentPriceCents > 0 ? a.currentPriceCents
    : a.startPriceCents
}

function modeLabel(mode: Auction['mode']): string {
  if (mode === 'extension') return '延时'
  if (mode === 'reserve') return '保底价'
  return '绝杀'
}

function wsEventAuctionId(message: WsMessage): number | undefined {
  const data = message.data as { auctionId?: number; payload?: { auctionId?: number } } | undefined
  return data?.auctionId ?? data?.payload?.auctionId
}

function wsEventBuyerId(message: WsMessage): number | undefined {
  const data = message.data as { buyerId?: number; payload?: { buyerId?: number } } | undefined
  return data?.buyerId ?? data?.payload?.buyerId
}

export default function AuctionPanel({
  roomId, userId, wsMessage, connected,
  activeAuctionId, productName, productImage,
  paidAuctionIds = [], onPaid,
  selectedAddressId, selectedAddress,
  onAuctionEnd, onOutbid, onBidSuccess,
}: Props) {
  const [current, setCurrent] = useState<Auction | null>(null)
  const [ranking, setRanking] = useState<RankingItem[]>([])
  const [bidAmount, setBidAmount] = useState('')
  const [bidStatus, setBidStatus] = useState<'idle'|'sending'|'ok'|'fail'>('idle')
  const [bidMsg, setBidMsg] = useState('')
  const [lastOutbid, setLastOutbid] = useState(false)
  const [priceAnim, setPriceAnim] = useState(false)
  const [extendHint, setExtendHint] = useState(false)
  const [showPayModal, setShowPayModal] = useState(false)
  const [payCountdown, setPayCountdown] = useState(PAYMENT_WINDOW_SECONDS)
  const [payOrder, setPayOrder] = useState<Order | null>(null)
  const [payLoading, setPayLoading] = useState(false)

  const prevStatusRef = useRef<string>('')
  const idemCounter = useRef(0)
  const payTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const payTriggeredRef = useRef<number | null>(null)
  const payDeadlineRef = useRef(0) // 支付截止时间戳（毫秒），0 表示未开始
  const handledWsMessageRef = useRef<WsMessage | null>(null)

  // 初始加载
  useEffect(() => {
    if (activeAuctionId) {
      fetchAuction(activeAuctionId).then(setCurrent)
    } else {
      fetchRoomAuctions(roomId, 'running').then((list) => {
        if (list.length > 0) setCurrent(list[0])
      })
    }
  }, [roomId, activeAuctionId])

  const currentId = current?.id

  // 刷新详情 + 排行
  useEffect(() => {
    if (!currentId) return
    fetchAuction(currentId).then(setCurrent)
    getRanking(currentId, 10).then(setRanking)
  }, [currentId])

  // WS 消息处理
  useEffect(() => {
    if (!wsMessage || !current) return
    if (handledWsMessageRef.current === wsMessage) return
    handledWsMessageRef.current = wsMessage
    const t = wsMessage.type

    if (t === 'bid.accepted') {
      fetchAuction(current.id).then((updated: Auction) => {
        prevStatusRef.current = current.status; setCurrent(updated)
        setPriceAnim(true); setTimeout(() => setPriceAnim(false), 600)
        if (updated.mode === 'extension' && updated.endAt !== current.endAt) {
          setExtendHint(true); setTimeout(() => setExtendHint(false), 3000)
        }
      })
      getRanking(current.id, 10).then(setRanking)
    }

    if (t === 'outbid' || t === 'auction.outbid') {
      setLastOutbid(true); onOutbid?.()
      try { navigator.vibrate?.(200) } catch {
        // Vibration is best-effort and unavailable in some browsers.
      }
      setTimeout(() => setLastOutbid(false), 4000)
      fetchAuction(current.id).then(setCurrent)
      getRanking(current.id, 10).then(setRanking)
    }

    if (t === 'auction.extended') {
      setExtendHint(true); setTimeout(() => setExtendHint(false), 5000)
      fetchAuction(current.id).then(setCurrent)
    }

    if (t === 'auction.ended') {
      fetchAuction(current.id).then((updated) => {
        prevStatusRef.current = current.status; setCurrent(updated)
        if (updated.status === 'sold' || updated.status === 'failed') onAuctionEnd?.(updated)
      })
    }

    if (t === 'ranking.updated') getRanking(current.id, 10).then(setRanking)

    if (t === 'order.paid') {
      const eventAuctionId = wsEventAuctionId(wsMessage)
      if (eventAuctionId && eventAuctionId !== current.id) return
      fetchAuction(current.id).then(setCurrent)
      getRanking(current.id, 10).then(setRanking)
      if (wsEventBuyerId(wsMessage) === userId) {
        if (payTimerRef.current) {
          clearInterval(payTimerRef.current)
          payTimerRef.current = null
        }
        payDeadlineRef.current = 0
        setPayCountdown(0)
        setShowPayModal(false)
        onPaid?.(current.id)
        setBidStatus('ok')
        setBidMsg('支付成功！')
      }
    }
    if (t === 'order.closed' || t === 'auction.payment_timeout') {
      const eventAuctionId = wsEventAuctionId(wsMessage)
      if (eventAuctionId && eventAuctionId !== current.id) return
      fetchAuction(current.id).then(setCurrent)
      getRanking(current.id, 10).then(setRanking)
      window.dispatchEvent(new CustomEvent('order:refresh'))
      if (current.winnerUserId === userId || wsEventBuyerId(wsMessage) === userId) {
        if (payTimerRef.current) {
          clearInterval(payTimerRef.current)
          payTimerRef.current = null
        }
        payDeadlineRef.current = 0
        setPayCountdown(0)
        setShowPayModal(false)
        setBidStatus('fail')
        setBidMsg('支付超时，订单已关闭')
      }
    }
  }, [wsMessage, current, onAuctionEnd, onOutbid, onPaid, userId])

  // 出价
  const handleBid = useCallback(async () => {
    if (!current) return
    const amount = parseFloat(bidAmount)
    if (isNaN(amount) || amount <= 0) { setBidStatus('fail'); setBidMsg('输入有效金额'); return }
    idemCounter.current++
    setBidStatus('sending'); setBidMsg('')
    try {
      const key = `${userId}-${current.id}-${Date.now()}-${idemCounter.current}`
      const res = await placeBid(current.id, userId, Math.round(amount*100), key)
      if (res.accepted) {
        setBidStatus('ok')
        setBidMsg(res.sold ? '成交！' : res.extended ? '出价成功，时间延长' : '出价成功')
        setLastOutbid(false); setPriceAnim(true); setTimeout(()=>setPriceAnim(false),600)
        fetchAuction(current.id).then(setCurrent)
        getRanking(current.id, 10).then(setRanking)
        onBidSuccess?.()
      } else {
        setBidStatus('fail')
        setBidMsg(res.tooFrequent ? '出价过快，稍后再试' : '出价被拒绝')
      }
    } catch (e: any) { setBidStatus('fail'); setBidMsg(e.message||'出价失败') }
  }, [current, userId, bidAmount, onBidSuccess])

  // 快捷出价：仅更新输入框中的预计出价金额，不自动提交
  const handleQuickBid = useCallback(() => {
    if (!current) return
    const nextAmount = current.currentPriceCents + current.bidIncrementCents
    setBidAmount(fmt(nextAmount))
  }, [current])

  // 停止支付倒计时（完全清理）
  const stopPayCountdown = useCallback(() => {
    if (payTimerRef.current) {
      clearInterval(payTimerRef.current)
      payTimerRef.current = null
    }
    payDeadlineRef.current = 0
    setPayCountdown(0)
  }, [])

  // 启动支付倒计时（独立运行，不依赖弹窗状态）
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
    tick() // 立即计算一次
    if (remainingPaymentSeconds(deadlineMs) > 0) {
      payTimerRef.current = setInterval(tick, 1000)
    }
  }, [stopPayCountdown])

  const loadPaymentOrder = useCallback(async (auctionId: number): Promise<Order | null> => {
    const orders = await listBuyerOrders(auctionId)
    return orders.find(order => order.auctionId === auctionId) || orders[0] || null
  }, [])

  const handleCountdownEnd = useCallback(() => {
    if (!current) return
    fetchAuction(current.id).then(setCurrent)
  }, [current])

  // 关闭支付弹窗（倒计时继续在后台运行）
  const closePayModal = useCallback(() => {
    setShowPayModal(false)
    // 不停止倒计时，后台继续
  }, [])

  // 打开支付弹窗（从后台倒计时同步当前剩余时间）
  const openPayModal = useCallback(async () => {
    if (!current || paidAuctionIds.includes(current.id)) return
    try {
      const order = payOrder?.auctionId === current.id ? payOrder : await loadPaymentOrder(current.id)
      if (!order) {
        setBidStatus('fail')
        setBidMsg('订单未生成，请稍后重试')
        return
      }
      if (order.status === 'paid') {
        setPayOrder(order)
        onPaid?.(current.id)
        setBidStatus('ok')
        setBidMsg('支付成功！')
        getRanking(current.id, 10).then(setRanking)
        return
      }
      if (order.status === 'closed') {
        setPayOrder(order)
        stopPayCountdown()
        setBidStatus('fail')
        setBidMsg('支付超时，订单已关闭')
        fetchAuction(current.id).then(setCurrent)
        return
      }
      setPayOrder(order)
      startPayCountdown(paymentDeadlineMs(order.createdAt))
      setShowPayModal(true)
    } catch (err: any) {
      setBidStatus('fail')
      setBidMsg(err.message || '加载订单失败')
    }
  }, [current, loadPaymentOrder, onPaid, paidAuctionIds, payOrder, startPayCountdown, stopPayCountdown])

  const soldWinnerAuctionId = current?.status === 'sold' && current.winnerUserId === userId
    ? current.id
    : null
  const isCurrentPaid = current ? paidAuctionIds.includes(current.id) : false

  // 成交后自动启动支付倒计时 + 弹出弹窗（每个成交竞拍只触发一次）
  useEffect(() => {
    if (!soldWinnerAuctionId || paidAuctionIds.includes(soldWinnerAuctionId)) return
    if (payTriggeredRef.current === soldWinnerAuctionId && payOrder?.auctionId === soldWinnerAuctionId) return

    let cancelled = false
    loadPaymentOrder(soldWinnerAuctionId)
      .then(order => {
        if (cancelled) return
        if (!order) {
          setBidStatus('fail')
          setBidMsg('订单未生成，请稍后重试')
          return
        }
        if (order.status === 'paid') {
          setPayOrder(order)
          payTriggeredRef.current = soldWinnerAuctionId
          stopPayCountdown()
          onPaid?.(soldWinnerAuctionId)
          return
        }
        if (order.status === 'closed') {
          setPayOrder(order)
          payTriggeredRef.current = soldWinnerAuctionId
          stopPayCountdown()
          setShowPayModal(false)
          setBidStatus('fail')
          setBidMsg('支付超时，订单已关闭')
          fetchAuction(soldWinnerAuctionId).then(setCurrent)
          return
        }
        setPayOrder(order)
        payTriggeredRef.current = soldWinnerAuctionId
        startPayCountdown(paymentDeadlineMs(order.createdAt))
        setShowPayModal(true)
      })
      .catch((err: any) => {
        if (cancelled) return
        setBidStatus('fail')
        setBidMsg(err.message || '加载订单失败')
      })

    return () => { cancelled = true }
  }, [loadPaymentOrder, onPaid, paidAuctionIds, payOrder?.auctionId, soldWinnerAuctionId, startPayCountdown, stopPayCountdown])

  // 组件卸载时清理
  useEffect(() => () => stopPayCountdown(), [stopPayCountdown])

  const handlePay = async () => {
    if (!current) return
    if (!selectedAddressId || !selectedAddress) {
      setBidStatus('fail')
      setBidMsg('请先选择收货地址')
      return
    }
    if (payCountdown === 0) {
      setBidStatus('fail')
      setBidMsg('支付已超时')
      return
    }
    setPayLoading(true)
    try {
      const order = payOrder?.auctionId === current.id ? payOrder : await loadPaymentOrder(current.id)
      if (!order) {
        setBidMsg('订单未生成，请稍后重试')
        setPayLoading(false)
        return
      }
      if (order.status === 'paid') {
        setPayOrder(order)
        onPaid?.(current.id)
        setBidStatus('ok')
        setBidMsg('支付成功！')
        setShowPayModal(false)
        stopPayCountdown()
        getRanking(current.id, 10).then(setRanking)
        return
      }
      const addrSnapshot = `${selectedAddress.name} ${selectedAddress.phone} ${selectedAddress.province}${selectedAddress.city}${selectedAddress.district}${selectedAddress.detail}`
      const paidOrder = await payBuyerOrder(order.id, selectedAddressId ?? undefined, addrSnapshot)
      setPayOrder(paidOrder)
      onPaid?.(current.id)
      setBidStatus('ok')
      setBidMsg('支付成功！')
      setShowPayModal(false)
      stopPayCountdown()
      payTriggeredRef.current = current.id
      window.dispatchEvent(new CustomEvent('order:refresh'))
      fetchAuction(current.id).then(setCurrent)
      getRanking(current.id, 10).then(setRanking)
    } catch (err: any) {
      setBidMsg(err.message || '支付失败')
      if (String(err.message || '').includes('timeout')) {
        setShowPayModal(false)
        stopPayCountdown()
        fetchAuction(current.id).then(setCurrent)
        window.dispatchEvent(new CustomEvent('order:refresh'))
      }
    } finally {
      setPayLoading(false)
    }
  }

  if (!current) {
    return <div style={{ textAlign:'center', padding:'48px 20px', color:'var(--text-muted)' }}>
      <div style={{ fontSize:40, marginBottom:8 }}>[商品]</div>
      <div>加载竞拍中...</div>
    </div>
  }

  const isSold = current.status === 'sold'
  const isRunning = current.status === 'running'
  const isScheduled = current.status === 'scheduled'

  return (
    <div className="auction-panel">
      {/* 商品头部 */}
      <div style={{ display:'flex', alignItems:'center', gap:12, marginBottom:10 }}>
        {productImage && (
          <img
            src={productImage}
            alt={productName || `商品 #${current.productId}`}
            style={{ width: 52, height: 52, borderRadius: 8, objectFit: 'cover', flex: '0 0 auto' }}
          />
        )}
        <div style={{ flex:1, minWidth:0 }}>
          <div className="ap-product-name">
            {productName || `商品 #${current.productId}`}
            <span className="ap-mode-tag" style={{ marginLeft:8 }}>{modeLabel(current.mode)}</span>
          </div>
        </div>
      </div>

      {/* 被超越提醒 */}
      {lastOutbid && (
        <div className="outbid-alert-bar">
          您已被超越！当前最高价已更新
        </div>
      )}

      {/* 延时提醒 */}
      {extendHint && (
        <div className="extend-hint-bar">
          ⏰ 倒计时已延长！有人最后时刻出价
        </div>
      )}

      {/* 倒计时 */}
      {(isRunning || isScheduled) && (
        <Countdown endAt={current.endAt}
          onEnd={handleCountdownEnd} />
      )}

      {/* 价格区域 */}
      {(isRunning || isScheduled || isSold) && (
        <div className={`ap-price-section ${priceAnim?'':''}`} style={priceAnim?{animation:'pricePop .5s ease'}:{}}>
          <span className="ap-price-label">{priceLabel(current)}</span>
          <div className={`ap-price-value ${isSold?'sold':''} ${priceAnim?'animating':''}`}>
            &yen;{fmt(priceCents(current))}
          </div>
        </div>
      )}

      {/* 信息网格 */}
      <div className="ap-info-grid">
        <div className="ap-info-item">
          <span className="ap-info-label">加价幅度</span>
          <span className="ap-info-val">&yen;{fmt(current.bidIncrementCents)}</span>
        </div>
        {current.capPriceCents > 0 && (
          <div className="ap-info-item">
            <span className="ap-info-label">封顶</span>
            <span className="ap-info-val">&yen;{fmt(current.capPriceCents)}</span>
          </div>
        )}
        <div className="ap-info-item">
          <span className="ap-info-label">状态</span>
          <span className="ap-info-val"><StatusBadge status={current.status} size="small" /></span>
        </div>
      </div>

      {/* 成交信息 */}
      {isSold && (
        <div className="winner-banner">
          <div className="winner-text">
            {current.winnerUserId === userId ? '恭喜您得标！' : `已成交`}
          </div>
          <div className="winner-price">&yen;{fmt(current.currentPriceCents)}</div>
          {current.winnerUserId === userId && (
            <button
              className="bid-submit-btn instant-bid-btn"
              style={{ marginTop: 10, width: '100%' }}
              disabled={isCurrentPaid}
              onClick={openPayModal}
            >
              {isCurrentPaid ? '支付成功' : '立即支付'}
            </button>
          )}
        </div>
      )}

      {/* 支付弹窗 */}
      {showPayModal && current?.status === 'sold' && current.winnerUserId === userId && createPortal(
        <div className="modal-overlay" onClick={closePayModal}>
          <div className="modal-inner" onClick={e => e.stopPropagation()}>
            <div className="modal-title">恭喜成交！</div>
            <div className="modal-body">
              <div style={{ textAlign: 'center', marginBottom: 16 }}>
                <div style={{ fontSize: 13, color: 'var(--text-muted)' }}>成交价</div>
                <div style={{ fontSize: 32, fontWeight: 800, color: 'var(--primary)' }}>&yen;{fmt(current.currentPriceCents)}</div>
              </div>
              {/* 收货地址 */}
              {selectedAddress && (
                <div style={{
                  marginTop: 12, padding: '10px 12px',
                  background: 'rgba(255,255,255,.04)',
                  borderRadius: 8,
                  border: '1px solid var(--glass-border)',
                }}>
                  <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 4 }}>收货地址</div>
                  <div style={{ fontSize: 13, fontWeight: 600 }}>{selectedAddress.name} {selectedAddress.phone}</div>
                  <div style={{ fontSize: 12, color: 'var(--text-secondary)' }}>
                    {selectedAddress.province}{selectedAddress.city}{selectedAddress.district}{selectedAddress.detail}
                  </div>
                </div>
              )}
              {!selectedAddress && (
                <div style={{
                  marginTop: 12, padding: '10px 12px',
                  background: 'rgba(255,152,0,.08)',
                  borderRadius: 8,
                  border: '1px solid rgba(255,152,0,.15)',
                  textAlign: 'center',
                  fontSize: 12,
                  color: '#ffa726',
                }}>
                  未选择收货地址，请在右侧地址栏中选择
                </div>
              )}
              <div style={{ textAlign: 'center', fontSize: 14, color: '#ff4757', fontWeight: 700, marginTop: 10 }}>
                ⏰ 支付倒计时: {formatPaymentCountdown(payCountdown)}
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
                disabled={payLoading || payCountdown === 0 || !selectedAddressId || !selectedAddress}
                onClick={handlePay}
              >
                {payLoading ? '支付中...' : '立即支付'}
              </button>
            </div>
          </div>
        </div>,
        document.body,
      )}

      {/* 出价操作（仅 running） */}
      {isRunning && (
        <div className="bid-actions">
          {/* 快捷加价按钮：仅更新预计出价 */}
          <div className="quick-bids">
            <button className="quick-bid-btn" onClick={handleQuickBid}>
              +&yen;{fmt(current.bidIncrementCents)}
            </button>
          </div>

          {/* 输入 + 提交 */}
          <div className="bid-input-row">
            <span className="bid-currency">&yen;</span>
            <input type="text" inputMode="decimal" placeholder="输入出价金额"
              value={bidAmount} onChange={(e)=>setBidAmount(e.target.value)}
              disabled={bidStatus==='sending'}
            />
            <button className="bid-submit-btn instant-bid-btn"
              onClick={handleBid} disabled={bidStatus==='sending'}>
              {bidStatus==='sending'?'...':'出价'}
            </button>
          </div>
          {bidMsg && <div className={`bid-status-msg ${bidStatus}`}>{bidMsg}</div>}
        </div>
      )}

      {/* 返回直播按钮（关闭竞拍面板，回到视频直播画面） */}
      <button
        className="back-to-live-btn"
        onClick={() => window.dispatchEvent(new CustomEvent('auction:close'))}
        style={{
          width: '100%',
          padding: '10px',
          marginTop: 10,
          background: 'rgba(255,255,255,0.08)',
          border: '1px solid var(--glass-border)',
          borderRadius: 10,
          color: 'var(--text-muted)',
          fontSize: 13,
          cursor: 'pointer',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 6,
        }}
      >
        ← 返回直播间
      </button>

      {/* 排行榜 */}
      <div className="ranking-section">
        <div className="ranking-header">
          <h4>排行 TOP{ranking.length}</h4>
          <span style={{ fontSize:11,color:'var(--text-muted)' }}>{connected?'实时':'同步中'}</span>
        </div>
        {ranking.length===0 ? (
          <div style={{textAlign:'center',padding:'16px',color:'var(--text-muted)',fontSize:13 }}>暂无出价，来当第一人！</div>
        ) : (
          <ul className="ranking-list">
            {ranking.map(item=>(
              <li key={item.rank} className={`ranking-item ${item.userId===userId?'me':''}`}>
                <span className="rank-num">#{item.rank}</span>
                <div className="ranking-user">
                  <div className="ranking-user-line">
                    <div className="ranking-user-name">{item.userId===userId?'我':`用户${item.userId}`}</div>
                    {isCurrentPaid && item.userId === userId && (
                      <span className="ranking-paid-badge">已支付</span>
                    )}
                  </div>
                </div>
                <span className="ranking-amount">&yen;{fmt(item.amountCents)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  )
}
