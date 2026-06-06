import { useEffect, useState } from 'react'
import { listProducts, createProduct, deleteProduct, type Product } from '../api/client'
import ImageUploader from '../components/ImageUploader'

export default function ProductListPage() {
  const [products, setProducts] = useState<Product[]>([])
  const [selectedIds, setSelectedIds] = useState<Set<number>>(new Set())
  const [showCreate, setShowCreate] = useState(false)
  // editing 状态预留用于后续编辑功能
  const [_editing, setEditing] = useState<Product | null>(null)
  const [msg, setMsg] = useState('')

  // 表单字段
  const [name, setName] = useState('')
  const [desc, setDesc] = useState('')
  const [imageUrl, setImageUrl] = useState('')

  const load = () => { listProducts().then(setProducts).catch(() => {}) }
  useEffect(load, [])

  const resetForm = () => { setName(''); setDesc(''); setImageUrl(''); setEditing(null) }

  const handleCreate = async () => {
    if (!name.trim()) { setMsg('请输入商品名称'); return }
    try {
      await createProduct(name.trim(), imageUrl || '', desc.trim())
      setMsg('商品已添加'); setShowCreate(false); resetForm(); load()
    } catch (err: any) { setMsg(err.message) }
  }

  const handleDelete = async (id: number) => {
    if (!confirm('确定删除该商品？')) return
    try { await deleteProduct(id); setMsg('商品已删除'); load() } catch (err: any) { setMsg(err.message) }
  }

  const handleBatchDelete = async () => {
    if (!confirm(`确定删除选中的 ${selectedIds.size} 个商品？`)) return
    const ids = Array.from(selectedIds)
    const results = await Promise.allSettled(ids.map(id => deleteProduct(id)))
    const successCount = results.filter(r => r.status === 'fulfilled').length
    const failCount = results.length - successCount
    setSelectedIds(new Set())
    setMsg(`已删除 ${successCount} 个商品${failCount > 0 ? `，${failCount} 个失败` : ''}`)
    load()
  }

  const toggleSelect = (id: number) => {
    const next = new Set(selectedIds)
    if (next.has(id)) next.delete(id); else next.add(id)
    setSelectedIds(next)
  }
  const selectAll = () => {
    if (selectedIds.size === products.length) setSelectedIds(new Set())
    else setSelectedIds(new Set(products.map(p => p.id)))
  }

  return (
    <div className="admin-page">
      <div className="page-header">
        <h1>商品管理</h1>
        <button className="admin-btn primary" onClick={() => { setShowCreate(!showCreate); resetForm() }}>
          {showCreate ? '取消' : '+ 新建商品'}
        </button>
      </div>

      {msg && <div className="toast" onClick={() => setMsg('')}>{msg}</div>}

      {showCreate && (
        <form className="create-form-card" onSubmit={(e) => { e.preventDefault(); handleCreate() }}>
          <h3>新建商品</h3>
          <div className="form-grid-2col">
            <div className="field">
              <label>商品名称 *</label>
              <input type="text" placeholder="输入商品名称" value={name} onChange={e => setName(e.target.value)} required />
            </div>
            <div className="field">
              <label>商品图片</label>
              <ImageUploader value={imageUrl} onChange={setImageUrl} placeholder="点击或拖拽上传" />
            </div>
          </div>
          <div className="field">
            <label>描述</label>
            <textarea placeholder="商品介绍..." value={desc} onChange={e => setDesc(e.target.value)} rows={3} />
          </div>
          <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 16 }}>
            <button type="button" className="admin-btn" onClick={() => { setShowCreate(false); resetForm() }}>取消</button>
            <button type="submit" className="admin-btn primary">保存商品</button>
          </div>
        </form>
      )}

      {/* 批量操作栏 */}
      {products.length > 0 && (
        <div className="batch-bar">
          <label className="batch-check">
            <input type="checkbox" checked={selectedIds.size === products.length && products.length > 0} onChange={selectAll} />
            全选
          </label>
          {selectedIds.size > 0 && (
            <button className="admin-btn small danger" onClick={handleBatchDelete}>删除选中 ({selectedIds.size})</button>
          )}
        </div>
      )}

      {/* 商品表格 */}
      {products.length > 0 ? (
        <table className="data-table">
          <thead>
            <tr>
              <th style={{ width: 40 }}></th>
              <th>缩略图</th>
              <th>名称</th>
              <th>描述</th>
              <th>创建时间</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            {products.map(p => (
              <tr key={p.id}>
                <td><input type="checkbox" checked={selectedIds.has(p.id)} onChange={() => toggleSelect(p.id)} /></td>
                <td>
                  {p.imageUrl ? (
                    <img src={p.imageUrl} alt="" className="thumb-img" />
                  ) : <span className="no-thumb">无图</span>}
                </td>
                <td><strong>{p.name}</strong><br /><span className="meta">ID: #{p.id}</span></td>
                <td className="desc-cell">{p.description || '-'}</td>
                <td>{new Date(p.createdAt).toLocaleDateString('zh-CN')}</td>
                <td>
                  <div className="action-cell">
                    <button className="admin-btn small danger" onClick={() => handleDelete(p.id)}>删除</button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : (
        <div className="empty-state-box">
          <div className="empty-icon">[商品]</div>
          <p>暂无商品</p>
          <p className="sub">点击右上角"新建商品"添加</p>
        </div>
      )}
    </div>
  )
}
