/**
 * LiveRoomPage — 全屏沉浸式直播间页面
 */

import { useState, useEffect, useMemo, useCallback } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { useWebSocket } from '../hooks/useWebSocket'
import { getRoom, getRoomAuctions, type Auction as ApiAuction } from '../api/client'
import type { WsMessage } from '../hooks/useWebSocket'
import type { Auction, UserInfo } from '../shared/types'
import { useSound } from '../hooks/useSound'
import type { BarrageItem } from '../components/BarrageLayer'
import { parseUserIdFromToken } from '../utils/auth'

// 组件
import VideoPlayer from '../components/VideoPlayer'
import AnchorHeader from '../components/AnchorHeader'
import AuctionPanel from '../components/AuctionPanel'
import ProductFloatPanel from '../components/ProductFloatPanel'
import AddressFloatPanel, { type AddressItem } from '../components/AddressFloatPanel'
import AuctionResultModal from '../components/AuctionResultModal'
import Toast from '../components/Toast'
import BarrageSimLayer from '../components/ui/BarrageSimLayer'
import LiveViewerCount from '../components/ui/LiveViewerCount'

const AUCTION_REFRESH_EVENTS = new Set([
  'product.created',
  'product.offline',
  'auction.created',
  'auction.updated',
  'order.created',
  'auction.payment_timeout',
  'order.paid',
  'order.closed',
])

function wsEventAuctionId(message: WsMessage): number | undefined {
  const data = message.data as { auctionId?: number; payload?: { auctionId?: number } } | undefined
  return data?.auctionId ?? data?.payload?.auctionId
}

function wsEventBuyerId(message: WsMessage): number | undefined {
  const data = message.data as { buyerId?: number; payload?: { buyerId?: number } } | undefined
  return data?.buyerId ?? data?.payload?.buyerId
}

