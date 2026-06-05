/**
 * OrderPage — 买家订单页面（增强版）
 * Tab 切换 + 真实地址选择 + 支付流程
 */

import { useEffect, useState } from 'react'
import type { Order, Address } from '../api/client'
import { payBuyerOrder, getBuyerOrder, listAddresses, getToken } from '../api/client'

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

const statusLabel: Record<string, string> = {
  pending_payment: '待付款',
  paid: '已付款',
  closed: '已关闭',
}

const statusClass: Record<string, string> = {
  pending_payment: 'status-pending',
  paid: 'status-paid',
  closed: 'status-closed',
}

type TabKey = 'all' | 'pending_payment' | 'paid'

export default function OrderPage() {
  const [orders, setOrders] = useState<Order[]>([])
  const [activeTab, setActiveTab] = useState<TabKey>('all')
  const [selected, setSelected] = useState<Order | null>(null)
  const [loading, setLoading] = useState(true)
  const [msg, setMsg] = useState('')
  const [showAddress, setShowAddress] = useState(false)
  const [addresses, setAddresses] = useState<Address[]>([])
  const [selectedAddrId, setSelectedAddrId] = useState<number | null>(null)
  const [payLoading, setPayLoading] = useState(false)

  const load = () => {
    fetchOrders().then(setOrders).catch(() => {}).finally(() => setLoading(false))
  }
  useEffect(load, [])

  const filtered = activeTab === 'all' ? orders : orders.filter(o => o.status === activeTab)

  const handleSelect = async (id: number) => {
    try {
      const o = await getBuyerOrder(id)
      setSelected(o)
    } catch { setMsg('加载订单详情失败') }
  }

  const handlePay = async () => {
    if (!selected) return
    try {
      const list = await listAddresses()
      setAddresses(list)
      if (list.length > 0) {
        const def = list.find(a => a.isDefault)
        setSelectedAddrId(def ? def.id : list[0].id)
      }
      setShowAddress(true)
    } catch {
      setMsg('加载地址失败，请先到地址管理添加收货地址')
    }
  }

  const confirmPay = async () => {
    if (!selected) return
    if (addresses.length === 0) {
      setMsg('请先添加收货地址')
      return
    }
    if (!selectedAddrId) {
      setMsg('请选择收货地址')
      return
    }
    const addr = addresses.find(a => a.id === selectedAddrId)
    if (!addr) {
      setMsg('地址信息异常')
      return
    }
    const snapshot = `${addr.name} ${addr.phone} ${addr.province}${addr.city}${addr.district}${addr.detail}`
    setPayLoading(true)
    try {
      const updated = await payBuyerOrder(selected.id, addr.id, snapshot)
      setSelected(updated)
      setMsg('支付成功')
      setShowAddress(false)
      load()
    } catch (err: any) {
      setMsg(err.message || '支付失败')
    } finally {
      setPayLoading(false)
    }
  }

  if (loading) return <div className="panel pending">加载中…</div>

  return (
    <div className="order-page">
      <div className="page-header">
        <h2>我的订单</h2>
        {selected && <button className="back-btn" onClick={() => { setSelected(null); setShowAddress(false) }}>← 返回</button>}
      </div>

      {msg && <div className="toast" onClick={() => setMsg('')}>{msg}</div>}

      {selected ? (
        <div className="panel">
          <h3>订单 #{selected.id}</h3>
          <div className="order-detail">
            <div className="info-row"><span>竞拍</span><span>#{selected.auctionId}</span></div>
            <div className="info-row"><span>商品</span><span>#{selected.productId}</span></div>
            <div className="info-row"><span>金额</span><span className="price">¥{formatCents(selected.finalPriceCents)}</span></div>
            <div className="info-row"><span>状态</span><span className={statusClass[selected.status]}>{statusLabel[selected.status]}</span></div>
            <div className="info-row"><span>创建时间</span><span>{new Date(selected.createdAt).toLocaleString('zh-CN')}</span></div>
            {selected.paidAt && <div className="info-row"><span>支付时间</span><span>{new Date(selected.paidAt).toLocaleString('zh-CN')}</span></div>}
            {selected.addressSnapshot && (
              <div className="info-row"><span>收货地址</span><span style={{ textAlign: 'right', maxWidth: '60%' }}>{selected.addressSnapshot}</span></div>
            )}
          </div>

          {selected.status === 'pending_payment' && !showAddress && (
            <button className="bid-btn" style={{ width: '100%', marginTop: 16 }} onClick={handlePay}>去支付</button>
          )}

          {/* 地址选择 */}
          {showAddress && (
            <div className="address-selector">
              <h4>选择收货地址</h4>
              {addresses.length === 0 ? (
                <div className="empty-state-box" style={{ padding: 16 }}>
                  <p>暂无收货地址，请先到「我的-地址管理」添加</p>
                </div>
              ) : (
                <div className="address-list" style={{ display: 'flex', flexDirection: 'column', gap: 8, marginBottom: 12 }}>
                  {addresses.map(addr => (
                    <div
                      key={addr.id}
                      className={`address-option ${selectedAddrId === addr.id ? 'selected' : ''}`}
                      onClick={() => setSelectedAddrId(addr.id)}
                      style={{
                        display: 'flex',
                        alignItems: 'flex-start',
                        gap: 8,
                        padding: 12,
                        borderRadius: 8,
                        border: selectedAddrId === addr.id ? '2px solid #ff6b35' : '1px solid #eee',
                        background: selectedAddrId === addr.id ? '#fff5f0' : '#fff',
                        cursor: 'pointer',
                      }}
                    >
                      <div className="addr-radio" style={{ marginTop: 2 }}>{selectedAddrId === addr.id ? '◉' : '○'}</div>
                      <div className="addr-info" style={{ flex: 1 }}>
                        <div className="addr-name" style={{ fontWeight: 600, fontSize: 14 }}>
                          {addr.name} {addr.phone} {addr.isDefault && <span style={{ color: '#ff6b35', fontSize: 12 }}>[默认]</span>}
                        </div>
                        <div className="addr-detail" style={{ fontSize: 13, color: '#666', marginTop: 4 }}>
                          {addr.province}{addr.city}{addr.district}{addr.detail}
                        </div>
                      </div>
                    </div>
                  ))}
                </div>
              )}
              <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                <button className="admin-btn" style={{ flex: 1 }} onClick={() => setShowAddress(false)}>取消</button>
                <button className="bid-btn" style={{ flex: 1 }} disabled={payLoading || addresses.length === 0} onClick={confirmPay}>
                  {payLoading ? '支付中…' : '确认支付'}
                </button>
              </div>
            </div>
          )}
        </div>
      ) : (
        <>
          {/* Tab 切换 */}
          <div className="tab-bar">
            <button className={`tab-btn ${activeTab === 'all' ? 'active' : ''}`} onClick={() => setActiveTab('all')}>
              全部 ({orders.length})
            </button>
            <button className={`tab-btn ${activeTab === 'pending_payment' ? 'active' : ''}`} onClick={() => setActiveTab('pending_payment')}>
              待付款 ({orders.filter(o => o.status === 'pending_payment').length})
            </button>
            <button className={`tab-btn ${activeTab === 'paid' ? 'active' : ''}`} onClick={() => setActiveTab('paid')}>
              已付款 ({orders.filter(o => o.status === 'paid').length})
            </button>
          </div>

          <div className="card-list">
            {filtered.map(o => (
              <div key={o.id} className="panel order-card" onClick={() => handleSelect(o.id)}>
                <div className="order-card-row">
                  <span>订单 #{o.id}</span>
                  <span className={`order-status ${statusClass[o.status]}`}>{statusLabel[o.status]}</span>
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
            {filtered.length === 0 && (
              <div className="empty-state-box">
                <div className="empty-icon">📋</div>
                <p>{activeTab === 'all' ? '暂无订单' : `暂无${statusLabel[activeTab]}订单`}</p>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}
