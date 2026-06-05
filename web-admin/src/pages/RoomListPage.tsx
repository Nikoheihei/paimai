import { useEffect, useState } from 'react'
import { listRooms, createRoom, goLive, closeRoom, type LiveRoom } from '../api/client'

export default function RoomListPage() {
  const [rooms, setRooms] = useState<LiveRoom[]>([])
  const [filtered, setFiltered] = useState<LiveRoom[]>([])
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [showCreate, setShowCreate] = useState(false)
  const [title, setTitle] = useState('')
  const [coverUrl, setCoverUrl] = useState('')
  const [loading, setLoading] = useState(false)

  const load = () => {
    setLoading(true)
    listRooms().then(data => {
      setRooms(data)
      setFiltered(data)
    }).catch(() => {}).finally(() => setLoading(false))
  }
  useEffect(load, [])

  useEffect(() => {
    let result = rooms
    if (search.trim()) {
      result = result.filter(r => r.title.toLowerCase().includes(search.trim().toLowerCase()))
    }
    if (statusFilter) {
      result = result.filter(r => r.status === statusFilter)
    }
    setFiltered(result)
  }, [search, statusFilter, rooms])

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!title.trim()) return
    setLoading(true)
    try {
      await createRoom(title.trim(), coverUrl || undefined)
      setTitle(''); setCoverUrl('')
      setShowCreate(false)
      load()
    } catch (err: any) { alert(err.message) }
    finally { setLoading(false) }
  }

  const handleGoLive = async (id: number) => {
    try { await goLive(id); load() } catch (err: any) { alert(err.message) }
  }
  const handleClose = async (id: number) => {
    if (!confirm('关播后将结算该直播间所有进行中的竞拍，确定？')) return
    try { await closeRoom(id); load() } catch (err: any) { alert(err.message) }
  }

  const statusLabel = (s: string) => ({ offline: '未开播', live: '直播中', closed: '已结束' })[s] || s
  const statusClass = (s: string) => ({ offline: 'gray', live: 'green', closed: 'gray' })[s] || 'gray'

  return (
    <div className="admin-page">
      <div className="page-header">
        <h1>我的直播间</h1>
        <button className="admin-btn primary" onClick={() => setShowCreate(!showCreate)}>
          {showCreate ? '取消' : '+ 创建直播间'}
        </button>
      </div>

      {/* 搜索筛选栏 */}
      <div className="filter-bar">
        <input
          type="text"
          placeholder="搜索直播间标题..."
          value={search}
          onChange={e => setSearch(e.target.value)}
          className="filter-input"
        />
        <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)} className="filter-select">
          <option value="">全部状态</option>
          <option value="offline">未开播</option>
          <option value="live">直播中</option>
          <option value="closed">已结束</option>
        </select>
      </div>

      {showCreate && (
        <form className="create-form-card" onSubmit={handleCreate}>
          <h3>新建直播间</h3>
          <div className="form-grid-2col">
            <div className="field">
              <label>直播间标题 *</label>
              <input type="text" placeholder="输入直播间标题" value={title} onChange={e => setTitle(e.target.value)} required />
            </div>
            <div className="field">
              <label>封面图 URL（可选）</label>
              <input type="text" placeholder="https://..." value={coverUrl} onChange={e => setCoverUrl(e.target.value)} />
            </div>
          </div>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
            <button type="button" className="admin-btn" onClick={() => { setShowCreate(false); setTitle(''); setCoverUrl('') }}>取消</button>
            <button type="submit" className="admin-btn primary" disabled={loading}>创建</button>
          </div>
        </form>
      )}

      {/* 直播间表格 */}
      {filtered.length > 0 ? (
        <table className="data-table">
          <thead>
            <tr>
              <th>封面</th>
              <th>名称</th>
              <th>状态</th>
              <th>创建时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map(r => (
              <tr key={r.id}>
                <td>
                  {r.coverUrl ? (
                    <img src={r.coverUrl} alt="" className="thumb-img" style={{ width: 64, height: 48 }} />
                  ) : (
                    <div className="no-thumb" style={{ width: 64, height: 48, fontSize: 10 }}>无封面</div>
                  )}
                </td>
                <td>
                  <strong>{r.title}</strong>
                  <br /><span className="meta">ID: #{r.id}</span>
                </td>
                <td><span className={`status-tag ${statusClass(r.status)}`}>{statusLabel(r.status)}</span></td>
                <td>{new Date(r.createdAt).toLocaleDateString('zh-CN')}</td>
                <td>
                  <div className="action-cell">
                    <button className="admin-btn small" onClick={() => window.location.hash = `#/rooms/${r.id}`}>管理</button>
                    {r.status === 'offline' && <button className="admin-btn small primary" onClick={() => handleGoLive(r.id)}>开播</button>}
                    {r.status === 'live' && <button className="admin-btn small danger" onClick={() => handleClose(r.id)}>关播</button>}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className="empty-state-box">
          <div className="empty-icon">📺</div>
          <p>暂无直播间</p>
          <p className="sub">点击右上角"创建直播间"开始</p>
        </div>
      )}
    </div>
  )
}