export default function LiveRoomPage() {
  const { roomId: roomIdStr } = useParams<{ roomId: string }>()
  const roomId = Number(roomIdStr)
  const navigate = useNavigate()
  const onBack = useCallback(() => navigate(-1), [navigate])

  const userId = useMemo(() => parseUserIdFromToken(), [])
  const [lastMessage, setLastMessage] = useState<WsMessage | null>(null)

  // 房间数据
  const [roomTitle, setRoomTitle] = useState('')
  const [coverUrl, setCoverUrl] = useState('')
  const [allAuctions, setAllAuctions] = useState<Auction[]>([])
  const [activeAuctionId, setActiveAuctionId] = useState<number | null>(null)
  const [autoOpenAuction, setAutoOpenAuction] = useState(true)

  const [endedAuction, setEndedAuction] = useState<Auction | null>(null)
  const [showResultModal, setShowResultModal] = useState(false)
  const [paidAuctionIds, setPaidAuctionIds] = useState<number[]>([])

  const handlePaid = useCallback((auctionId: number) => {
    setPaidAuctionIds(prev => prev.includes(auctionId) ? prev : [...prev, auctionId])
  }, [])

  const [selectedAddressId, setSelectedAddressId] = useState<number | null>(null)
  const [selectedAddress, setSelectedAddress] = useState<AddressItem | null>(null)

  const handleSelectAddress = useCallback((addr: AddressItem) => {
    setSelectedAddressId(addr.id)
    setSelectedAddress(addr)
  }, [])

  const [userBarrages, setUserBarrages] = useState<BarrageItem[]>([])
  const sound = useSound()

  const { connected, reconnect } = useWebSocket(roomId, userId, {
    onMessage: (msg) => {
      setLastMessage(msg)
      if (AUCTION_REFRESH_EVENTS.has(msg.type)) {
        loadRoomAuctions()
      }
      if (msg.type === 'order.created' || msg.type === 'order.paid' || msg.type === 'order.closed') {
        window.dispatchEvent(new CustomEvent('order:refresh'))
      }
      if (msg.type === 'order.paid') {
        const paidAuctionId = wsEventAuctionId(msg)
        if (paidAuctionId && wsEventBuyerId(msg) === userId) {
          handlePaid(paidAuctionId)
        }
      }
    },
    onConnected: () => console.log('[WS] 已连接'),
    onDisconnected: () => console.log('[WS] 已断开'),
  })

  const [anchorInfo, setAnchorInfo] = useState<UserInfo | null>(null)

  const loadRoomAuctions = useCallback(() => {
    getRoomAuctions(roomId).then((list: ApiAuction[]) => {
      setAllAuctions(list as unknown as Auction[])
      const running = list.find(a => a.status === 'running')
      if (running && !activeAuctionId && autoOpenAuction) setActiveAuctionId(running.id)
    })
  }, [roomId, activeAuctionId, autoOpenAuction])

  useEffect(() => {
    getRoom(roomId).then(r => {
      setRoomTitle(r.title); setCoverUrl(r.coverUrl)
      if (r.title) document.title = `${r.title} - 直播拍卖`
      if (r.anchorNickname) {
        setAnchorInfo({
          userId: r.sellerId,
          nickname: r.anchorNickname,
          avatarUrl: r.anchorAvatar || '',
          username: '',
          role: 'anchor',
        })
      }
    })
    loadRoomAuctions()
  }, [roomId, loadRoomAuctions])

  const handleCloseAuction = useCallback(()=>{
    setAutoOpenAuction(false)
    setActiveAuctionId(null)
  },[])

  useEffect(() => {
    window.addEventListener('auction:close', handleCloseAuction)
    return () => window.removeEventListener('auction:close', handleCloseAuction)
  }, [handleCloseAuction])

  const productNames = useMemo(() => {
    const map: Record<number,string> = {}
    allAuctions.forEach(a => {
      map[a.productId] = a.productName || `商品 #${a.productId}`
    })
    return map
  }, [allAuctions])

  const productImages = useMemo(() => {
    const map: Record<number,string> = {}
    allAuctions.forEach(a => {
      if (a.productImage) map[a.productId] = a.productImage
    })
    return map
  }, [allAuctions])

  const activeProductImage = activeAuctionId ? productImages[allAuctions.find(a=>a.id===activeAuctionId)?.productId||0] : undefined
  const activeProductName = activeAuctionId ? productNames[allAuctions.find(a=>a.id===activeAuctionId)?.productId||0] : undefined

  const handleSelectAuction = useCallback((auctionId:number)=>{
    setAutoOpenAuction(false)
    setActiveAuctionId(auctionId); setLastMessage(null)
  },[])

  const handleAuctionEnd = useCallback((auction:Auction)=>{
    setEndedAuction(auction); setShowResultModal(true)
    if (auction.winnerUserId === userId) {
      setTimeout(() => {
        window.dispatchEvent(new CustomEvent('order:refresh'))
      }, 1000)
    }
  },[userId])

  const handleResultClose = useCallback(()=>{
    const shouldKeepWinningAuction = endedAuction?.winnerUserId === userId
    setShowResultModal(false); setEndedAuction(null)
    if (shouldKeepWinningAuction) {
      if (endedAuction?.id) setActiveAuctionId(endedAuction.id)
      return
    }
    const next=allAuctions.find(a=>a.status==='running')
    if(next&&next.id!==activeAuctionId) setActiveAuctionId(next.id)
  },[allAuctions,activeAuctionId,endedAuction,userId])

  const handleSendBarrage = useCallback((text: string) => {
    if (!text.trim()) return
    setUserBarrages(prev => [...prev, {
      id: `self-${Date.now()}`,
      text: text.trim(),
      isSelf: true,
    }])
  }, [])

  if (!userId) return (
    <div className="live-room-page" style={{display:'flex',alignItems:'center',justifyContent:'center'}}>
        <div style={{textAlign:'center',color:'var(--text-muted)'}}>
        <div style={{fontSize:40,marginBottom:12}}>!</div>
        <div>无法识别用户身份</div>
        <button style={{marginTop:12,padding:'8px 20px',background:'var(--primary-grad)',border:'none',borderRadius:10,color:'#fff',fontWeight:700,cursor:'pointer'}}
          onClick={onBack}>返回登录</button>
      </div>
    </div>
  )

  return (
    <div className="live-room-page">
      <div className="lrp-scroll-area">
        {/* A: 视频画面（全屏） */}
        <VideoPlayer coverUrl={coverUrl} videoUrl="/videos/auction-demo.mp4" isLive={true} viewerCount={<LiveViewerCount />} roomTitle={roomTitle} />

        {/* B: 主播信息卡 */}
        {anchorInfo ? (
          <AnchorHeader info={anchorInfo} viewerCount={<LiveViewerCount />}
            onMoreRooms={()=>navigate('/')} />
        ) : (
          <div className="anchor-header" style={{justifyContent:'center',padding:14}}>
            <span style={{fontSize:13,color:'var(--text-muted)'}}>⏳ 加载主播...</span>
          </div>
        )}

        {/* C+D: 竞拍面板（含排行榜） */}
        {activeAuctionId && (
          <AuctionPanel roomId={roomId} userId={userId}
            wsMessage={lastMessage} connected={connected}
            activeAuctionId={activeAuctionId}
            paidAuctionIds={paidAuctionIds}
            onPaid={handlePaid}
            selectedAddressId={selectedAddressId}
            selectedAddress={selectedAddress}
            productName={activeProductName}
            productImage={activeProductImage}
            onAuctionEnd={(a) => { sound.playAuctionEnd(); handleAuctionEnd(a) }}
            onOutbid={() => { sound.playOutbid() }}
            onBidSuccess={() => { sound.playBidSuccess() }}
          />
        )}

        <button className="back-btn" onClick={onBack} title="返回">
          &#8249;
        </button>

        {!connected && (
          <button className="reconnect-btn" onClick={reconnect} title="重新连接">
            &#8635;
          </button>
        )}
      </div>

      <ProductFloatPanel auctions={allAuctions} activeAuctionId={activeAuctionId}
        onSelect={handleSelectAuction} productNames={productNames}
        productImages={productImages} />

      <AddressFloatPanel
        selectedId={selectedAddressId}
        onSelect={handleSelectAddress}
      />

      <BarrageSimLayer active={connected} userMessages={userBarrages} />

      <button className="sound-toggle" onClick={sound.toggle} title={sound.enabled ? '关闭音效' : '开启音效'}>
        {sound.enabled ? '开' : '关'}
      </button>

      <div className="bottom-toolbar">
        <input
          className="bt-chat-input"
          placeholder="说点什么..."
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              const target = e.target as HTMLInputElement
              handleSendBarrage(target.value)
              target.value = ''
            }
          }}
        />
        <button className="bt-icon-btn" title="分享"
          onClick={() => Toast.show('分享功能开发中')}>分享</button>
        <button className="bt-icon-btn" title="点赞"
          onClick={() => Toast.success('点赞 +1')}>赞</button>
        <button className="bt-cart-btn" title="购物车/商品"
          onClick={() => Toast.show('购物车功能开发中')}>购物车</button>
      </div>

      <AuctionResultModal open={showResultModal} auction={endedAuction}
        currentUserId={userId} productName={activeProductName}
        productImage={activeProductImage} onClose={handleResultClose} />
    </div>
  )
}
