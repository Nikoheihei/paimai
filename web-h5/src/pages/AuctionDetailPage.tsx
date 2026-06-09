import { useState, useEffect, useCallback, useMemo } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { getAuction, getRanking, listAddresses, type Auction, type RankingItem, type Address } from '../api/client'
import { useWebSocket } from '../hooks/useWebSocket'
import { usePaymentFlow } from '../hooks/usePaymentFlow'
import { useBidAction } from '../hooks/useBidAction'
import Countdown from '../components/Countdown'
import BottomDrawer from '../components/ui/BottomDrawer'
import { formatPaymentCountdown } from '../utils/paymentDeadline'
import { parseUserIdFromToken } from '../utils/auth'
import type { WsMessage } from '../hooks/useWebSocket'
import Toast from '../components/Toast'
import './AuctionDetailPage.css'

function formatCents(c: number) { return (c / 100).toFixed(2) }

function wsEventAuctionId(message: WsMessage): number | undefined {
  const data = message.data as { auctionId?: number; payload?: { auctionId?: number } } | undefined
  return data?.auctionId ?? data?.payload?.auctionId
}

function wsEventBuyerId(message: WsMessage): number | undefined {
  const data = message.data as { buyerId?: number; payload?: { buyerId?: number } } | undefined
  return data?.buyerId ?? data?.payload?.buyerId
}

