/**
 * LiveRoomPage — 全屏沉浸式直播间页面
 *
 * 布局（从下到上叠加在视频上）：
 *   A: 视频播放器（全屏铺满）
 *   B: 主播信息卡（浮动，毛玻璃）
 *   C: 竞拍面板（毛玻璃卡片）
 *   D: 右侧商品抽屉
 *   E: 底部工具栏（固定）
 *   F: 结果弹窗覆盖层
 */

import { useState, useEffect, useMemo, useCallback } from 'react'
import { useWebSocket } from '../hooks/useWebSocket'
import { getToken, getRoom, getRoomAuctions, type Auction as ApiAuction } from '../api/client'
import type { WsMessage } from '../hooks/useWebSocket'
import type { Auction, UserInfo } from '../shared/types'
import { useSound } from '../hooks/useSound'
import type { BarrageItem } from '../components/BarrageLayer'

// 组件
import VideoPlayer from '../components/VideoPlayer'
import AnchorHeader from '../components/AnchorHeader'
import AuctionPanel from '../components/AuctionPanel'
import ProductFloatPanel from '../components/ProductFloatPanel'
import AddressFloatPanel, { type AddressItem } from '../components/AddressFloatPanel'
import AuctionResultModal from '../components/AuctionResultModal'
import Toast from '../components/Toast'
import BarrageLayer, { randomBarrage } from '../components/BarrageLayer'

type Props = {
  roomId: number
  onBack: () => void
}

const AUCTION_REFRESH_EVENTS = new Set(['product.created', 'auction.created', 'auction.updated'])

function parseUserIdFromToken(): number {
  const token = getToken()
  if (!token) return 0
  try {
    const payload = JSON.parse(atob(token.split('.')[1]))
    return payload.userId || 0
  } catch { return 0 }
}

