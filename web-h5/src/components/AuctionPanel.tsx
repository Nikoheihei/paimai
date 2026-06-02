/**
 * AuctionPanel — 竞拍主面板
 * 显示商品信息、当前价、倒计时、出价操作和排行榜
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import type { Auction, RankingItem } from '../api/client'
import { getAuction, getRanking, getRoomAuctions, placeBid } from '../api/client'
import type { WsMessage } from '../hooks/useWebSocket'

type Props = {
  roomId: number
  userId: number
  wsMessage: WsMessage | null
  connected: boolean
}

function formatCents(cents: number): string {
  return (cents / 100).toFixed(2)
}

function formatEndAt(endAt: string): string {
  const d = new Date(endAt)
  return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

export default function AuctionPanel({ roomId, userId, wsMessage, connected }: Props) {
  const [current, setCurrent] = useState<Auction | null>(null)
  const [ranking, setRanking] = useState<RankingItem[]>([])
  const [bidAmount, setBidAmount] = useState('')
  const [bidStatus, setBidStatus] = useState<'idle' | 'sending' | 'ok' | 'fail'>('idle')
  const [bidMsg, setBidMsg] = useState('')
  const [lastOutbid, setLastOutbid] = useState(false)
  const [countdown, setCountdown] = useState('')
  const tickRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const idemCounter = useRef(0)

  // 初始加载
  useEffect(() => {
    getRoomAuctions(roomId, 'running').then((list) => {
      if (list.length > 0) setCurrent(list[0])
    })
  }, [roomId])

  // 选中竞拍时加载详情
  useEffect(() => {
    if (!current) return
    getAuction(current.id).then(setCurrent)
    getRanking(current.id, 10).then(setRanking)
  }, [current?.id])

  // WS 消息处理
  useEffect(() => {
    if (!wsMessage || !current) return
    if (wsMessage.type === 'bid.accepted') {
      getAuction(current.id).then(setCurrent)
      getRanking(current.id, 10).then(setRanking)
    }
  }, [wsMessage])

  // 倒计时
  useEffect(() => {
    if (!current || current.status !== 'running') return
    tickRef.current = setInterval(() => {
      const diff = new Date(current.endAt).getTime() - Date.now()
      if (diff <= 0) {
        setCountdown('00:00')
        getAuction(current.id).then(setCurrent)
        return
      }
      const s = Math.floor(diff / 1000)
      const m = Math.floor(s / 60)
      setCountdown(`${String(m).padStart(2, '0')}:${String(s % 60).padStart(2, '0')}`)
    }, 1000)
    return () => clearInterval(tickRef.current)
  }, [current?.id, current?.status, current?.endAt])

  // 出价
  const handleBid = useCallback(async () => {
    if (!current) return
    const amount = parseInt(bidAmount, 10)
    if (isNaN(amount) || amount <= 0) {
      setBidStatus('fail')
      setBidMsg('输入有效金额')
      return
    }
    const amountCents = Math.round(amount * 100)
    idemCounter.current++
    setBidStatus('sending')
    setBidMsg('')
    try {
      const key = `${userId}-${current.id}-${Date.now()}-${idemCounter.current}`
      const res = await placeBid(current.id, userId, amountCents, key)
      if (res.accepted) {
        setBidStatus('ok')
        setBidMsg(res.sold ? '🎉 成交！' : res.extended ? '⏱ 出价成功，时间延长' : '✅ 出价成功')
        setLastOutbid(false)
        // 刷新数据
        getAuction(current.id).then(setCurrent)
        getRanking(current.id, 10).then(setRanking)
      } else {
        setBidStatus('fail')
        setBidMsg(res.tooFrequent ? '⏳ 出价过快，稍后再试' : '❌ 出价被拒绝')
      }
    } catch (e: any) {
      setBidStatus('fail')
      setBidMsg(e.message || '出价失败')
    }
  }, [current, userId, bidAmount])

  // 快捷出价
  const quickBid = useCallback((extra: number) => {
    if (!current) return
    setBidAmount(formatCents(current.currentPriceCents + extra))
  }, [current])

  if (!current) {
    return <div className="panel pending">加载竞拍中…</div>
  }

  const isSold = current.status === 'sold'
  const isRunning = current.status === 'running'
  const countdownUrgent = isRunning && countdown.startsWith('00:') && countdown !== '00:00'

  return (
    <>
      {/* 连接状态 */}
      <div className="connection-indicator">
        <span className={`dot ${connected ? 'green' : 'red'}`} />
        {connected ? '已连接' : '重连中…'}
        {lastOutbid && <span className="outbid-alert">⚡ 被超越</span>}
      </div>

      {/* 竞拍面板 */}
      <div className="panel auction-panel">
        <h2 className="product-name">商品 #{current.productId}</h2>
        <p className="mode-tag">
          {current.mode === 'sudden_death' ? '⚡ 绝杀模式' : '🔄 延时模式'}
        </p>

        {/* 倒计时 */}
        {isRunning && (
          <div className={`countdown-section ${countdownUrgent ? 'urgent' : ''}`}>
            <span className="countdown-label">剩余时间</span>
            <span className="countdown-value">{countdown}</span>
          </div>
        )}

        {/* 当前价 */}
        <div className="price-section">
          <span className="price-label">{isSold ? '成交价' : '当前价'}</span>
          <span className={`price-value ${isSold ? 'sold' : ''}`}>
            ¥{formatCents(current.currentPriceCents)}
          </span>
        </div>

        <div className="info-row">
          <span>加价幅度: ¥{formatCents(current.bidIncrementCents)}</span>
          {current.capPriceCents > 0 && (
            <span>封顶价: ¥{formatCents(current.capPriceCents)}</span>
          )}
        </div>

        <div className="info-row">
          <span>状态: {statusLabel(current.status)}</span>
          <span>结束: {formatEndAt(current.endAt)}</span>
        </div>

        {/* 成交信息 */}
        {isSold && current.winnerUserId && (
          <div className="winner-section">
            🏆 用户 {current.winnerUserId} 以 ¥{formatCents(current.currentPriceCents)} 成交
          </div>
        )}
      </div>

      {/* 出价区域 */}
      {isRunning && (
        <div className="panel bid-panel">
          <h3>出价</h3>
          <div className="quick-bids">
            <button onClick={() => quickBid(current.bidIncrementCents)}>+{formatCents(current.bidIncrementCents)}</button>
            <button onClick={() => quickBid(current.bidIncrementCents * 2)}>+{formatCents(current.bidIncrementCents * 2)}</button>
            <button onClick={() => quickBid(current.bidIncrementCents * 5)}>+{formatCents(current.bidIncrementCents * 5)}</button>
          </div>
          <div className="bid-input-row">
            <span className="currency">¥</span>
            <input
              type="number"
              step="0.01"
              placeholder="输入金额"
              value={bidAmount}
              onChange={(e) => setBidAmount(e.target.value)}
              disabled={bidStatus === 'sending'}
            />
            <button
              className="bid-btn"
              onClick={handleBid}
              disabled={bidStatus === 'sending'}
            >
              {bidStatus === 'sending' ? '…' : '出价'}
            </button>
          </div>
          {bidMsg && (
            <p className={`bid-status ${bidStatus === 'ok' ? 'ok' : 'fail'}`}>
              {bidMsg}
            </p>
          )}
        </div>
      )}

      {/* 排行榜 */}
      <div className="panel ranking-panel">
        <h3>🏆 排行榜</h3>
        {ranking.length === 0 ? (
          <p className="empty">暂无出价</p>
        ) : (
          <ol className="ranking-list">
            {ranking.map((item) => (
              <li key={item.rank} className={`ranking-item ${item.userId === userId ? 'me' : ''}`}>
                <span className="rank">#{item.rank}</span>
                <span className="user">
                  {item.userId === userId ? '我' : `用户 ${item.userId}`}
                </span>
                <span className="amount">¥{formatCents(item.amountCents)}</span>
              </li>
            ))}
          </ol>
        )}
      </div>
    </>
  )
}

function statusLabel(s: string): string {
  switch (s) {
    case 'draft': return '草稿'
    case 'scheduled': return '待开始'
    case 'running': return '进行中'
    case 'sold': return '已成交 🎉'
    case 'failed': return '流拍'
    case 'cancelled': return '已取消'
    default: return s
  }
}