export default function AuctionDetailPage() {
  const { auctionId: auctionIdStr } = useParams<{ auctionId: string }>()
  const auctionId = Number(auctionIdStr)
  const navigate = useNavigate()
  const onBack = useCallback(() => navigate(-1), [navigate])

  const userId = useMemo(() => parseUserIdFromToken(), [])
  const [auction, setAuction] = useState<Auction | null>(null)
  const [ranking, setRanking] = useState<RankingItem[]>([])
  const [customAmount, setCustomAmount] = useState('')
  const [addresses, setAddresses] = useState<Address[]>([])
  const [selectedAddrId, setSelectedAddrId] = useState<number | null>(null)

  const load = useCallback(() => {
    getAuction(auctionId).then(setAuction).catch(() => {})
    getRanking(auctionId, 20).then(setRanking).catch(() => {})
  }, [auctionId])

  useEffect(() => { load() }, [load])

  const {
    showPayDrawer,
    payCountdown,
    payOrder,
    payLoading,
    paidAuctionIds,
    triggerPaymentFlow,
    openPayDrawer,
    closePayDrawer,
    executePay,
    handleWsOrderEvent
  } = usePaymentFlow(auctionId, userId, auction?.winnerUserId || null)

  const { bidStatus, handleBid } = useBidAction(auctionId, userId, load)

  const { connected: _connected } = useWebSocket(auction?.roomId || 0, userId, {
    onMessage: (msg) => {
      const msgType = msg.type
      if (['bid.accepted', 'price.updated', 'auction.extended', 'auction.ended'].includes(msgType)) {
        load()
      } else if (msgType === 'order.paid' || msgType === 'order.closed' || msgType === 'auction.payment_timeout') {
        const eventAuctionId = wsEventAuctionId(msg)
        const eventBuyerId = wsEventBuyerId(msg)
        handleWsOrderEvent(msgType, eventAuctionId, eventBuyerId, load)
      }
    }
  })

  // Trigger payment flow when auction ends and user is winner
  useEffect(() => {
    if (auction?.status === 'sold' && auction.winnerUserId === userId) {
      triggerPaymentFlow(auction.id)
    }
  }, [auction, userId, triggerPaymentFlow])

  useEffect(() => {
    if (!showPayDrawer) return
    listAddresses().then(list => {
      setAddresses(list)
      if (selectedAddrId && !list.some(addr => addr.id === selectedAddrId)) {
        setSelectedAddrId(null)
      }
    }).catch(() => {
      setAddresses([])
      setSelectedAddrId(null)
    })
  }, [showPayDrawer, selectedAddrId])

  const selectedAddress = useMemo(
    () => addresses.find(addr => addr.id === selectedAddrId) || null,
    [addresses, selectedAddrId]
  )

  const onConfirmPay = () => {
    if (!selectedAddress) {
      Toast.error('请选择收货地址')
      return
    }
    const snapshot = `${selectedAddress.name} ${selectedAddress.phone} ${selectedAddress.province}${selectedAddress.city}${selectedAddress.district}${selectedAddress.detail}`
    executePay(selectedAddress.id, snapshot, load)
  }

  const quickBidAmount = useMemo(() => {
    if (!auction) return 0
    const base = auction.currentPriceCents > 0 ? auction.currentPriceCents : auction.startPriceCents
    return base + auction.bidIncrementCents
  }, [auction])

  if (!auction) {
    return (
      <div className="auction-detail-layout skeleton-loader">
        <div className="skeleton-header" />
        <div className="skeleton-price" />
        <div className="skeleton-history" />
      </div>
    )
  }

  const isRunning = auction.status === 'running'
  const price = auction.currentPriceCents > 0 ? auction.currentPriceCents : auction.startPriceCents
  const isCurrentPaid = paidAuctionIds.includes(auction.id) || payOrder?.status === 'paid'

  return (
    <div className="auction-detail-layout">
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
        <button
          className="admin-btn"
          style={{ marginTop: 12 }}
          onClick={() => navigate('/agents')}
        >
          🤖 派 Agent 帮我自动出价
        </button>
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
                {isCurrentPaid && item.userId === userId && (
                  <span className="history-paid-badge">已支付</span>
                )}
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
              placeholder="自定义金额"
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
          <div>{auction.status === 'sold' ? '竞拍已成交' : auction.status === 'payment_timeout' ? '支付超时，成交失效' : auction.status === 'failed' ? '竞拍流拍' : '竞拍未开始'}</div>
          {auction.status === 'sold' && auction.winnerUserId === userId && (
            <button
              className="bid-btn-primary detail-pay-btn"
              disabled={isCurrentPaid}
              onClick={openPayDrawer}
            >
              {isCurrentPaid ? '支付成功' : '立即支付'}
            </button>
          )}
        </div>
      )}

      {/* 支付抽屉 */}
      <BottomDrawer open={showPayDrawer} onClose={closePayDrawer} title="确认支付">
        <div className="pay-drawer-body">
          <div className="pay-amount-box">
            <div className="label">成交价</div>
            <div className="amount">¥{formatCents(auction.currentPriceCents)}</div>
          </div>
          <div className="pay-countdown-alert">
            ⏰ 支付倒计时: {formatPaymentCountdown(payCountdown)}
          </div>

          <div className="address-section">
            <div className="section-title">选择收货地址</div>
            {addresses.length === 0 ? (
              <div className="empty-address">暂无地址，请先到地址页添加</div>
            ) : (
              <div className="address-options">
                {addresses.map(addr => (
                  <button
                    key={addr.id}
                    type="button"
                    className={`address-option-btn ${selectedAddrId === addr.id ? 'active' : ''}`}
                    onClick={() => setSelectedAddrId(addr.id)}
                  >
                    <div className="addr-name-row">
                      <span className="name">{addr.name}</span>
                      <span className="phone">{addr.phone}</span>
                    </div>
                    <div className="addr-detail-row">
                      {addr.province}{addr.city}{addr.district}{addr.detail}
                    </div>
                  </button>
                ))}
              </div>
            )}
          </div>

          {payCountdown === 0 && <div className="timeout-text">订单已关闭</div>}

          <div className="pay-drawer-actions">
            <button className="btn-cancel" onClick={closePayDrawer}>稍后再付</button>
            <button
              className="btn-confirm"
              disabled={payLoading || payCountdown === 0 || addresses.length === 0}
              onClick={onConfirmPay}
            >
              {payLoading ? '支付中...' : '立即支付'}
            </button>
          </div>
        </div>
      </BottomDrawer>
    </div>
  )
}