export default function LiveRoomPage({ roomId, onBack }: Props) {
  const userId = useMemo(() => parseUserIdFromToken(), [])
  const [lastMessage, setLastMessage] = useState<WsMessage | null>(null)

  // 房间数据
  const [roomTitle, setRoomTitle] = useState('')
  const [coverUrl, setCoverUrl] = useState('')
  const [allAuctions, setAllAuctions] = useState<Auction[]>([] as Auction[])
  // 当前选中的竞拍 ID（由商品浮层切换控制）
  const [activeAuctionId, setActiveAuctionId] = useState<number | null>(null)

  // 拍卖结束弹窗状态
  const [endedAuction, setEndedAuction] = useState<Auction | null>(null)
  const [showResultModal, setShowResultModal] = useState(false)
  // 已支付的竞拍 ID（跨面板保持状态）
  const [paidAuctionIds, setPaidAuctionIds] = useState<number[]>([])
  const handlePaid = useCallback((auctionId: number) => {
    setPaidAuctionIds(prev => prev.includes(auctionId) ? prev : [...prev, auctionId])
  }, [])
  // 选中的收货地址
  const [selectedAddressId, setSelectedAddressId] = useState<number | null>(null)
  const [selectedAddress, setSelectedAddress] = useState<AddressItem | null>(null)
  const handleSelectAddress = useCallback((addr: AddressItem) => {
    setSelectedAddressId(addr.id)
    setSelectedAddress(addr)
  }, [])
  // 观看数模拟
  const [viewerCount, setViewerCount] = useState(128 + Math.floor(Math.random() * 500))

  // 弹幕
  const [barrages, setBarrages] = useState<BarrageItem[]>([])

  // 音效
  const sound = useSound()

  // WebSocket 连接
  const { connected, reconnect } = useWebSocket(roomId, userId, {
    onMessage: (msg) => {
      setLastMessage(msg)
      // 收到商品/竞拍变更事件，刷新竞拍列表
      if (AUCTION_REFRESH_EVENTS.has(msg.type)) {
        loadRoomAuctions()
      }
    },
    onConnected: () => console.log('[WS] 已连接'),
    onDisconnected: () => console.log('[WS] 已断开'),
  })

  // 主播信息
  const [anchorInfo, setAnchorInfo] = useState<UserInfo | null>(null)

  // 加载房间信息 + 所有竞拍 + 用户信息
  const loadRoomAuctions = useCallback(() => {
    getRoomAuctions(roomId).then((list: ApiAuction[]) => {
      setAllAuctions(list as unknown as Auction[])
      const running = list.find(a => a.status === 'running')
      if (running && !activeAuctionId) setActiveAuctionId(running.id)
    })
  }, [roomId, activeAuctionId])

  useEffect(() => {
    getRoom(roomId).then(r => {
      setRoomTitle(r.title); setCoverUrl(r.coverUrl)
      if (r.title) document.title = `${r.title} - 直播拍卖`
      // 使用房间返回的主播信息
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

  // 模拟观看数波动 + 模拟弹幕
  useEffect(() => {
    if (!connected) return
    const t = setInterval(() =>
      setViewerCount(v => v + Math.floor(Math.random()*7)-2), 5000
    )
    // 每 3-8 秒随机生成一条弹幕
    const barrageTimer = setInterval(() => {
      setBarrages(prev => [...prev, randomBarrage(prev.length)])
    }, 3000 + Math.floor(Math.random() * 5000))
    return () => {
      clearInterval(t)
      clearInterval(barrageTimer)
    }
  }, [connected])

  // 关闭竞拍面板回到直播画面
  const handleCloseAuction = useCallback(()=>{
    setActiveAuctionId(null)
  },[])

  // 监听关闭竞拍面板事件
  useEffect(() => {
    window.addEventListener('auction:close', handleCloseAuction)
    return () => window.removeEventListener('auction:close', handleCloseAuction)
  }, [handleCloseAuction])

  // 商品名称和图片映射（优先使用后端返回的 productName）
  const productNames = useMemo(() => {
    const map: Record<number,string> = {}
    allAuctions.forEach(a => {
      map[a.productId] = a.productName || `\u5546\u54C1 #${a.productId}`
    })
    return map
  }, [allAuctions])

  const productImages = useMemo<Record<number,string>>(() => {
    const map: Record<number,string> = {}
    allAuctions.forEach(a => {
      if (a.productImage) map[a.productId] = a.productImage
    })
    return map
  }, [allAuctions])

  // 当前选中商品
  const activeProductImage = activeAuctionId
    ? productImages[allAuctions.find(a=>a.id===activeAuctionId)?.productId||0]
    : undefined
  const activeProductName = activeAuctionId
    ? productNames[allAuctions.find(a=>a.id===activeAuctionId)?.productId||0]
    : undefined

  // 商品浮层选择回调
  const handleSelectAuction = useCallback((auctionId:number)=>{
    setActiveAuctionId(auctionId); setLastMessage(null)
  },[])

  // 拍卖结束回调
  const handleAuctionEnd = useCallback((auction:Auction)=>{
    setEndedAuction(auction); setShowResultModal(true)
    // 成交后刷新订单列表（如果用户是赢家，订单会出现在"我的订单"中）
    if (auction.winnerUserId === userId) {
      // 延迟一下让结算完成
      setTimeout(() => {
        window.dispatchEvent(new CustomEvent('order:refresh'))
      }, 1000)
    }
  },[userId])

  // 结果弹窗关闭
  const handleResultClose = useCallback(()=>{
    setShowResultModal(false); setEndedAuction(null)
    const next=allAuctions.find(a=>a.status==='running')
    if(next&&next.id!==activeAuctionId) setActiveAuctionId(next.id)
  },[allAuctions,activeAuctionId])

  // 发送弹幕
  const handleSendBarrage = useCallback((text: string) => {
    if (!text.trim()) return
    setBarrages(prev => [...prev, {
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
        <VideoPlayer coverUrl={coverUrl} isLive={true} viewerCount={viewerCount} roomTitle={roomTitle} />

        {/* B: 主播信息卡 */}
        {anchorInfo ? (
          <AnchorHeader info={anchorInfo} viewerCount={viewerCount}
            onMoreRooms={()=>window.location.hash='#/'} />
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

        {/* 返回按钮 */}
        <button className="back-btn" onClick={onBack} title="返回">
          &#8249;
        </button>

        {/* 重连按钮 */}
        {!connected && (
          <button className="reconnect-btn" onClick={reconnect} title="重新连接">
            &#8635;
          </button>
        )}
      </div>

      {/* 右侧商品抽屉 */}
      <ProductFloatPanel auctions={allAuctions} activeAuctionId={activeAuctionId}
        onSelect={handleSelectAuction} productNames={productNames}
        productImages={productImages} />

      {/* 右侧地址浮窗 */}
      <AddressFloatPanel
        selectedId={selectedAddressId}
        onSelect={handleSelectAddress}
      />

      {/* 弹幕层 */}
      <BarrageLayer messages={barrages} />

      {/* 音效开关 */}
      <button className="sound-toggle" onClick={sound.toggle} title={sound.enabled ? '关闭音效' : '开启音效'}>
        {sound.enabled ? '开' : '关'}
      </button>

      {/* E: 底部工具栏 */}
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

      {/* F: 拍卖结果弹窗 */}
      <AuctionResultModal open={showResultModal} auction={endedAuction}
        currentUserId={userId} productName={activeProductName}
        productImage={activeProductImage} onClose={handleResultClose} />
    </div>
  )
}
