/**
 * OrderPage — 买家订单页面（H5）
 * 显示当前用户的订单列表，支持查看详情和模拟支付。
 */

import { useEffect, useState } from 'react'
import type { Order } from '../api/client'
import { payBuyerOrder, getToken } from '../api/client'

const BASE = '/api'

function formatCents(c: number) { return (c / 100).toFixed(2) }

function authHeaders(): Record<string, string> {
  const token = getToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

async function fetchOrders(): Promise<Order[]> {
  const res = await fetch(`${BASE}/orders`, { headers: authHeaders() })
  const body = await res.json()
  if (body.code !== 0) throw new Error(body.message)
  return body.data || []
}

async function fetchOrder(id: number): Promise<Order> {
  const res = await fetch(`${BASE}/orders/${id}`, { headers: authHeaders() })
  const body = await res.json()
  if (body.code !== 0) throw new Error(body.message)
  return body.data
}

const statusLabel: Record<string, string> = {
  pending_payment: '待付款',
  paid: '已付款',
  closed: '已关闭',
}

export default function OrderPage() {
  const [orders, setOrders] = useState<Order[]>([])
  const [selected, setSelected] = useState<Order | null>(null)
  const [loading, setLoading] = useState(true)
  const [msg, setMsg] = useState('')

  useEffect(() => {
    fetchOrders().then(setOrders).catch(() => {}).finally(() => setLoading(false))
  }, [])

  const handleSelect = async (id: number) => {
    try {
      const o = await fetchOrder(id)
      setSelected(o)
    } catch { setMsg('加载订单详情失败') }
  }

  const handlePay = async () => {
    if (!selected) return
    try {
      const updated = await payBuyerOrder(selected.id)
      setSelected(updated)
      setMsg('支付成功')
      fetchOrders().then(setOrders).catch(() => {})
    } catch (err: any) { setMsg(err.message || '支付失败') }
  }

  if (loading) return <div className="panel pending">加载中…</div>

  return (
    <div className="order-page">
      <div className="page-header">
        <h2>我的订单</h2>
        {selected && <button className="back-btn" onClick={() => setSelected(null)}>← 返回</button>}
      </div>

      {msg && <div className="toast" onClick={() => setMsg('')}>{msg}</div>}

      {selected ? (
        <div className="panel">
          <h3>订单 #{selected.id}</h3>
          <div className="order-detail">
            <div className="info-row"><span>竞拍</span><span>#{selected.auctionId}</span></div>
            <div className="info-row"><span>商品</span><span>#{selected.productId}</span></div>
            <div className="info-row"><span>金额</span><span className="price">¥{formatCents(selected.finalPriceCents)}</span></div>
            <div className="info-row"><span>状态</span><span>{statusLabel[selected.status] || selected.status}</span></div>
            <div className="info-row"><span>创建时间</span><span>{new Date(selected.createdAt).toLocaleString('zh-CN')}</span></div>
            {selected.paidAt && <div className="info-row"><span>支付时间</span><span>{new Date(selected.paidAt).toLocaleString('zh-CN')}</span></div>}
          </div>
          {selected.status === 'pending_payment' && (
            <button className="bid-btn" style={{width:'100%',marginTop:16}} onClick={handlePay}>模拟支付</button>
          )}
        </div>
      ) : (
        <div className="card-list">
          {orders.map(o => (
            <div key={o.id} className="panel order-card" onClick={() => handleSelect(o.id)}>
              <div className="order-card-row">
                <span>订单 #{o.id}</span>
                <span className={`order-status ${o.status}`}>{statusLabel[o.status] || o.status}</span>
              </div>
              <div className="order-card-row">
                <span>竞拍 #{o.auctionId}</span>
                <span className="price">¥{formatCents(o.finalPriceCents)}</span>
              </div>
              <div className="order-card-row meta">
                <span>{new Date(o.createdAt).toLocaleString('zh-CN')}</span>
              </div>
            </div>
          ))}
          {orders.length === 0 && <p className="empty">暂无订单</p>}
        </div>
      )}
    </div>
  )
}
