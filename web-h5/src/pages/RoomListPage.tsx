/**
 * HomePage — 直播间列表首页（增强版）
 * 显示所有直播中的房间，支持封面图、搜索、下拉刷新。
 */

import { useEffect, useState, useCallback, useRef } from 'react'
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
  const [filtered, setFiltered] = useState<LiveRoom[]>([])
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const touchStartY = useRef(0)

  const load = useCallback(async () => {
    try {
      const res = await fetch(`${BASE}/rooms`, { headers: authHeaders() })
      const body = await res.json()
      const data = body.data || []
      setRooms(data)
      setFiltered(data)
    } catch {
      setRooms([])
      setFiltered([])
    }
  }, [])

  useEffect(() => { load().finally(() => setLoading(false)) }, [load])

  useEffect(() => {
    if (!search.trim()) { setFiltered(rooms); return }
    const q = search.trim().toLowerCase()
    setFiltered(rooms.filter(r => r.title.toLowerCase().includes(q)))
  }, [search, rooms])

  // 下拉刷新
  const handleTouchStart = (e: React.TouchEvent) => { touchStartY.current = e.touches[0].clientY }
  const handleTouchEnd = async (e: React.TouchEvent) => {
    const diff = e.changedTouches[0].clientY - touchStartY.current
    if (diff > 80 && window.scrollY <= 0) {
      setRefreshing(true)
      await load()
      setRefreshing(false)
    }
  }

  // 模拟观看人数（后续替换为后端真实数据）
  const viewerCount = (id: number) => {
    const base = 500 + (id * 137) % 3000
    return base >= 10000 ? `${(base / 10000).toFixed(1)}万` : String(base)
  }

  if (loading) {
    return <div className="panel pending">加载中…</div>
  }

  return (
    <div className="room-list-page" onTouchStart={handleTouchStart} onTouchEnd={handleTouchEnd}>
      {/* 顶部搜索栏 */}
      <div className="home-search-bar">
        <span className="search-icon">搜索</span>
        <input
          type="text"
          placeholder="搜索直播间..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="home-search-input"
        />
        {search && <button className="search-clear" onClick={() => setSearch('')}>×</button>}
      </div>

      {/* 下拉刷新指示器 */}
      {refreshing && <div className="refresh-indicator">刷新中...</div>}

      {filtered.length === 0 ? (
        <div className="panel empty-state">
          <p>{search ? '未找到匹配的直播间' : '暂无直播中的房间'}</p>
          {!search && <p className="sub">商家开播后会出现在这里</p>}
        </div>
      ) : (
        <div className="room-grid">
          {filtered.map(room => (
            <div
              key={room.id}
              className="room-card"
              onClick={() => window.location.hash = `#/rooms/${room.id}`}
            >
              <div className="room-card-cover" style={room.coverUrl ? { backgroundImage: `url(${room.coverUrl})` } : undefined}>
                <span className="live-badge">LIVE</span>
                <span className="viewer-count">{'\u{1F441}'} {viewerCount(room.id)}</span>
              </div>
              <div className="room-card-body">
                <h3>{room.title}</h3>
                <p className="room-card-meta">房间 #{room.id}</p>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
