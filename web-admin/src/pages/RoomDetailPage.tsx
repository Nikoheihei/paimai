import { useCallback, useEffect, useMemo, useState } from 'react'
import { getRoom, goLive, closeRoom, listProducts, createProduct, deleteProduct, offlineProduct, listAuctions, updateAuction, publishAuction, startAuction, cancelAuction, relistProduct, settleAuction, listOrders, type LiveRoom, type Product, type Auction, type Order } from '../api/client'
import ImageUploader from '../components/ImageUploader'

function formatCents(c: number) { return (c / 100).toFixed(2) }
function durationFromAuction(a: Auction): string {
  const start = new Date(a.startAt).getTime()
  const end = new Date(a.endAt).getTime()
  if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return '300'
  return String(Math.max(1, Math.round((end - start) / 1000)))
}
function normalizeDuration(value: string, fallback = 300): number {
  const parsed = parseInt(value, 10)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}
function yuanToCents(value: string, fallbackCents: number) {
  const parsed = Number(value)
  if (!Number.isFinite(parsed) || parsed < 0) return fallbackCents
  return Math.round(parsed * 100)
}
function optionalYuanToCents(value: string) {
  if (!value.trim()) return null
  return yuanToCents(value, 0)
}
function toDatetimeLocalValue(date: Date) {
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60000)
  return local.toISOString().slice(0, 16)
}

type TabKey = 'products' | 'auctions'
type ListingLaunchMode = 'manual' | 'scheduled' | 'immediate'

type Props = { roomId: number; onBack: () => void }

