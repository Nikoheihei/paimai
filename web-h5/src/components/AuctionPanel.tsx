/**
 * AuctionPanel — 竞拍主面板（沉浸式直播风格）
 *
 * 仿抖音/快手直播间的出价卡片：
 * - 毛玻璃背景，浮在视频上方
 * - 商品信息 + 价格大字展示 + 快捷出价 + 排行榜
 * - 出价成功动效 / 被超越提醒 / 延时提示
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import type { Auction, RankingItem } from '../shared/types'
import { getAuction as apiGetAuction, getRanking, getRoomAuctions as apiGetRoomAuctions, placeBid } from '../api/client'

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
  onAuctionEnd?: (auction: Auction) => void
  onOutbid?: () => void
  onBidSuccess?: () => void
}

function fmt(cents: number): string { return (cents / 100).toFixed(2) }

function priceLabel(a: Auction): string {
  switch (a.status) {
    case 'sold':      return '落槌价'
    case 'running':   return a.currentPriceCents > 0 ? '当前最高价' : '起拍价'
    case 'scheduled': return '起拍价'
    default:          return '-'
  }
}

function priceCents(a: Auction): number {
  return a.status === 'sold' ? a.currentPriceCents
    : a.currentPriceCents > 0 ? a.currentPriceCents
    : a.startPriceCents
}

export default function AuctionPanel({
  roomId, userId, wsMessage, connected,
  activeAuctionId, productName, productImage,
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

  const prevStatusRef = useRef<string>('')
  const idemCounter = useRef(0)

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

  // 刷新详情 + 排行
  useEffect(() => {
    if (!current) return
    fetchAuction(current.id).then(setCurrent)
    getRanking(current.id, 10).then(setRanking)
  }, [current?.id])

  // WS 消息处理
  useEffect(() => {
    if (!wsMessage || !current) return
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
      try { navigator.vibrate?.(200) } catch {}
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
  }, [wsMessage])

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
        setBidMsg(res.sold ? '\u{1F389} 成交！' : res.extended ? '\u23F1 出价成功，时间延长' : '\u2705 出价成功')
        setLastOutbid(false); setPriceAnim(true); setTimeout(()=>setPriceAnim(false),600)
        fetchAuction(current.id).then(setCurrent)
        getRanking(current.id, 10).then(setRanking)
        onBidSuccess?.()
      } else {
        setBidStatus('fail')
        setBidMsg(res.tooFrequent ? '\u23F3 出价过快，稍后再试' : '\u274C 出价被拒绝')
      }
    } catch (e: any) { setBidStatus('fail'); setBidMsg(e.message||'出价失败') }
  }, [current, userId, bidAmount])

  const quickBid = useCallback((extra: number) => {
    if (!current) return
    setBidAmount(fmt(current.currentPriceCents + extra))
  }, [current])

  if (!current) {
    return <div style={{ textAlign:'center', padding:'48px 20px', color:'var(--text-muted)' }}>
      <div style={{ fontSize:40, marginBottom:8 }}>&#128722;</div>
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
        {productImage ? (
          <img src={productImage} alt="" style={{ width:54,height:54,borderRadius:14,objectFit:'cover',background:'rgba(255,255,255,.04)',border:'1px solid var(--glass-border)' }} />
        ) : (
          <div style={{ width:54,height:54,borderRadius:14,background:'rgba(254,44,85,.1)',display:'flex',alignItems:'center',justifyContent:'center',fontSize:24,border:'1px solid rgba(254,44,85,.15)' }}>
            &#128722;
          </div>
        )}
        <div style={{ flex:1, minWidth:0 }}>
          <div className="ap-product-name">{productName || `商品 #${current.productId}`}</div>
          <div style={{ display:'flex', alignItems:'center', gap:6, marginTop:4 }}>
            <span className="ap-mode-tag">{current.mode==='sudden_death'?'\u26A1 绝杀':'\uD83D\uDD04 延时'}</span>
            <StatusBadge status={current.status} size="small" />
          </div>
        </div>
      </div>

      {/* 被超越提醒 */}
      {lastOutbid && (
        <div className="outbid-alert-bar">
          &#9889; 您已被超越！当前最高价已更新
        </div>
      )}

      {/* 延时提醒 */}
      {extendHint && (
        <div className="extend-hint-bar">
          &#x23F0; 倒计时已延长！有人最后时刻出价
        </div>
      )}

      {/* 倒计时 */}
      {(isRunning || isScheduled) && (
        <Countdown endAt={current.endAt}
          onEnd={() => fetchAuction(current.id).then(setCurrent)} />
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
            {current.winnerUserId === userId ? '\uD83C\uDFC6 恭喜您得标！' : `已成交`}
          </div>
          <div className="winner-price">&yen;{fmt(current.currentPriceCents)}</div>
        </div>
      )}

      {/* 出价操作（仅 running） */}
      {isRunning && (
        <div className="bid-actions">
          {/* 快捷按钮行 */}
          <div className="quick-bids">
            <button className="quick-bid-btn" onClick={()=>quickBid(current.bidIncrementCents)}>
              +&yen;{fmt(current.bidIncrementCents)}
            </button>
            <button className="quick-bid-btn" onClick={()=>quickBid(current.bidIncrementCents*2)}>
              +&yen;{fmt(current.bidIncrementCents*2)}
            </button>
            <button className="quick-bid-btn" onClick={()=>quickBid(current.bidIncrementCents*5)}>
              +&yen;{fmt(current.bidIncrementCents*5)}
            </button>
          </div>

          {/* 输入 + 提交 */}
          <div className="bid-input-row">
            <span className="bid-currency">&yen;</span>
            <input type="number" step="0.01" placeholder="输入出价金额"
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

      {/* 排行榜 */}
      <div className="ranking-section">
        <div className="ranking-header">
          <h4>\uD83C\uDFC6 排行 TOP{ranking.length}</h4>
          <span style={{ fontSize:11,color:'var(--text-muted)' }}>{connected?'\u2705 实时':'\u23F3 同步中'}</span>
        </div>
        {ranking.length===0 ? (
          <div style={{textAlign:'center',padding:'16px',color:'var(--text-muted)',fontSize:13 }}>暂无出价，来当第一人！</div>
        ) : (
          <ul className="ranking-list">
            {ranking.map(item=>(
              <li key={item.rank} className={`ranking-item ${item.userId===userId?'me':''}`}>
                <span className="rank-num">#{item.rank}</span>
                <div className="ranking-user">
                  <div className="ranking-user-name">{item.userId===userId?'\u6211 \uD83D\uDC49':`用户${item.userId}`}</div>
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
