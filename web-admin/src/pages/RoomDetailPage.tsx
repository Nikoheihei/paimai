import { useEffect, useState } from 'react'
import { getRoom, goLive, closeRoom, listProducts, createProduct, deleteProduct, listAuctions, publishAuction, startAuction, cancelAuction, createAuction, type LiveRoom, type Product, type Auction } from '../api/client'

function formatCents(c: number) { return (c / 100).toFixed(2) }
function statusLabel(s: string) {
  const m: Record<string, string> = { draft: '草稿', scheduled: '待开始', running: '进行中', sold: '已成交', failed: '流拍', cancelled: '已取消' }
  return m[s] || s
}

type Props = { roomId: number; onBack: () => void }

export default function RoomDetailPage({ roomId, onBack }: Props) {
  const [room, setRoom] = useState<LiveRoom | null>(null)
  const [products, setProducts] = useState<Product[]>([])
  const [auctions, setAuctions] = useState<Auction[]>([])
  const [newProductName, setNewProductName] = useState('')
  const [newProductDesc, setNewProductDesc] = useState('')
  const [showNewProduct, setShowNewProduct] = useState(false)
  const [newAuctionProductId, setNewAuctionProductId] = useState(0)
  const [newAuctionMode, setNewAuctionMode] = useState('sudden_death')
  const [newAuctionIncrement, setNewAuctionIncrement] = useState('100')
  const [newAuctionCap, setNewAuctionCap] = useState('10000')
  const [newAuctionDuration, setNewAuctionDuration] = useState('300')
  const [msg, setMsg] = useState('')
  
  const load = () => {
    getRoom(roomId).then(setRoom).catch(() => {})
    listProducts().then(setProducts).catch(() => {})
    listAuctions(roomId).then(setAuctions).catch(() => {})
  }
  useEffect(load, [roomId])

  const handleGoLive = async () => {
    try { const r = await goLive(roomId); setRoom(r); setMsg('已开播') } catch (err: any) { setMsg(err.message) }
  }
  const handleClose = async () => {
    if (!confirm('关播后将结算该直播间所有进行中的竞拍，确定？')) return
    try { const r = await closeRoom(roomId); setMsg(`已关播，结算了 ${r.settled} 个竞拍`); load() } catch (err: any) { setMsg(err.message) }
  }
  const handleCreateProduct = async (e: React.FormEvent) => {
    e.preventDefault(); if (!newProductName.trim()) return
    try { await createProduct(newProductName.trim(), '', newProductDesc.trim()); setNewProductName(''); setNewProductDesc(''); setShowNewProduct(false); listProducts().then(setProducts) } catch (err: any) { setMsg(err.message) }
  }
  const handleDeleteProduct = async (id: number) => {
    if (!confirm('确定删除该商品？')) return
    try { await deleteProduct(id); listProducts().then(setProducts) } catch (err: any) { setMsg(err.message) }
  }
  const handleCreateAuction = async () => {
    if (!newAuctionProductId) { setMsg('请选择商品'); return }
    try {
      await createAuction(roomId, newAuctionProductId, newAuctionMode, 0, parseInt(newAuctionIncrement) || 100, parseInt(newAuctionCap) || 10000)
      setMsg('竞拍已创建'); listAuctions(roomId).then(setAuctions)
    } catch (err: any) { setMsg(err.message) }
  }
  const handlePublish = async (id: number) => {
    try { await publishAuction(id); setMsg('已发布'); listAuctions(roomId).then(setAuctions) } catch (err: any) { setMsg(err.message) }
  }
  const handleStart = async (id: number) => {
    try { await startAuction(id, parseInt(newAuctionDuration) || 300); setMsg('已开始'); listAuctions(roomId).then(setAuctions) } catch (err: any) { setMsg(err.message) }
  }
  const handleCancel = async (id: number) => {
    const reason = prompt('取消原因（可选）') || ''
    try { await cancelAuction(id, reason); setMsg('已取消'); listAuctions(roomId).then(setAuctions) } catch (err: any) { setMsg(err.message) }
  }

  if (!room) return <div className="admin-page"><p className="empty">加载中…</p></div>

  return (
    <div className="admin-page">
      <div className="page-header">
        <button className="admin-btn" onClick={onBack}>← 返回</button>
        <h1>{room.title}</h1>
        <span className={`status-tag ${room.status === 'live' ? 'green' : 'gray'}`}>
          {{ offline: '未开播', live: '直播中', closed: '已结束' }[room.status] || room.status}
        </span>
      </div>

      {msg && <div className="toast" onClick={() => setMsg('')}>{msg}</div>}

      <div className="action-bar">
        {room.status === 'offline' && <button className="admin-btn primary" onClick={handleGoLive}>开播</button>}
        {room.status !== 'closed' && <button className="admin-btn danger" onClick={handleClose}>关播</button>}
      </div>

      {/* 商品管理 */}
      <section>
        <div className="section-header"><h2>商品</h2><button className="admin-btn" onClick={() => setShowNewProduct(!showNewProduct)}>+ 添加</button></div>
        {showNewProduct && (
          <form className="inline-form" onSubmit={handleCreateProduct}>
            <input type="text" placeholder="商品名称" value={newProductName} onChange={e => setNewProductName(e.target.value)} required />
            <input type="text" placeholder="描述（可选）" value={newProductDesc} onChange={e => setNewProductDesc(e.target.value)} />
            <button className="admin-btn primary" type="submit">保存</button>
          </form>
        )}
        <div className="card-list">
          {products.map(p => (
            <div key={p.id} className="card compact">
              <span className="card-body"><strong>{p.name}</strong> <span className="meta">#{p.id}</span></span>
              <button className="admin-btn small danger" onClick={() => handleDeleteProduct(p.id)}>删除</button>
            </div>
          ))}
          {products.length === 0 && <p className="empty">暂无商品</p>}
        </div>
      </section>

      {/* 竞拍管理 */}
      <section>
        <div className="section-header"><h2>竞拍</h2></div>
        {room.status !== 'closed' && (
          <div className="create-auction">
            <select value={newAuctionProductId} onChange={e => setNewAuctionProductId(parseInt(e.target.value) || 0)}>
              <option value={0}>选择商品…</option>
              {products.map(p => <option key={p.id} value={p.id}>{p.name} (#{p.id})</option>)}
            </select>
            <select value={newAuctionMode} onChange={e => setNewAuctionMode(e.target.value)}>
              <option value="sudden_death">绝杀模式</option>
              <option value="extension">延时模式</option>
            </select>
            <input type="number" placeholder="加价幅度(分)" value={newAuctionIncrement} onChange={e => setNewAuctionIncrement(e.target.value)} className="small-input" />
            <input type="number" placeholder="封顶价(分)" value={newAuctionCap} onChange={e => setNewAuctionCap(e.target.value)} className="small-input" />
            <input type="number" placeholder="时长(秒)" value={newAuctionDuration} onChange={e => setNewAuctionDuration(e.target.value)} className="small-input" />
            <button className="admin-btn primary" onClick={handleCreateAuction}>创建竞拍</button>
          </div>
        )}
        <div className="card-list">
          {auctions.map(a => (
            <div key={a.id} className="card compact">
              <div className="card-body">
                <strong>竞拍 #{a.id}</strong> · 商品 #{a.productId} · ¥{formatCents(a.currentPriceCents)} · <span className="meta">{statusLabel(a.status)}</span>
              </div>
              <div className="card-actions">
                {a.status === 'draft' && <button className="admin-btn small" onClick={() => handlePublish(a.id)}>发布</button>}
                {a.status === 'scheduled' && <button className="admin-btn small primary" onClick={() => handleStart(a.id)}>开始</button>}
                {(a.status === 'draft' || a.status === 'scheduled') && <button className="admin-btn small danger" onClick={() => handleCancel(a.id)}>取消</button>}
              </div>
            </div>
          ))}
          {auctions.length === 0 && <p className="empty">暂无竞拍</p>}
        </div>
      </section>
    </div>
  )
}
