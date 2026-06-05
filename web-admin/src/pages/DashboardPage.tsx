import { useEffect, useState } from 'react'
import { listRooms, listProducts, listAuctions, listOrders, type Product, type Auction } from '../api/client'

function formatCents(c: number) { return (c / 100).toFixed(2) }

export default function DashboardPage() {
  const [stats, setStats] = useState({ rooms: 0, products: 0, running: 0, todayRevenue: 0 })
  const [recentAuctions, setRecentAuctions] = useState<Auction[]>([])
  const [products, setProducts] = useState<Product[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    Promise.all([
      listRooms(),
      listProducts(),
      listAuctions(),
      listOrders(),
    ]).then(([rooms, products, allAuctions, orders]) => {
      const today = new Date().toDateString()
      // #5 修复: 用 paidAt 而非 createdAt 计算今日成交额
      const todayRevenue = orders
        .filter(o => o.status === 'paid' && o.paidAt && new Date(o.paidAt).toDateString() === today)
        .reduce((sum, o) => sum + o.finalPriceCents, 0)

      setStats({
        rooms: rooms.length,
        products: products.length,
        running: allAuctions.filter(a => a.status === 'running').length,
        todayRevenue,
      })
      setProducts(products)
      // #1 修复: 最近竞拍动态从 listAuctions 取最近 5 条
      setRecentAuctions(allAuctions.slice(0, 5))
      setLoading(false)
    }).catch(() => setLoading(false))
  }, [])

  const statCards = [
    { label: '直播间数', value: stats.rooms, icon: '📺', color: '#ff6b35' },
    { label: '商品总数', value: stats.products, icon: '📦', color: '#1890ff' },
    { label: '进行中竞拍', value: stats.running, icon: '⚡', color: '#52c41a' },
    { label: '今日成交额', value: `¥${formatCents(stats.todayRevenue)}`, icon: '💰', color: '#faad14' },
  ]

  if (loading) return <div className="admin-page"><p className="empty">加载中…</p></div>

  return (
    <div className="admin-page">
      <div className="page-header">
        <h1>📊 商家数据概览</h1>
      </div>

      {/* 统计卡片 */}
      <div className="dashboard-stats">
        {statCards.map(card => (
          <div key={card.label} className="stat-card">
            <div className="stat-icon" style={{ background: `${card.color}15`, color: card.color }}>{card.icon}</div>
            <div className="stat-value" style={{ color: card.color }}>{card.value}</div>
            <div className="stat-label">{card.label}</div>
          </div>
        ))}
      </div>

      {/* 快捷操作 */}
      <div className="dashboard-actions">
        <button className="admin-btn primary" onClick={() => window.location.hash = '#/rooms'}>📺 管理直播间</button>
        <button className="admin-btn primary" onClick={() => window.location.hash = '#/products'}>📦 管理商品</button>
        <button className="admin-btn" onClick={() => window.location.hash = '#/orders'}>📋 查看订单</button>
      </div>

      {/* 最近竞拍动态 */}
      <section>
        <div className="section-header"><h2>最近竞拍动态</h2></div>
        {recentAuctions.length > 0 ? (
          <table className="data-table">
            <thead>
              <tr><th>竞拍ID</th><th>商品</th><th>状态</th><th>成交价</th><th>操作</th></tr>
            </thead>
            <tbody>
              {recentAuctions.map(a => (
                <tr key={a.id}>
                  <td>#{a.id}</td>
                  <td>{products.find(p => p.id === a.productId)?.name || `商品#${a.productId}`}</td>
                  <td>{statusBadge(a.status as string)}</td>
                  <td><strong>¥{formatCents(a.currentPriceCents)}</strong></td>
                  <td>
                    <button className="admin-btn small" onClick={() => window.location.hash = `#/rooms`}>查看</button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <p className="empty">暂无竞拍记录</p>
        )}
      </section>
    </div>
  )
}

function statusBadge(s: string) {
  const map: Record<string, { text: string; cls: string }> = {
    draft: { text: '草稿', cls: 'badge-gray' },
    scheduled: { text: '待开始', cls: 'badge-blue' },
    running: { text: '竞拍中', cls: 'badge-red' },
    sold: { text: '已成交', cls: 'badge-green' },
    failed: { text: '流拍', cls: 'badge-gray' },
    cancelled: { text: '已取消', cls: 'badge-gray' },
  }
  const info = map[s] || { text: s, cls: 'badge-gray' }
  return <span className={`status-badge ${info.cls}`}>{info.text}</span>
}
