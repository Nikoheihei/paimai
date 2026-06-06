import { useEffect, useState } from 'react'
import { getRoom, goLive, closeRoom, listProducts, createProduct, deleteProduct, listAuctions, publishAuction, startAuction, cancelAuction, createAuction, settleAuction, type LiveRoom, type Product, type Auction } from '../api/client'
import ImageUploader from '../components/ImageUploader'

function formatCents(c: number) { return (c / 100).toFixed(2) }
function yuanToCents(value: string, fallbackCents: number) {
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed < 0) return fallbackCents
  return Math.round(parsed * 100)
}
function optionalYuanToCents(value: string) {
  if (!value.trim()) return null
  return yuanToCents(value, 0)
}

type TabKey = 'products' | 'auctions'

type Props = { roomId: number; onBack: () => void }

export default function RoomDetailPage({ roomId, onBack }: Props) {
  const [room, setRoom] = useState<LiveRoom | null>(null)
  const [products, setProducts] = useState<Product[]>([])
  const [auctions, setAuctions] = useState<Auction[]>([])
  const [activeTab, setActiveTab] = useState<TabKey>('products')
  const [msg, setMsg] = useState('')

  // 商品创建表单
  const [newProductName, setNewProductName] = useState('')
  const [newProductDesc, setNewProductDesc] = useState('')
  const [newProductImage, setNewProductImage] = useState('')
  const [showNewProduct, setShowNewProduct] = useState(false)

  // 竞拍创建表单（完整字段）
  const [newAuctionProductId, setNewAuctionProductId] = useState(0)
  const [newAuctionMode, setNewAuctionMode] = useState<'sudden_death' | 'extension'>('sudden_death')
  const [newAuctionStartPrice, setNewAuctionStartPrice] = useState('0')
  const [newAuctionIncrement, setNewAuctionIncrement] = useState('1.00')
  const [newAuctionCap, setNewAuctionCap] = useState('100.00')
  const [newAuctionReserve, setNewAuctionReserve] = useState('')
  const [newAuctionDuration, setNewAuctionDuration] = useState('300')
  const [newAuctionExtendThreshold, setNewAuctionExtendThreshold] = useState('10')
  const [newAuctionExtendDuration, setNewAuctionExtendDuration] = useState('15')
  const [showNewAuction, setShowNewAuction] = useState(false)

  const load = () => {
    getRoom(roomId).then(setRoom).catch(() => {})
    listProducts().then(setProducts).catch(() => {})
    listAuctions(roomId).then(setAuctions).catch(() => {})
  }
  useEffect(load, [roomId])

  // === 房间操作 ===
  const handleGoLive = async () => {
    try { const r = await goLive(roomId); setRoom(r); setMsg('已开播') } catch (err: any) { setMsg(err.message) }
  }
  const handleClose = async () => {
    if (!confirm('关播后将结算该直播间所有进行中的竞拍，确定？')) return
    try { const r = await closeRoom(roomId); setMsg(`已关播，结算了 ${r.settled} 个竞拍`); load() } catch (err: any) { setMsg(err.message) }
  }

  // === 商品操作 ===
  const handleCreateProduct = async () => {
    if (!newProductName.trim()) return
    try {
      await createProduct(newProductName.trim(), newProductImage || '', newProductDesc.trim())
      setNewProductName(''); setNewProductDesc(''); setNewProductImage('')
      setShowNewProduct(false)
      listProducts().then(setProducts)
      setMsg('商品已添加')
    } catch (err: any) { setMsg(err.message) }
  }
  const handleDeleteProduct = async (id: number) => {
    if (!confirm('确定删除该商品？')) return
    try { await deleteProduct(id); listProducts().then(setProducts); setMsg('商品已删除') } catch (err: any) { setMsg(err.message) }
  }

  // ImageUploader 的 onChange 返回 URL（base64 占位），这里直接用
  const handleImageUrlChange = (url: string) => { setNewProductImage(url) }

  // === 竞拍操作 ===
  const handleCreateAuction = async () => {
    if (!newAuctionProductId) { setMsg('请选择商品'); return }
    try {
      await createAuction(
        roomId,
        newAuctionProductId,
        newAuctionMode,
        yuanToCents(newAuctionStartPrice, 0),
        yuanToCents(newAuctionIncrement, 100),
        yuanToCents(newAuctionCap, 10000),
        optionalYuanToCents(newAuctionReserve),
        newAuctionMode === 'extension' ? parseInt(newAuctionExtendThreshold) || 10 : 0,
        newAuctionMode === 'extension' ? parseInt(newAuctionExtendDuration) || 15 : 0,
        undefined, undefined, // startAt/endAt 由后端计算
      )
      setMsg('竞拍已创建'); listAuctions(roomId).then(setAuctions)
      setShowNewAuction(false)
      resetAuctionForm()
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
  const handleSettle = async (id: number) => {
    if (!confirm('确定手动结算此竞拍？')) return
    try { await settleAuction(id); setMsg('已结算'); listAuctions(roomId).then(setAuctions) } catch (err: any) { setMsg(err.message) }
  }
  const resetAuctionForm = () => {
    setNewAuctionProductId(0); setNewAuctionMode('sudden_death')
    setNewAuctionStartPrice('0'); setNewAuctionIncrement('1.00')
    setNewAuctionCap('100.00'); setNewAuctionReserve('')
    setNewAuctionDuration('300'); setNewAuctionExtendThreshold('10')
    setNewAuctionExtendDuration('15')
  }

  if (!room) return <div className="admin-page"><p className="empty">加载中…</p></div>

  return (
    <div className="admin-page">
      {/* 页头 */}
      <div className="page-header">
        <button className="admin-btn" onClick={onBack}>← 返回</button>
        <h1>{room.title}</h1>
        <span className={`status-tag ${room.status === 'live' ? 'green' : 'gray'}`}>
          {{ offline: '未开播', live: '直播中', closed: '已结束' }[room.status] || room.status}
        </span>
      </div>

      {msg && <div className="toast" onClick={() => setMsg('')}>{msg}</div>}

      {/* 操作栏 */}
      <div className="action-bar">
        {room.status === 'offline' && <button className="admin-btn primary" onClick={handleGoLive}>开播</button>}
        {room.status !== 'closed' && <button className="admin-btn danger" onClick={handleClose}>⏹ 关播</button>}
      </div>

      {/* Tab 切换 */}
      <div className="tab-bar">
        <button className={`tab-btn ${activeTab === 'products' ? 'active' : ''}`} onClick={() => setActiveTab('products')}>
          商品 ({products.length})
        </button>
        <button className={`tab-btn ${activeTab === 'auctions' ? 'active' : ''}`} onClick={() => setActiveTab('auctions')}>
          竞拍 ({auctions.length})
        </button>
      </div>

      {/* ===== Tab: 商品管理 ===== */}
      {activeTab === 'products' && (
        <>
          <section>
            <div className="section-header">
              <h2>商品列表</h2>
              <button className="admin-btn primary" onClick={() => setShowNewProduct(!showNewProduct)}>
                {showNewProduct ? '收起' : '+ 添加商品'}
              </button>
            </div>

            {showNewProduct && (
              <form className="create-form-card" onSubmit={(e) => { e.preventDefault(); handleCreateProduct() }}>
                <h3>新增商品</h3>
                <div className="field">
                  <label>商品名称 *</label>
                  <input type="text" placeholder="输入商品名称" value={newProductName} onChange={e => setNewProductName(e.target.value)} required />
                </div>
                <div className="field">
                  <label>商品图片</label>
                  <ImageUploader value={newProductImage} onChange={handleImageUrlChange} placeholder="点击或拖拽上传商品封面图" />
                </div>
                <div className="field">
                  <label>描述</label>
                  <textarea placeholder="商品介绍..." value={newProductDesc} onChange={e => setNewProductDesc(e.target.value)} rows={3} />
                </div>
                <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
                  <button type="button" className="admin-btn" onClick={() => { setShowNewProduct(false); setNewProductName(''); setNewProductImage(''); setNewProductDesc('') }}>取消</button>
                  <button type="submit" className="admin-btn primary">保存商品</button>
                </div>
              </form>
            )}

            {/* 商品表格 */}
            {products.length > 0 ? (
              <table className="data-table">
                <thead>
                  <tr><th>缩略图</th><th>名称</th><th>描述</th><th>操作</th></tr>
                </thead>
                <tbody>
                  {products.map(p => (
                    <tr key={p.id}>
                      <td>
                        {p.imageUrl ? (
                          <img src={p.imageUrl} alt="" className="thumb-img" />
                        ) : <span className="no-thumb">无图</span>}
                      </td>
                      <td><strong>{p.name}</strong><br /><span className="meta">ID: #{p.id}</span></td>
                      <td className="desc-cell">{p.description || '-'}</td>
                      <td>
                        <button className="admin-btn small danger" onClick={() => handleDeleteProduct(p.id)}>删除</button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            ) : (
              <p className="empty">暂无商品，请先添加</p>
            )}
          </section>
        </>
      )}

      {/* ===== Tab: 竞拍管理 ===== */}
      {activeTab === 'auctions' && (
        <>
          <section>
            <div className="section-header"><h2>竞拍列表</h2></div>

            {/* 创建竞拍表单 */}
            {room.status !== 'closed' && (
              <div>
                <button className="admin-btn primary" onClick={() => setShowNewAuction(!showNewAuction)} style={{ marginBottom: 12 }}>
                  {showNewAuction ? '收起' : '+ 创建竞拍'}
                </button>
                {showNewAuction && (
                  <form className="create-form-card" onSubmit={(e) => { e.preventDefault(); handleCreateAuction() }}>
                    <h3>新建竞拍</h3>
                    <div className="form-grid-2col">
                      <div className="field">
                        <label>选择商品 *</label>
                        <select value={newAuctionProductId} onChange={e => setNewAuctionProductId(parseInt(e.target.value) || 0)}>
                          <option value={0}>选择商品…</option>
                          {products.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                        </select>
                      </div>
                      <div className="field">
                        <label>竞拍模式 *</label>
                        <select value={newAuctionMode} onChange={e => setNewAuctionMode(e.target.value as any)}>
                          <option value="sudden_death">绝杀模式</option>
                          <option value="extension">延时模式</option>
                        </select>
                      </div>
                      <div className="field">
                        <label>起拍价 (元)</label>
                        <input type="number" placeholder="0" min="0" value={newAuctionStartPrice} onChange={e => setNewAuctionStartPrice(e.target.value)} />
                      </div>
                      <div className="field">
                        <label>加价幅度 (元)</label>
                        <input type="number" placeholder="1.00" step="0.01" value={newAuctionIncrement} onChange={e => setNewAuctionIncrement(e.target.value)} />
                      </div>
                      <div className="field">
                        <label>封顶价 (元)</label>
                        <input type="number" placeholder="100.00" step="0.01" value={newAuctionCap} onChange={e => setNewAuctionCap(e.target.value)} />
                      </div>
                      <div className="field">
                        <label>保留价 (元, 可选)</label>
                        <input type="number" placeholder="留空表示不设保留" value={newAuctionReserve} onChange={e => setNewAuctionReserve(e.target.value)} />
                      </div>
                      <div className="field">
                        <label>时长 (秒)</label>
                        <input type="number" placeholder="300" value={newAuctionDuration} onChange={e => setNewAuctionDuration(e.target.value)} />
                      </div>
                      {(newAuctionMode === 'extension') && (
                        <>
                          <div className="field">
                            <label>延时阈值 (秒)</label>
                            <input type="number" placeholder="10" value={newAuctionExtendThreshold} onChange={e => setNewAuctionExtendThreshold(e.target.value)} />
                          </div>
                          <div className="field">
                            <label>延时时长 (秒)</label>
                            <input type="number" placeholder="15" value={newAuctionExtendDuration} onChange={e => setNewAuctionExtendDuration(e.target.value)} />
                          </div>
                        </>
                      )}
                    </div>
                    <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
                      <button type="button" className="admin-btn" onClick={() => { setShowNewAuction(false); resetAuctionForm() }}>取消</button>
                      <button type="submit" className="admin-btn primary">创建竞拍</button>
                    </div>
                  </form>
                )}
              </div>
            )}

            {/* 竞拍表格 */}
            {auctions.length > 0 ? (
              <table className="data-table">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>商品</th>
                    <th>模式</th>
                    <th>起拍价</th>
                    <th>当前价</th>
                    <th>状态</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {auctions.map(a => {
                    // 价格文案规则
                    let priceLabel = '-'; let priceVal = a.startPriceCents
                    switch (a.status) {
                      case 'sold': priceLabel = '落槌价'; priceVal = a.currentPriceCents; break
                      case 'running': priceLabel = a.currentPriceCents > 0 ? '当前最高价' : '起拍价'; priceVal = a.currentPriceCents > 0 ? a.currentPriceCents : a.startPriceCents; break
                      case 'scheduled': priceLabel = '起拍价'; break
                    }

                    return (
                      <tr key={a.id}>
                        <td>#{a.id}</td>
                        <td>{products.find(p => p.id === a.productId)?.name || `商品#${a.productId}`}</td>
                        <td>{a.mode === 'sudden_death' ? '绝杀' : '延时'}</td>
                        <td>&yen;{formatCents(a.startPriceCents)}</td>
                        <td><strong>&yen;{formatCents(priceVal)}</strong><br/><span style={{ fontSize: 11, color: '#888' }}>{priceLabel}</span></td>
                        <td>{statusBadge(a.status)}</td>
                        <td>
                          <div className="action-cell">
                            {a.status === 'draft' && <button className="admin-btn small" onClick={() => handlePublish(a.id)}>发布</button>}
                            {a.status === 'scheduled' && <button className="admin-btn small primary" onClick={() => handleStart(a.id)}>开始</button>}
                            {(a.status === 'draft' || a.status === 'scheduled') && <button className="admin-btn small danger" onClick={() => handleCancel(a.id)}>取消</button>}
                            {(a.status === 'sold' || a.status === 'failed') && <button className="admin-btn small" onClick={() => handleSettle(a.id)}>结算</button>}
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            ) : (
              <p className="empty">暂无竞拍，请先添加商品后创建竞拍</p>
            )}
          </section>
        </>
      )}
    </div>
  )
}

function statusBadge(s: string): React.ReactNode {
  const map: Record<string, { text: string; cls: string }> = {
    draft:     { text: '草稿', cls: 'badge-gray' },
    scheduled: { text: '待开始', cls: 'badge-blue' },
    running:   { text: '竞拍中', cls: 'badge-red' },
    sold:      { text: '已成交', cls: 'badge-green' },
    failed:    { text: '流拍', cls: 'badge-gray' },
    cancelled: { text: '已取消', cls: 'badge-gray' },
  }
  const info = map[s] || { text: s, cls: 'badge-gray' }
  return <span className={`status-badge ${info.cls}`}>{info.text}</span>
}
