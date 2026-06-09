/**
 * PactListPage — 赢拍后的人工审批（Pact）
 *
 * 入口：Agent 列表 / 详情页「赢拍审批」。
 * 流程：Agent 赢拍 → 生成 Pact（created）→ 用户选地址批准 → 跳订单页用现有支付。
 * 拒绝则不支付，订单走现有超时关闭路径。
 */

import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { AgentPact, Address } from '../api/client'
import { listPacts, approvePact, rejectPact, listAddresses } from '../api/client'
import { formatPaymentCountdown, remainingPaymentSeconds } from '../utils/paymentDeadline'

function formatYuan(cents: number) {
  return (cents / 100).toFixed(2)
}

const statusLabel: Record<string, string> = {
  created: '待审批', approved: '已批准', rejected: '已拒绝', expired: '已过期',
}
const statusColor: Record<string, string> = {
  created: '#ffa726', approved: 'var(--green)', rejected: '#ff4757', expired: 'var(--text-muted)',
}

type ProductSnapshot = { name?: string; imageUrl?: string; description?: string }

function parseProduct(json: string): ProductSnapshot {
  try { return JSON.parse(json || '{}') as ProductSnapshot } catch { return {} }
}

function remaining(pact: AgentPact, nowMs: number): number {
  if (pact.status !== 'created') return 0
  return remainingPaymentSeconds(new Date(pact.paymentDeadlineAt).getTime(), nowMs)
}