export default function RoomDetailPage({ roomId, onBack }: Props) {
  const [room, setRoom] = useState<LiveRoom | null>(null)
  const [products, setProducts] = useState<Product[]>([])
  const [auctions, setAuctions] = useState<Auction[]>([])
  const [orders, setOrders] = useState<Order[]>([])
  const [auctionDurations, setAuctionDurations] = useState<Record<number, string>>({})
  const [activeTab, setActiveTab] = useState<TabKey>('products')
  const [msg, setMsg] = useState('')

  // 商品创建表单
  const [newProductName, setNewProductName] = useState('')
  const [newProductDesc, setNewProductDesc] = useState('')
  const [newProductImage, setNewProductImage] = useState('')
  const [newProductStock, setNewProductStock] = useState('1')
  const [showNewProduct, setShowNewProduct] = useState(false)

  // 上架配置表单（底层仍复用 auction 状态机）
  const [newAuctionProductId, setNewAuctionProductId] = useState(0)
  const [newAuctionMode, setNewAuctionMode] = useState<'sudden_death' | 'extension' | 'reserve'>('sudden_death')
  const [newAuctionStartPrice, setNewAuctionStartPrice] = useState('0')
  const [newAuctionIncrement, setNewAuctionIncrement] = useState('1.00')
  const [newAuctionCap, setNewAuctionCap] = useState('100.00')
  const [newAuctionReserve, setNewAuctionReserve] = useState('')
  const [newAuctionDuration, setNewAuctionDuration] = useState('300')
  const [newAuctionExtendThreshold, setNewAuctionExtendThreshold] = useState('10')
  const [newAuctionExtendDuration, setNewAuctionExtendDuration] = useState('15')
  const [newListingLaunchMode, setNewListingLaunchMode] = useState<ListingLaunchMode>('manual')
  const [newListingStartAt, setNewListingStartAt] = useState('')
  const [showNewAuction, setShowNewAuction] = useState(false)

  const syncAuctions = useCallback((list: Auction[]) => {
    setAuctions(list)
    setAuctionDurations(prev => {
      const next = { ...prev }
      list.forEach(a => {
        if (!next[a.id]) next[a.id] = durationFromAuction(a)
      })
      return next
    })
  }, [])
  const loadAuctions = useCallback(() => {
    listAuctions(roomId).then(syncAuctions).catch(() => {})
  }, [roomId, syncAuctions])
  const loadOrders = useCallback(() => {
    listOrders().then(setOrders).catch(() => {})
  }, [])
  const load = useCallback(() => {
    getRoom(roomId).then(setRoom).catch(() => {})
    listProducts().then(setProducts).catch(() => {})
    loadAuctions()
    loadOrders()
  }, [roomId, loadAuctions, loadOrders])
  useEffect(load, [load])
  useEffect(() => {
    const timer = window.setInterval(load, 10000)
    return () => window.clearInterval(timer)
  }, [load])
  useEffect(() => {
    const timer = window.setInterval(loadOrders, 2000)
    return () => window.clearInterval(timer)
  }, [loadOrders])

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
    const stock = Math.max(1, parseInt(newProductStock, 10) || 1)
    try {
      await createProduct(newProductName.trim(), newProductImage || '', newProductDesc.trim(), stock)
      setNewProductName(''); setNewProductDesc(''); setNewProductImage(''); setNewProductStock('1')
      setShowNewProduct(false)
      listProducts().then(setProducts)
      setMsg('商品已添加')
    } catch (err: any) { setMsg(err.message) }
  }
  const handleDeleteProduct = async (id: number) => {
    if (!confirm('确定删除该商品？')) return
    try { await deleteProduct(id); listProducts().then(setProducts); setMsg('商品已删除') } catch (err: any) { setMsg(err.message) }
  }
  const handleOfflineProduct = async (id: number) => {
    if (!confirm('确定下架该商品？下架后不会继续参与竞拍。')) return
    try { await offlineProduct(id); listProducts().then(setProducts); setMsg('商品已下架') } catch (err: any) { setMsg(err.message) }
  }
  const handleConfigureListing = (product: Product) => {
    if ((product.stock ?? 0) <= 0) {
      setMsg('库存不足，无法上架')
      return
    }
    setNewAuctionProductId(product.id)
    setNewListingLaunchMode('manual')
    setShowNewAuction(true)
    setActiveTab('auctions')
    setMsg(`正在为「${product.name}」配置上架`)
  }

  // ImageUploader 的 onChange 返回 URL（base64 占位），这里直接用
  const handleImageUrlChange = (url: string) => { setNewProductImage(url) }

  // === 竞拍操作 ===
  const handleCreateAuction = async () => {
    if (!newAuctionProductId) { setMsg('请选择商品'); return }
    const reservePrice = optionalYuanToCents(newAuctionReserve)
    if (newAuctionMode === 'reserve' && reservePrice == null) {
      setMsg('保留价模式需要填写保底价')
      return
    }
    let startAt = new Date()
    if (newListingLaunchMode === 'scheduled') {
      if (!newListingStartAt) {
        setMsg('请选择定时上架时间')
        return
      }
      startAt = new Date(newListingStartAt)
      if (!Number.isFinite(startAt.getTime())) {
        setMsg('定时上架时间不正确')
        return
      }
    }
    try {
      const durationSec = normalizeDuration(newAuctionDuration, 300)
      const endAt = new Date(startAt.getTime() + durationSec * 1000)
      const auction = await relistProduct(
        newAuctionProductId,
        roomId,
        newAuctionMode,
        yuanToCents(newAuctionStartPrice, 0),
        yuanToCents(newAuctionIncrement, 100),
        yuanToCents(newAuctionCap, 10000),
        reservePrice,
        newAuctionMode === 'extension' ? parseInt(newAuctionExtendThreshold) || 10 : 0,
        newAuctionMode === 'extension' ? parseInt(newAuctionExtendDuration) || 15 : 0,
        startAt.toISOString(),
        endAt.toISOString(),
      )
      if (newListingLaunchMode === 'scheduled') {
        await publishAuction(auction.id)
      }
      if (newListingLaunchMode === 'immediate') {
        await publishAuction(auction.id)
        await startAuction(auction.id, durationSec)
      }
      setAuctionDurations(prev => ({ ...prev, [auction.id]: String(durationSec) }))
      setMsg(newListingLaunchMode === 'manual' ? '上架配置已保存' : newListingLaunchMode === 'scheduled' ? '定时上架已保存' : '商品已上架')
      loadAuctions()
      listProducts().then(setProducts)
      setShowNewAuction(false)
      resetAuctionForm()
    } catch (err: any) { setMsg(err.message) }
  }
  const handleManualList = async (auction: Auction) => {
    try {
      if (auction.status === 'draft') {
        await publishAuction(auction.id)
      }
      await startAuction(auction.id, normalizeDuration(auctionDurations[auction.id] || durationFromAuction(auction)))
      setMsg('商品已上架')
      loadAuctions()
      listProducts().then(setProducts)
    } catch (err: any) { setMsg(err.message) }
  }
  const handleSaveDuration = async (auction: Auction) => {
    const durationSec = normalizeDuration(auctionDurations[auction.id] || durationFromAuction(auction))
    const scheduledStart = new Date(auction.startAt)
    const startAt = auction.status === 'scheduled' && Number.isFinite(scheduledStart.getTime()) ? scheduledStart : new Date()
    const endAt = new Date(startAt.getTime() + durationSec * 1000)
    try {
      await updateAuction(auction.id, { startAt: startAt.toISOString(), endAt: endAt.toISOString() })
      setMsg(`上架计划 #${auction.id} 时长已保存为 ${durationSec} 秒`)
      loadAuctions()
    } catch (err: any) { setMsg(err.message) }
  }
  const handleCancel = async (id: number) => {
    const reason = prompt('取消原因（可选）') || ''
    try { await cancelAuction(id, reason); setMsg('已取消'); loadAuctions() } catch (err: any) { setMsg(err.message) }
  }
  const handleSettle = async (id: number) => {
    if (!confirm('确定手动结算此竞拍？')) return
    try { await settleAuction(id); setMsg('已结算'); loadAuctions(); listProducts().then(setProducts) } catch (err: any) { setMsg(err.message) }
  }
  const resetAuctionForm = () => {
    setNewAuctionProductId(0); setNewAuctionMode('sudden_death')
    setNewAuctionStartPrice('0'); setNewAuctionIncrement('1.00')
    setNewAuctionCap('100.00'); setNewAuctionReserve('')
    setNewAuctionDuration('300'); setNewAuctionExtendThreshold('10')
    setNewAuctionExtendDuration('15')
    setNewListingLaunchMode('manual'); setNewListingStartAt('')
  }
  const handleLaunchModeChange = (mode: ListingLaunchMode) => {
    setNewListingLaunchMode(mode)
    if (mode === 'scheduled' && !newListingStartAt) {
      setNewListingStartAt(toDatetimeLocalValue(new Date(Date.now() + 60 * 1000)))
    }
  }
  const listingSubmitText = () => {
    if (newListingLaunchMode === 'scheduled') return '保存定时上架'
    if (newListingLaunchMode === 'immediate') return '立即上架'
    return '保存上架配置'
  }
  const auctionModeLabel = (mode: string) => {
    if (mode === 'extension') return '延时'
    if (mode === 'reserve') return '保留价'
    return '绝杀'
  }
  const availableProducts = products.filter(p => (p.stock ?? 0) > 0 && p.status !== 'offline')
  const ordersByAuctionId = useMemo(() => {
    const map: Record<number, Order> = {}
    orders.forEach(order => { map[order.auctionId] = order })
    return map
  }, [orders])

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
          商品库 ({products.length})
        </button>
        <button className={`tab-btn ${activeTab === 'auctions' ? 'active' : ''}`} onClick={() => setActiveTab('auctions')}>
          上架计划 ({auctions.length})
        </button>
      </div>

      {/* ===== Tab: 商品库 ===== */}
      {activeTab === 'products' && (
        <>
          <section>
            <div className="section-header">
              <h2>商品库</h2>
              <button className="admin-btn primary" onClick={() => setShowNewProduct(!showNewProduct)}>
                {showNewProduct ? '收起' : '+ 添加商品'}
              </button>
            </div>

            {showNewProduct && (
              <form className="create-form-card" onSubmit={(e) => { e.preventDefault(); handleCreateProduct() }}>
                <h3>新增商品档案</h3>
                <div className="field">
                  <label>商品名称 *</label>
                  <input type="text" placeholder="输入商品名称" value={newProductName} onChange={e => setNewProductName(e.target.value)} required />
                </div>
                <div className="field">
                  <label>商品图片</label>
                  <ImageUploader value={newProductImage} onChange={handleImageUrlChange} placeholder="点击或拖拽上传商品封面图" />
                </div>
                <div className="field">
                  <label>库存 *</label>
                  <input type="number" min="1" step="1" value={newProductStock} onChange={e => setNewProductStock(e.target.value)} required />
                </div>
                <div className="field">
                  <label>描述</label>
                  <textarea placeholder="商品介绍..." value={newProductDesc} onChange={e => setNewProductDesc(e.target.value)} rows={3} />
                </div>
                <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
                  <button type="button" className="admin-btn" onClick={() => { setShowNewProduct(false); setNewProductName(''); setNewProductImage(''); setNewProductDesc(''); setNewProductStock('1') }}>取消</button>
                  <button type="submit" className="admin-btn primary">保存商品</button>
                </div>
              </form>
            )}

            {/* 商品表格 */}
            {products.length > 0 ? (
              <table className="data-table">
                <thead>
                  <tr><th>缩略图</th><th>名称</th><th>描述</th><th>库存</th><th>状态</th><th>操作</th></tr>
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
                      <td><strong>{p.stock ?? '-'}</strong></td>
                      <td>{productStatusBadge(p.status)}</td>
                      <td>
                        <div className="action-cell">
                          <button className="admin-btn small primary" disabled={(p.stock ?? 0) <= 0} onClick={() => handleConfigureListing(p)}>
                            创建上架
                          </button>
                          {p.status !== 'offline' && <button className="admin-btn small" disabled={p.status === 'locked'} onClick={() => handleOfflineProduct(p.id)}>下架</button>}
                          <button className="admin-btn small danger" onClick={() => handleDeleteProduct(p.id)}>删除</button>
                        </div>
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

      {/* ===== Tab: 上架计划 ===== */}
      {activeTab === 'auctions' && (
        <>
          <section>
            <div className="section-header"><h2>上架计划</h2></div>

            {/* 创建上架计划表单 */}
            {room.status !== 'closed' && (
              <div>
                <button className="admin-btn primary" onClick={() => setShowNewAuction(!showNewAuction)} style={{ marginBottom: 12 }}>
                  {showNewAuction ? '收起' : '+ 配置上架商品'}
                </button>
                {showNewAuction && (
                  <form className="create-form-card" onSubmit={(e) => { e.preventDefault(); handleCreateAuction() }}>
                    <h3>商品上架配置</h3>
                    <div className="form-grid-2col">
                      <div className="field">
                        <label>商品 *</label>
                        <select value={newAuctionProductId} onChange={e => setNewAuctionProductId(parseInt(e.target.value) || 0)}>
                          <option value={0}>选择商品…</option>
                          {availableProducts.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                        </select>
                      </div>
                      <div className="field">
                        <label>上架方式 *</label>
                        <select value={newListingLaunchMode} onChange={e => handleLaunchModeChange(e.target.value as ListingLaunchMode)}>
                          <option value="manual">先保存，稍后手动上架</option>
                          <option value="scheduled">定时上架</option>
                          <option value="immediate">保存后立即上架</option>
                        </select>
                      </div>
                      {newListingLaunchMode === 'scheduled' && (
                        <div className="field">
                          <label>定时上架时间 *</label>
                          <input type="datetime-local" value={newListingStartAt} onChange={e => setNewListingStartAt(e.target.value)} />
                        </div>
                      )}
                      <div className="field">
                        <label>竞拍模式 *</label>
                        <select value={newAuctionMode} onChange={e => setNewAuctionMode(e.target.value as any)}>
                          <option value="sudden_death">绝杀竞拍</option>
                          <option value="extension">延时竞拍</option>
                          <option value="reserve">保留价竞拍</option>
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
                        <label>{newAuctionMode === 'reserve' ? '保底价 (元) *' : '保底价 (元, 可选)'}</label>
                        <input type="number" placeholder="留空表示不设保底价" value={newAuctionReserve} onChange={e => setNewAuctionReserve(e.target.value)} />
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
                      <button type="submit" className="admin-btn primary">{listingSubmitText()}</button>
                    </div>
                  </form>
                )}
              </div>
            )}

            {/* 上架计划表格 */}
            {auctions.length > 0 ? (
              <table className="data-table">
                <thead>
                  <tr>
                    <th>ID</th>
                    <th>商品</th>
                    <th>模式</th>
                    <th>起拍价</th>
                    <th>当前价</th>
                    <th>上架时长</th>
                    <th>状态</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {auctions.map(a => {
                    const order = ordersByAuctionId[a.id]
                    // 价格文案规则
                    let priceLabel = '-'; let priceVal = a.startPriceCents
                    switch (a.status) {
                      case 'sold': priceLabel = '落槌价'; priceVal = a.currentPriceCents; break
                      case 'payment_timeout': priceLabel = '失效成交价'; priceVal = a.currentPriceCents; break
                      case 'running': priceLabel = a.currentPriceCents > 0 ? '当前最高价' : '起拍价'; priceVal = a.currentPriceCents > 0 ? a.currentPriceCents : a.startPriceCents; break
                      case 'scheduled': priceLabel = '起拍价'; break
                      case 'draft': priceLabel = '起拍价'; break
                    }

                    return (
                      <tr key={a.id}>
                        <td>#{a.id}</td>
                        <td>{products.find(p => p.id === a.productId)?.name || `商品#${a.productId}`}</td>
                        <td>{auctionModeLabel(a.mode)}</td>
                        <td>&yen;{formatCents(a.startPriceCents)}</td>
                        <td><strong>&yen;{formatCents(priceVal)}</strong><br/><span style={{ fontSize: 11, color: '#888' }}>{priceLabel}</span></td>
                        <td>
                          {(a.status === 'draft' || a.status === 'scheduled') ? (
                            <input
                              className="duration-input"
                              type="number"
                              min="1"
                              value={auctionDurations[a.id] || durationFromAuction(a)}
                              onChange={e => setAuctionDurations(prev => ({ ...prev, [a.id]: e.target.value }))}
                            />
                          ) : (
                            <span>{durationFromAuction(a)} 秒</span>
                          )}
                        </td>
                        <td>{statusBadge(a.status, order)}</td>
                        <td>
                          <div className="action-cell">
                            {(a.status === 'draft' || a.status === 'scheduled') && <>
                              <button className="admin-btn small" onClick={() => handleSaveDuration(a)}>保存时长</button>
                              <button className="admin-btn small primary" onClick={() => handleManualList(a)}>手动上架</button>
                              <button className="admin-btn small danger" onClick={() => handleCancel(a.id)}>取消</button>
                            </>}
                            {a.status === 'running' && <button className="admin-btn small primary" onClick={() => handleSettle(a.id)}>结算</button>}
                            {a.status === 'sold' && order?.status === 'paid' && <span className="status-badge badge-green">成交完成</span>}
                            {a.status === 'sold' && order?.status === 'closed' && <button className="admin-btn small primary" onClick={() => handleConfigureListing({ id: a.productId, name: products.find(p => p.id === a.productId)?.name || '', stock: 1 } as Product)}>重新开拍</button>}
                            {a.status === 'sold' && (!order || order.status === 'pending_payment') && <span className="status-badge badge-blue">等待买家支付</span>}
                            {(a.status === 'failed' || a.status === 'cancelled' || a.status === 'payment_timeout') && <button className="admin-btn small primary" onClick={() => handleConfigureListing({ id: a.productId, name: products.find(p => p.id === a.productId)?.name || '', stock: 1 } as Product)}>重新开拍</button>}
                          </div>
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
            ) : (
              <p className="empty">暂无上架计划，请先从商品库选择商品并配置上架</p>
            )}
          </section>
        </>
      )}
    </div>
  )
}

function statusBadge(s: string, order?: Order) {
  if (s === 'sold' && order?.status === 'paid') {
    return <span className="status-badge badge-green">成交完成</span>
  }
  if (s === 'sold' && order?.status === 'closed') {
    return <span className="status-badge badge-gray">订单已关闭</span>
  }
  const map: Record<string, { text: string; cls: string }> = {
    draft:     { text: '待手动上架', cls: 'badge-gray' },
    scheduled: { text: '定时待上架', cls: 'badge-blue' },
    running:   { text: '已上架竞拍中', cls: 'badge-red' },
    sold:      { text: '已成交待支付', cls: 'badge-orange' },
    paid:      { text: '成交完成', cls: 'badge-green' },
    payment_timeout: { text: '支付超时', cls: 'badge-gray' },
    failed:    { text: '流拍', cls: 'badge-gray' },
    cancelled: { text: '已取消', cls: 'badge-gray' },
  }
  const info = map[s] || { text: s, cls: 'badge-gray' }
  return <span className={`status-badge ${info.cls}`}>{info.text}</span>
}

function productStatusBadge(status?: Product['status']) {
  const map: Record<string, { text: string; cls: string }> = {
    available: { text: '可上架', cls: 'badge-green' },
    locked: { text: '已占用', cls: 'badge-blue' },
    offline: { text: '已下架', cls: 'badge-gray' },
  }
  const info = map[status || 'available'] || { text: status || '可上架', cls: 'badge-gray' }
  return <span className={`status-badge ${info.cls}`}>{info.text}</span>
}
