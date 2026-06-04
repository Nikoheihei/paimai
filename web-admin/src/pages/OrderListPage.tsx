import { useEffect, useState } from 'react'
import { listOrders, payOrder, type Order } from '../api/client'

function formatCents(c: number) { return (c / 100).toFixed(2) }

export default function OrderListPage() {
  const [orders, setOrders] = useState<Order[]>([])
  const [msg, setMsg] = useState('')

  const load = () => { listOrders().then(setOrders).catch(() => {}) }
  useEffect(load, [])

  const handlePay = async (id: number) => {
    try { await payOrder(id); setMsg(`订单 #${id} 已支付`); load() } catch (err: any) { setMsg(err.message) }
  }

  const statusLabel = (s: string) => ({ pending_payment: '待付款', paid: '已付款', closed: '已关闭' })[s] || s

  return (
    <div className="admin-page">
      <div className="page-header">
        <h1>订单管理</h1>
      </div>
      {msg && <div className="toast" onClick={() => setMsg('')}>{msg}</div>}
      <div className="card-list">
        {orders.map(o => (
          <div key={o.id} className="card compact">
            <div className="card-body">
              <strong>#{o.id}</strong> · 竞拍 #{o.auctionId} · 买家 #{o.buyerId} · ¥{formatCents(o.finalPriceCents)} · <span className="meta">{statusLabel(o.status)}</span>
              {o.paidAt && <span className="meta"> · 支付于 {new Date(o.paidAt).toLocaleString('zh-CN')}</span>}
            </div>
            {o.status === 'pending_payment' && <button className="admin-btn small primary" onClick={() => handlePay(o.id)}>模拟支付</button>}
          </div>
        ))}
        {orders.length === 0 && <p className="empty">暂无订单</p>}
      </div>
    </div>
  )
}
