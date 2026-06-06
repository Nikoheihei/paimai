/**
 * AddressFloatPanel — 右侧地址选择浮窗
 *
 * 渲染在 LiveRoomPage 内部，利用其 transform 约束，position:fixed 相对直播间容器定位。
 * 折叠态：右侧竖排标签 "地址"
 * 展开态：地址列表面板
 * 位置在商品浮窗下方（top: 64%），避免重叠
 */

import { useState, useEffect, useCallback } from 'react'
import { listAddresses, type Address as ApiAddress } from '../api/client'

export interface AddressItem {
  id: number
  name: string
  phone: string
  province: string
  city: string
  district: string
  detail: string
  isDefault: boolean
}

function toLocal(a: ApiAddress): AddressItem {
  return {
    id: a.id,
    name: a.name,
    phone: a.phone,
    province: a.province,
    city: a.city,
    district: a.district,
    detail: a.detail,
    isDefault: a.isDefault,
  }
}

type Props = {
  selectedId: number | null
  onSelect: (addr: AddressItem) => void
}

export default function AddressFloatPanel({ selectedId, onSelect }: Props) {
  const [expanded, setExpanded] = useState(false)
  const [addresses, setAddresses] = useState<AddressItem[]>([])
  const [loading, setLoading] = useState(false)
  const [showForm, setShowForm] = useState(false)

  const [name, setName] = useState('')
  const [phone, setPhone] = useState('')
  const [province, setProvince] = useState('')
  const [city, setCity] = useState('')
  const [district, setDistrict] = useState('')
  const [detail, setDetail] = useState('')
  const [isDefault, setIsDefault] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const list = await listAddresses()
      setAddresses(list.map(toLocal))
    } catch {
      // 静默处理
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  const handleSave = async () => {
    if (!name.trim() || !phone.trim() || !detail.trim()) return
    const { createAddress } = await import('../api/client')
    await createAddress({ name, phone, province, city, district, detail, isDefault })
    await load()
    setShowForm(false)
    setName(''); setPhone(''); setProvince(''); setCity(''); setDistrict(''); setDetail(''); setIsDefault(false)
  }

  return (
    <>
      {/* 折叠标签 — 直播间右侧 */}
      <div
        onClick={() => setExpanded(!expanded)}
        style={{
          position: 'fixed',
          right: 0,
          top: '78%',
          width: 26,
          height: 56,
          background: 'rgba(18,18,30,.9)',
          backdropFilter: 'blur(12px)',
          borderRadius: '8px 0 0 8px',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          color: selectedId ? 'var(--green)' : 'var(--text-secondary)',
          fontSize: 13,
          fontWeight: 700,
          cursor: 'pointer',
          writingMode: 'vertical-lr',
          boxShadow: '-2px 0 10px rgba(0,0,0,.3)',
          border: '1px solid var(--glass-border)',
          borderRight: 'none',
          zIndex: 145,
          whiteSpace: 'nowrap',
          userSelect: 'none',
        }}
      >
        地址
      </div>

      {/* 展开面板 — 从右侧滑入 */}
      {expanded && (
        <>
          {/* 遮罩 */}
          <div
            onClick={() => setExpanded(false)}
            style={{
              position: 'fixed',
              inset: 0,
              zIndex: 146,
              background: 'rgba(0,0,0,.3)',
            }}
          />
          {/* 面板 */}
          <div
            style={{
              position: 'fixed',
              right: 0,
              top: '8%',
              bottom: '8%',
              width: 240,
              background: 'rgba(18,18,30,.95)',
              backdropFilter: 'blur(24px)',
              WebkitBackdropFilter: 'blur(24px)',
              borderRadius: 'var(--radius-lg) 0 0 var(--radius-lg)',
              boxShadow: '-4px 0 24px rgba(0,0,0,.5)',
              zIndex: 147,
              display: 'flex',
              flexDirection: 'column',
              border: '1px solid var(--glass-border)',
              borderRight: 'none',
              overflow: 'hidden',
            }}
          >
            {/* 头部 */}
            <div style={{
              padding: '14px 16px',
              borderBottom: '1px solid var(--glass-border)',
              display: 'flex',
              alignItems: 'center',
              justifyContent: 'space-between',
              flexShrink: 0,
            }}>
              <span style={{ fontSize: 15, fontWeight: 700 }}>收货地址</span>
              <button
                onClick={() => setShowForm(!showForm)}
                style={{
                  background: 'none',
                  border: '1px solid var(--glass-border)',
                  borderRadius: 6,
                  color: 'var(--text-secondary)',
                  fontSize: 12,
                  padding: '4px 10px',
                  cursor: 'pointer',
                }}
              >
                {showForm ? '取消' : '+ 新增'}
              </button>
            </div>

            {/* 新增表单 */}
            {showForm && (
              <div style={{
                padding: '12px 16px',
                borderBottom: '1px solid var(--glass-border)',
                flexShrink: 0,
                maxHeight: '50%',
                overflowY: 'auto',
              }}>
                <input style={inputStyle} value={name} onChange={e => setName(e.target.value)} placeholder="收货人姓名" />
                <input style={inputStyle} value={phone} onChange={e => setPhone(e.target.value)} placeholder="手机号" />
                <div style={{ display: 'flex', gap: 6 }}>
                  <input style={{ ...inputStyle, flex: 1 }} value={province} onChange={e => setProvince(e.target.value)} placeholder="省" />
                  <input style={{ ...inputStyle, flex: 1 }} value={city} onChange={e => setCity(e.target.value)} placeholder="市" />
                  <input style={{ ...inputStyle, flex: 1 }} value={district} onChange={e => setDistrict(e.target.value)} placeholder="区" />
                </div>
                <input style={inputStyle} value={detail} onChange={e => setDetail(e.target.value)} placeholder="详细地址（街道、门牌号）" />
                <label style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--text-secondary)', marginBottom: 8 }}>
                  <input type="checkbox" checked={isDefault} onChange={e => setIsDefault(e.target.checked)} />
                  设为默认地址
                </label>
                <button onClick={handleSave} style={{
                  width: '100%', padding: '8px 0', background: 'var(--primary-grad)',
                  border: 'none', borderRadius: 8, color: '#fff', fontSize: 13, fontWeight: 700, cursor: 'pointer',
                }}>
                  保存
                </button>
              </div>
            )}

            {/* 地址列表 */}
            <div style={{ flex: 1, overflowY: 'auto', padding: '8px 12px', scrollbarWidth: 'none' }}>
              {loading ? (
                <div style={{ textAlign: 'center', padding: 24, fontSize: 13, color: 'var(--text-muted)' }}>加载中...</div>
              ) : addresses.length === 0 ? (
                <div style={{ textAlign: 'center', padding: 24, fontSize: 13, color: 'var(--text-muted)' }}>暂无地址，点击上方新增</div>
              ) : (
                addresses.map(addr => {
                  const isSelected = addr.id === selectedId
                  return (
                    <div key={addr.id}
                      onClick={() => { onSelect(addr); setExpanded(false) }}
                      style={{
                        padding: '10px 12px', borderRadius: 10, marginBottom: 6, cursor: 'pointer',
                        background: isSelected ? 'rgba(37,199,120,.12)' : 'rgba(255,255,255,.04)',
                        border: isSelected ? '1px solid rgba(37,199,120,.3)' : '1px solid transparent',
                        transition: 'all .15s',
                      }}
                    >
                      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 4 }}>
                        <span style={{ fontSize: 14, fontWeight: 600 }}>{addr.name}</span>
                        <span style={{ fontSize: 12, color: 'var(--text-secondary)' }}>{addr.phone}</span>
                        {addr.isDefault && (
                          <span style={{ fontSize: 10, fontWeight: 700, background: 'var(--primary)', color: '#fff', padding: '1px 6px', borderRadius: 4, marginLeft: 'auto' }}>默认</span>
                        )}
                        {isSelected && !addr.isDefault && (
                          <span style={{ fontSize: 10, fontWeight: 700, background: 'rgba(37,199,120,.2)', color: 'var(--green)', padding: '1px 6px', borderRadius: 4, marginLeft: 'auto' }}>已选</span>
                        )}
                      </div>
                      <div style={{ fontSize: 12, color: 'var(--text-muted)', lineHeight: 1.4 }}>
                        {addr.province}{addr.city}{addr.district}{addr.detail}
                      </div>
                    </div>
                  )
                })
              )}
            </div>
          </div>
        </>
      )}
    </>
  )
}

const inputStyle: React.CSSProperties = {
  width: '100%',
  background: 'rgba(255,255,255,.06)',
  border: '1px solid var(--glass-border)',
  borderRadius: 8,
  padding: '8px 10px',
  color: 'var(--text-primary)',
  fontSize: 13,
  outline: 'none',
  marginBottom: 8,
}
