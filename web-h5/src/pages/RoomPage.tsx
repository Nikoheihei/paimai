/**
 * RoomPage — 直播间页面
 * 连接 WebSocket + 渲染竞拍面板
 */

import { useState } from 'react'
import AuctionPanel from '../components/AuctionPanel'
import { useWebSocket } from '../hooks/useWebSocket'
import type { WsMessage } from '../hooks/useWebSocket'

type Props = {
  roomId: number
  userId: number
}

export default function RoomPage({ roomId, userId }: Props) {
  const [lastMessage, setLastMessage] = useState<WsMessage | null>(null)

  const { connected, reconnect } = useWebSocket(roomId, userId, {
    onMessage: (msg) => {
      setLastMessage(msg)
    },
    onConnected: () => {
      console.log('WS 已连接')
    },
    onDisconnected: () => {
      console.log('WS 已断开')
    },
  })

  return (
    <div className="room-page">
      <header className="room-header">
        <h1>直播间 #{roomId}</h1>
        <button className="reconnect-btn" onClick={reconnect} title="手动重连">
          ↻
        </button>
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