export default function PactListPage() {
  const navigate = useNavigate()
  const [pacts, setPacts] = useState<AgentPact[]>([])
  const [selected, setSelected] = useState<AgentPact | null>(null)
  const [addresses, setAddresses] = useState<Address[]>([])
  const [selectedAddrId, setSelectedAddrId] = useState<number | null>(null)
  const [showAddress, setShowAddress] = useState(false)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [msg, setMsg] = useState('')
  const [nowMs, setNowMs] = useState(Date.now())

  const load = useCallback(() => {
    listPacts()
      .then(setPacts)
      .catch(err => setMsg(err.message || '加载失败'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    load()
    const timer = window.setInterval(load, 3000)
    return () => window.clearInterval(timer)
  }, [load])

  useEffect(() => {
    const timer = window.setInterval(() => setNowMs(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [])

  // 选中态跟随列表刷新（状态可能被支付/超时改变）
  useEffect(() => {
    if (selected) {
      const fresh = pacts.find(p => p.id === selected.id)
      if (fresh && fresh.status !== selected.status) setSelected(fresh)
    }
  }, [pacts, selected])

  const openApprove = async (pact: AgentPact) => {
    setSelected(pact)
    try {
      const list = await listAddresses()
      setAddresses(list)
      setSelectedAddrId(list.find(a => a.isDefault)?.id ?? null)
      setShowAddress(true)
    } catch {
      setMsg('加载地址失败，请先到「地址」页添加收货地址')
    }
  }

  const confirmApprove = async () => {
    if (!selected) return
    if (addresses.length === 0) { setMsg('请先添加收货地址'); return }
    if (!selectedAddrId) { setMsg('请选择收货地址'); return }
    const addr = addresses.find(a => a.id === selectedAddrId)
    if (!addr) { setMsg('地址信息异常'); return }
    const snapshot = `${addr.name} ${addr.phone} ${addr.province}${addr.city}${addr.district}${addr.detail}`
    setBusy(true)
    try {
      await approvePact(selected.id, addr.id, snapshot)
      setMsg('已批准，正在跳转订单页完成支付…')
      setShowAddress(false)
      load()
      setTimeout(() => navigate('/orders'), 800)
    } catch (err: any) {
      setMsg(err.message || '批准失败')
    } finally {
      setBusy(false)
    }
  }

  const handleReject = async (pact: AgentPact) => {
    setBusy(true)
    try {
      await rejectPact(pact.id)
      setMsg('已拒绝，订单将由系统超时关闭')
      setShowAddress(false)
      setSelected(null)
      load()
    } catch (err: any) {
      setMsg(err.message || '拒绝失败')
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <div className="empty-state-box">加载中…</div>

  return (
    <div className="room-list-page agent-page-shell">
      <div className="agent-hero-panel pact-hero-panel">
        <div>
          <div className="agent-hero-kicker">Human Approval Required</div>
          <h2>赢拍审批</h2>
          <p>Pact 只做人工确认、地址选择和支付前置放行；不替你付款，也不移动资金。</p>
        </div>
        <button className="agent-btn-secondary agent-hero-action" onClick={() => navigate('/agents')}>← 我的 Agent</button>
      </div>

      {msg && <div className="toast-wrap"><div className="toast-item" onClick={() => setMsg('')}>{msg}</div></div>}

      {pacts.length === 0 ? (
        <div className="empty-state-box">
          <div className="empty-icon">📜</div>
          <p className="empty-title">暂无待审批的赢拍</p>
          <p className="sub">当你的 Agent 赢得竞拍后，会在这里生成需要你确认的 Pact</p>
        </div>
      ) : (
        <div className="card-list">
          {pacts.map(pact => {
            const product = parseProduct(pact.productSnapshotJson)
            const rem = remaining(pact, nowMs)
            const editing = selected?.id === pact.id && showAddress
            return (
              <div key={pact.id} className="agent-card pact-card">
                <div className="agent-card-title-row">
                  <span className="agent-card-title">{product.name || `Pact #${pact.id}`}</span>
                  <span className="agent-status-pill" style={{ color: statusColor[pact.status] || 'var(--text-muted)' }}>
                    {statusLabel[pact.status] || pact.status}
                  </span>
                </div>
                <div className="pact-price-strip">
                  <div className="agent-info-row"><span className="agent-info-label">成交价</span><span className="agent-info-price">¥{formatYuan(pact.finalPriceCents)}</span></div>
                  <div className="agent-info-row"><span className="agent-info-label">预算上限</span><span className="agent-info-value">¥{formatYuan(pact.maxBudgetCents)}</span></div>
                  <div className="agent-info-row"><span className="agent-info-label">订单号</span><span className="agent-info-value">#{pact.orderId}</span></div>
                  {pact.status === 'created' && (
                    <div className="agent-info-row">
                      <span className="agent-info-label">审批截止</span>
                      <span className="agent-info-value" style={{ color: rem === 0 ? 'var(--text-muted)' : '#ffa726' }}>
                        {rem === 0 ? '已超时' : formatPaymentCountdown(rem)}
                      </span>
                    </div>
                  )}
                  {pact.status === 'approved' && pact.addressSnapshot && (
                    <div className="agent-info-row"><span className="agent-info-label">收货地址</span><span style={{ textAlign: 'right', maxWidth: '60%', color: 'var(--text-primary)' }}>{pact.addressSnapshot}</span></div>
                  )}
                </div>

                {pact.status === 'created' && !editing && (
                  <div className="agent-action-row">
                    <button className="agent-btn-primary" style={{ flex: 1 }} disabled={busy || rem === 0} onClick={() => openApprove(pact)}>
                      {rem === 0 ? '已超时' : '批准并选地址'}
                    </button>
                    <button className="agent-btn-secondary" style={{ flex: 1 }} disabled={busy} onClick={() => handleReject(pact)}>拒绝</button>
                  </div>
                )}

                {pact.status === 'approved' && (
                  <button className="agent-btn-primary" style={{ width: '100%', marginTop: 16 }} onClick={() => navigate('/orders')}>
                    去支付（订单 #{pact.orderId}）
                  </button>
                )}

                {/* 地址选择 */}
                {editing && (
                  <div className="pact-approval-panel">
                    <div className="agent-section-title">选择收货地址</div>
                    {addresses.length === 0 ? (
                      <div className="empty-state-box" style={{ padding: 16 }}>
                        <p>暂无收货地址</p>
                        <button className="agent-btn-secondary" onClick={() => navigate('/address')}>去添加地址</button>
                      </div>
                    ) : (
                      <div style={{ display: 'flex', flexDirection: 'column', marginBottom: 16 }}>
                        {addresses.map(addr => (
                          <div
                            key={addr.id}
                            onClick={() => setSelectedAddrId(addr.id)}
                            className={`pact-address-item ${selectedAddrId === addr.id ? 'selected' : ''}`}
                          >
                            <div className="pact-address-radio">{selectedAddrId === addr.id ? '◉' : '○'}</div>
                            <div className="pact-address-info">
                              <div className="pact-address-name">
                                {addr.name} {addr.phone} {addr.isDefault && <span style={{ color: 'var(--primary)', fontSize: 11, background: 'rgba(254,44,85,.1)', padding: '2px 6px', borderRadius: 4, marginLeft: 4 }}>[默认]</span>}
                              </div>
                              <div className="pact-address-detail">
                                {addr.province}{addr.city}{addr.district}{addr.detail}
                              </div>
                            </div>
                          </div>
                        ))}
                      </div>
                    )}
                    <div className="agent-action-row">
                      <button className="agent-btn-secondary" style={{ flex: 1 }} onClick={() => { setShowAddress(false); setSelected(null) }}>取消</button>
                      <button className="agent-btn-primary" style={{ flex: 1 }} disabled={busy || !selectedAddrId} onClick={confirmApprove}>
                        {busy ? '提交中…' : '确认批准'}
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
