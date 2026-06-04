/**
 * RoomListPage — 直播间列表首页
 * 显示所有直播中的房间，点击进入直播间。
 */

import { useEffect, useState } from 'react'
import { getToken } from '../api/client'

const BASE = '/api'

type LiveRoom = {
  id: number
  title: string
  coverUrl: string
  status: string
  createdAt: string
}

function authHeaders(): Record<string, string> {
  const token = getToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export default function RoomListPage() {
  const [rooms, setRooms] = useState<LiveRoom[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetch(`${BASE}/rooms`, { headers: authHeaders() })
      .then(r => r.json())
      .then(body => { setRooms(body.data || []) })
      .catch(() => {})
      .finally(() => setLoading(false))
  }, [])

  if (loading) {
    return <div className="panel pending">加载中…</div>
  }

  return (
    <div className="room-list-page">
      <h2 className="page-title">正在直播</h2>

      {rooms.length === 0 ? (
        <div className="panel empty-state">
          <p>暂无直播中的房间</p>
          <p className="sub">商家开播后会出现在这里</p>
        </div>
      ) : (
        <div className="room-grid">
          {rooms.map(room => (
            <div
              key={room.id}
              className="room-card"
              onClick={() => window.location.hash = `#/rooms/${room.id}`}
            >
              <div className="room-card-cover">
                <span className="live-badge">LIVE</span>
              </div>
              <div className="room-card-body">
                <h3>{room.title}</h3>
                <p className="room-card-meta">
                  房间 #{room.id}
                </p>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
