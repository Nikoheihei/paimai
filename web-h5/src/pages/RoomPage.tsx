/**
 * RoomPage — 直播间页面
 *
 * 从 RoomListPage 点击进入，带返回按钮。
 */

import { useState, useMemo } from 'react'
import AuctionPanel from '../components/AuctionPanel'
import { useWebSocket } from '../hooks/useWebSocket'
import { getToken } from '../api/client'
import type { WsMessage } from '../hooks/useWebSocket'

type Props = {
  roomId: number
  onBack: () => void
}

function parseUserIdFromToken(): number {
  const token = getToken()
  if (!token) return 0
  try {
    const payload = JSON.parse(atob(token.split('.')[1]))
    return payload.userId || 0
  } catch {
    return 0
  }
}

export default function RoomPage({ roomId, onBack }: Props) {
  const userId = useMemo(() => parseUserIdFromToken(), [])
  const [lastMessage, setLastMessage] = useState<WsMessage | null>(null)

  const { connected, reconnect } = useWebSocket(roomId, userId, {
    onMessage: (msg) => setLastMessage(msg),
    onConnected: () => console.log('WS 已连接'),
    onDisconnected: () => console.log('WS 已断开'),
  })

  if (!userId) {
    return <div className="panel pending">无法识别用户身份，请重新登录</div>
  }

  return (
    <div className="room-page">
      <header className="room-header">
        <button className="back-btn" onClick={onBack}>← 返回</button>
        <h1>直播间 #{roomId}</h1>
        <button className="reconnect-btn" onClick={reconnect} title="手动重连">↻</button>
      </header>

      <AuctionPanel
        roomId={roomId}
        userId={userId}
        wsMessage={lastMessage}
        connected={connected}
      />
    </div>
  )
}
