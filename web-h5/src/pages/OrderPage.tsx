/**
 * OrderPage — 买家订单页面（增强版）
 * Tab 切换 + 真实地址选择 + 支付流程
 */

import { useEffect, useState } from 'react'
import type { Order, Address } from '../api/client'
import { payBuyerOrder, getBuyerOrder, listAddresses, listBuyerOrders } from '../api/client'
import { formatPaymentCountdown, paymentDeadlineMs, remainingPaymentSeconds } from '../utils/paymentDeadline'

function formatCents(c: number) { return (c / 100).toFixed(2) }
function productName(order: Order) { return order.productName || `商品 #${order.productId}` }
function sellerName(order: Order) { return order.sellerNickname || `商家 #${order.sellerId}` }
function paymentRemaining(order: Order, nowMs: number): number {
  if (order.status !== 'pending_payment') return 0
  return remainingPaymentSeconds(paymentDeadlineMs(order.createdAt), nowMs)
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
  const [nowMs, setNowMs] = useState(Date.now())

  const load = () => {
    listBuyerOrders().then(setOrders).catch(err => setMsg(err.message || '加载订单失败')).finally(() => setLoading(false))
  }
  useEffect(load, [])

  useEffect(() => {
    const timer = setInterval(() => setNowMs(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])

  // 监听订单刷新事件（竞拍成交后自动刷新）
  useEffect(() => {
    const handler = () => { load() }
    window.addEventListener('order:refresh', handler)
    return () => window.removeEventListener('order:refresh', handler)
  }, [])

  const filtered = activeTab === 'all' ? orders : orders.filter(o => o.status === activeTab)

  const handleSelect = async (id: number) => {
    try {
      const o = await getBuyerOrder(id)
      setSelected(o)
    } catch { setMsg('加载订单详情失败') }
  }

  const handlePay = async () => {
    if (!selected) return
    if (paymentRemaining(selected, Date.now()) === 0) {
      setMsg('支付已超时')
      return
    }
    try {
      const list = await listAddresses()
      setAddresses(list)
      setSelectedAddrId(null)
      setShowAddress(true)
    } catch {
      setMsg('加载地址失败，请先到地址管理添加收货地址')
    }
  }

  const confirmPay = async () => {
    if (!selected) return
    if (paymentRemaining(selected, Date.now()) === 0) {
      setMsg('支付已超时')
      return
    }
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

  const selectedRemaining = selected ? paymentRemaining(selected, nowMs) : 0

  return (
    <div className="order-page">
      <div className="page-header">
        {selected && <button className="back-btn" onClick={() => { setSelected(null); setShowAddress(false) }}>← 返回</button>}
      </div>

      {msg && <div className="toast" onClick={() => setMsg('')}>{msg}</div>}

      {selected ? (
        <div className="panel">
          <h3>订单 #{selected.id}</h3>
          <div className="order-product-summary">
            {selected.productImage ? (
              <img className="order-product-thumb" src={selected.productImage} alt={productName(selected)} />
            ) : (
              <div className="order-product-thumb placeholder">[商品]</div>
            )}
            <div className="order-product-copy">
              <div className="order-product-title">{productName(selected)}</div>
              <div className="order-product-seller">下单商家：{sellerName(selected)}</div>
              <div className="order-product-final">成交价 ¥{formatCents(selected.finalPriceCents)}</div>
            </div>
          </div>
          <div className="order-detail">
            <div className="info-row"><span>竞拍</span><span>#{selected.auctionId}</span></div>
            <div className="info-row"><span>商品</span><span>{productName(selected)}</span></div>
            <div className="info-row"><span>下单商家</span><span>{sellerName(selected)}</span></div>
            <div className="info-row"><span>金额</span><span className="price">¥{formatCents(selected.finalPriceCents)}</span></div>
            <div className="info-row"><span>状态</span><span className={statusClass[selected.status]}>{statusLabel[selected.status]}</span></div>
            <div className="info-row"><span>创建时间</span><span>{new Date(selected.createdAt).toLocaleString('zh-CN')}</span></div>
            {selected.status === 'pending_payment' && (
              <div className="info-row">
                <span>支付倒计时</span>
                <span className={selectedRemaining === 0 ? 'status-closed' : 'status-pending'}>
                  {selectedRemaining === 0 ? '支付超时' : formatPaymentCountdown(selectedRemaining)}
                </span>
              </div>
            )}
            {selected.paidAt && <div className="info-row"><span>支付时间</span><span>{new Date(selected.paidAt).toLocaleString('zh-CN')}</span></div>}
            <div className="info-row">
              <span>收货地址</span>
              <span style={{ textAlign: 'right', maxWidth: '60%' }}>
                {selected.addressSnapshot || (selected.status === 'pending_payment' ? '待支付时选择' : '未记录')}
              </span>
            </div>
          </div>

          {selected.status === 'pending_payment' && !showAddress && (
            <button className="bid-btn" style={{ width: '100%', marginTop: 16 }} disabled={selectedRemaining === 0} onClick={handlePay}>
              {selectedRemaining === 0 ? '支付超时' : '去支付'}
            </button>
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
                <button className="bid-btn" style={{ flex: 1 }} disabled={payLoading || addresses.length === 0 || !selectedAddrId || selectedRemaining === 0} onClick={confirmPay}>
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
                <div className="order-card-main">
                  {o.productImage ? (
                    <img className="order-card-thumb" src={o.productImage} alt={productName(o)} />
                  ) : (
                    <div className="order-card-thumb placeholder">[商品]</div>
                  )}
                  <div className="order-card-info">
                    <div className="order-card-title-row">
                      <span className="order-card-title">{productName(o)}</span>
                      <span className={`order-status ${statusClass[o.status]}`}>{statusLabel[o.status]}</span>
                    </div>
                    <div className="order-card-seller">下单商家：{sellerName(o)}</div>
                    <div className="order-card-price-row">
                      <span>成交价</span>
                      <span className="price">¥{formatCents(o.finalPriceCents)}</span>
                    </div>
                    {o.status === 'pending_payment' && (
                      <div className="order-card-price-row">
                        <span>支付倒计时</span>
                        <span className={paymentRemaining(o, nowMs) === 0 ? 'status-closed' : 'status-pending'}>
                          {paymentRemaining(o, nowMs) === 0 ? '支付超时' : formatPaymentCountdown(paymentRemaining(o, nowMs))}
                        </span>
                      </div>
                    )}
                  </div>
                </div>
                <div className="order-card-row meta">
                  <span>订单 #{o.id} · 竞拍 #{o.auctionId}</span>
                  <span>{new Date(o.createdAt).toLocaleString('zh-CN')}</span>
                </div>
              </div>
            ))}
            {filtered.length === 0 && (
              <div className="empty-state-box">
                <div className="empty-icon">[订单]</div>
                <p>{activeTab === 'all' ? '暂无订单' : `暂无${statusLabel[activeTab]}订单`}</p>
              </div>
            )}
          </div>
        </>
      )}
    </div>
  )
}
