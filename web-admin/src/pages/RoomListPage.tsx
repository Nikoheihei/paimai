import { useEffect, useState } from 'react'
import { listRooms, createRoom, type LiveRoom } from '../api/client'

export default function RoomListPage() {
  const [rooms, setRooms] = useState<LiveRoom[]>([])
  const [showCreate, setShowCreate] = useState(false)
  const [title, setTitle] = useState('')
  const [loading, setLoading] = useState(false)

  const load = () => { listRooms().then(setRooms).catch(() => {}) }
  useEffect(load, [])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!title.trim()) return
    setLoading(true)
    try {
      await createRoom(title.trim())
      setTitle('')
      setShowCreate(false)
      load()
    } catch (err: any) { alert(err.message) }
    finally { setLoading(false) }
  }

  const statusLabel = (s: string) => ({ offline: '未开播', live: '直播中', closed: '已结束' })[s] || s
  const statusClass = (s: string) => ({ offline: 'gray', live: 'green', closed: 'gray' })[s] || 'gray'

  return (
    <div className="admin-page">
      <div className="page-header">
        <h1>我的直播间</h1>
        <button className="admin-btn primary" onClick={() => setShowCreate(!showCreate)}>{showCreate ? '取消' : '新建直播间'}</button>
      </div>

      {showCreate && (
        <form className="inline-form" onSubmit={handleCreate}>
          <input type="text" placeholder="直播间标题" value={title} onChange={e => setTitle(e.target.value)} required />
          <button className="admin-btn primary" type="submit" disabled={loading}>创建</button>
        </form>
      )}

      <div className="card-list">
        {rooms.map(r => (
          <div key={r.id} className="card" onClick={() => window.location.hash = `#/rooms/${r.id}`}>
            <div className="card-body">
              <h3>{r.title}</h3>
              <p className="meta">ID: {r.id} · 创建于 {new Date(r.createdAt).toLocaleString('zh-CN')}</p>
            </div>
            <span className={`status-tag ${statusClass(r.status)}`}>{statusLabel(r.status)}</span>
          </div>
        ))}
        {rooms.length === 0 && <p className="empty">暂无直播间</p>}
      </div>
    </div>
  )
}
