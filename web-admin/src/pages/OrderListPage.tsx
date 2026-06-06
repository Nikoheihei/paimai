import { useEffect, useState } from 'react'
import { listOrders, type Order } from '../api/client'

function formatCents(c: number) { return (c / 100).toFixed(2) }

const statusLabel: Record<string, string> = {
  pending_payment: '待付款',
  paid: '已付款',
  closed: '已关闭',
}

const statusBadgeClass: Record<string, string> = {
  pending_payment: 'badge-orange',
  paid: 'badge-green',
  closed: 'badge-gray',
}

export default function OrderListPage() {
  const [orders, setOrders] = useState<Order[]>([])
  const [filtered, setFiltered] = useState<Order[]>([])
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<string>('')
  const [dateFrom, setDateFrom] = useState('')
  const [dateTo, setDateTo] = useState('')
  const [detail, setDetail] = useState<Order | null>(null)
  const [msg, setMsg] = useState('')
  const [page, setPage] = useState(1)
  const PAGE_SIZE = 20

  const load = () => { listOrders().then(setOrders).catch(() => {}) }
  useEffect(load, [])

  // 监听订单刷新事件（竞拍成交后自动刷新）
  useEffect(() => {
    const handler = () => { load() }
    window.addEventListener('order:refresh', handler)
    return () => window.removeEventListener('order:refresh', handler)
  }, [])

  useEffect(() => {
    let result = orders
    if (search.trim()) {
      const q = search.trim().toLowerCase()
      result = result.filter(o => String(o.id).includes(q) || String(o.buyerId).includes(q) || String(o.auctionId).includes(q))
    }
    if (statusFilter) result = result.filter(o => o.status === statusFilter)
    if (dateFrom) result = result.filter(o => new Date(o.createdAt) >= new Date(dateFrom))
    if (dateTo) result = result.filter(o => new Date(o.createdAt) <= new Date(dateTo + 'T23:59:59'))
    setFiltered(result)
    setPage(1)
  }, [search, statusFilter, dateFrom, dateTo, orders])

  const handleExport = () => {
    const rows = filtered.map(o => `${o.id},${o.auctionId},${o.buyerId},${o.finalPriceCents},${o.status},${o.createdAt}`)
    const csv = '订单ID,竞拍ID,买家ID,金额(分),状态,创建时间\n' + rows.join('\n')
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8;' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url; a.download = `orders_${new Date().toISOString().slice(0,10)}.csv`; a.click()
    URL.revokeObjectURL(url)
  }

  const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE))
  const paged = filtered.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE)

  return (
    <div className="admin-page">
      <div className="page-header">
        <h1>订单管理</h1>
        <button className="admin-btn" onClick={handleExport}>导出 CSV</button>
      </div>

      {msg && <div className="toast" onClick={() => setMsg('')}>{msg}</div>}

      {/* 筛选栏 */}
      <div className="filter-bar">
        <input type="text" placeholder="搜索订单/竞拍/买家ID..." value={search} onChange={e => setSearch(e.target.value)} className="filter-input" />
        <select value={statusFilter} onChange={e => setStatusFilter(e.target.value)} className="filter-select">
          <option value="">全部状态</option>
          <option value="pending_payment">待付款</option>
          <option value="paid">已付款</option>
          <option value="closed">已关闭</option>
        </select>
        <input type="date" value={dateFrom} onChange={e => setDateFrom(e.target.value)} className="filter-date" />
        <span style={{ color: '#999' }}>~</span>
        <input type="date" value={dateTo} onChange={e => setDateTo(e.target.value)} className="filter-date" />
      </div>

      {/* 订单表格 */}
      {paged.length > 0 ? (
        <>
          <table className="data-table">
            <thead>
              <tr>
                <th>订单ID</th>
                <th>竞拍ID</th>
                <th>买家ID</th>
                <th>金额</th>
                <th>状态</th>
                <th>创建时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {paged.map(o => (
                <tr key={o.id} onClick={() => setDetail(o)} style={{ cursor: 'pointer' }}>
                  <td>#{o.id}</td>
                  <td>#{o.auctionId}</td>
                  <td>#{o.buyerId}</td>
                  <td><strong>¥{formatCents(o.finalPriceCents)}</strong></td>
                  <td><span className={`status-badge ${statusBadgeClass[o.status] || 'badge-gray'}`}>{statusLabel[o.status] || o.status}</span></td>
                  <td>{new Date(o.createdAt).toLocaleString('zh-CN')}</td>
                  <td>
                    <span className={`status-badge ${statusBadgeClass[o.status] || 'badge-gray'}`}>{statusLabel[o.status] || o.status}</span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>

          {/* 分页 */}
          <div className="pagination">
            <button className="admin-btn small" disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一页</button>
            <span style={{ fontSize: 14, color: '#666' }}>第 {page} / {totalPages} 页（共 {filtered.length} 条）</span>
            <button className="admin-btn small" disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>下一页</button>
          </div>
        </>
      ) : (
        <div className="empty-state-box">
          <div className="empty-icon">[订单]</div>
          <p>暂无订单</p>
        </div>
      )}

      {/* 详情弹窗 */}
      {detail && (
        <div className="modal-overlay" onClick={() => setDetail(null)}>
          <div className="modal-card" onClick={e => e.stopPropagation()}>
            <div className="modal-header">
              <h3>订单详情 #{detail.id}</h3>
              <button className="modal-close" onClick={() => setDetail(null)}>×</button>
            </div>
            <div className="modal-body">
              <div className="info-row"><span>竞拍ID</span><span>#{detail.auctionId}</span></div>
              <div className="info-row"><span>商品ID</span><span>#{detail.productId}</span></div>
              <div className="info-row"><span>买家ID</span><span>#{detail.buyerId}</span></div>
              <div className="info-row"><span>卖家ID</span><span>#{detail.sellerId}</span></div>
              <div className="info-row"><span>最终价格</span><span className="price">¥{formatCents(detail.finalPriceCents)}</span></div>
              <div className="info-row"><span>状态</span><span className={`status-badge ${statusBadgeClass[detail.status]}`}>{statusLabel[detail.status]}</span></div>
              <div className="info-row"><span>创建时间</span><span>{new Date(detail.createdAt).toLocaleString('zh-CN')}</span></div>
              {detail.paidAt && <div className="info-row"><span>支付时间</span><span>{new Date(detail.paidAt).toLocaleString('zh-CN')}</span></div>}
            </div>
            <div className="modal-footer">
              <button className="admin-btn" onClick={() => setDetail(null)}>关闭</button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
