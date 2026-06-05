/**
 * AddressListPage — 收货地址管理（H5）
 * 新增/编辑/删除地址，设为默认。
 * 已对接后端 API，数据持久化到服务端。
 */

import { useState, useEffect } from 'react'
import Toast from '../components/Toast'
import { listAddresses, createAddress, updateAddress, deleteAddress, type Address as ApiAddress } from '../api/client'

export interface Address {
  id: string
  name: string
  phone: string
  province: string
  city: string
  district: string
  detail: string
  isDefault: boolean
}

function toLocal(a: ApiAddress): Address {
  return { id: String(a.id), name: a.name, phone: a.phone, province: a.province, city: a.city, district: a.district, detail: a.detail, isDefault: a.isDefault }
}

export default function AddressListPage() {
  const [addresses, setAddresses] = useState<Address[]>([])
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState<Address | null>(null)
  const [loading, setLoading] = useState(true)

  // 表单字段
  const [name, setName] = useState('')
  const [phone, setPhone] = useState('')
  const [province, setProvince] = useState('')
  const [city, setCity] = useState('')
  const [district, setDistrict] = useState('')
  const [detail, setDetail] = useState('')
  const [isDefault, setIsDefault] = useState(false)

  const load = async () => {
    setLoading(true)
    try {
      const list = await listAddresses()
      setAddresses(list.map(toLocal))
    } catch (e: any) {
      Toast.error(e.message || '加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const resetForm = () => {
    setName(''); setPhone(''); setProvince(''); setCity(''); setDistrict(''); setDetail(''); setIsDefault(false)
    setEditing(null)
  }

  const handleSave = async () => {
    if (!name.trim() || !phone.trim() || !detail.trim()) {
      Toast.error('请填写完整信息')
      return
    }
    const payload = { name, phone, province, city, district, detail, isDefault }
    try {
      if (editing) {
        await updateAddress(Number(editing.id), payload)
        Toast.success('地址已更新')
      } else {
        await createAddress(payload)
        Toast.success('地址已添加')
      }
      await load()
      setShowForm(false)
      resetForm()
    } catch (e: any) {
      Toast.error(e.message || '保存失败')
    }
  }

  const handleDelete = async (id: string) => {
    if (!confirm('确定删除该地址？')) return
    try {
      await deleteAddress(Number(id))
      await load()
      Toast.success('地址已删除')
    } catch (e: any) {
      Toast.error(e.message || '删除失败')
    }
  }

  const handleEdit = (addr: Address) => {
    setEditing(addr)
    setName(addr.name); setPhone(addr.phone); setProvince(addr.province)
    setCity(addr.city); setDistrict(addr.district); setDetail(addr.detail); setIsDefault(addr.isDefault)
    setShowForm(true)
  }

  const handleSetDefault = async (id: string) => {
    const addr = addresses.find(a => a.id === id)
    if (!addr) return
    try {
      await updateAddress(Number(id), { name: addr.name, phone: addr.phone, province: addr.province, city: addr.city, district: addr.district, detail: addr.detail, isDefault: true })
      await load()
    } catch (e: any) {
      Toast.error(e.message || '设置失败')
    }
  }

  return (
    <div className="address-page">
      <div className="page-header">
        <button className="admin-btn primary" onClick={() => { setShowForm(!showForm); resetForm() }}>
          {showForm ? '取消' : '+ 新建地址'}
        </button>
      </div>

      {showForm && (
        <div className="panel address-form">
          <h3>{editing ? '编辑地址' : '新建地址'}</h3>
          <div className="field"><label>收货人</label><input value={name} onChange={e => setName(e.target.value)} placeholder="姓名" /></div>
          <div className="field"><label>手机号</label><input value={phone} onChange={e => setPhone(e.target.value)} placeholder="手机号" /></div>
          <div className="field"><label>省</label><input value={province} onChange={e => setProvince(e.target.value)} placeholder="省/直辖市" /></div>
          <div className="field"><label>市</label><input value={city} onChange={e => setCity(e.target.value)} placeholder="市" /></div>
          <div className="field"><label>区</label><input value={district} onChange={e => setDistrict(e.target.value)} placeholder="区/县" /></div>
          <div className="field"><label>详细地址</label><textarea value={detail} onChange={e => setDetail(e.target.value)} placeholder="街道、门牌号等" rows={2} /></div>
          <label className="default-check">
            <input type="checkbox" checked={isDefault} onChange={e => setIsDefault(e.target.checked)} />
            设为默认地址
          </label>
          <button className="bid-btn" style={{ width: '100%', marginTop: 12 }} onClick={handleSave}>保存</button>
        </div>
      )}

      {loading ? <div className="refresh-indicator">加载中...</div> : (
        <div className="address-list">
          {addresses.map(addr => (
            <div key={addr.id} className={`panel address-card ${addr.isDefault ? 'is-default' : ''}`}>
              <div className="address-card-header">
                <span className="addr-name">{addr.name}</span>
                <span className="addr-phone">{addr.phone}</span>
                {addr.isDefault && <span className="default-tag">默认</span>}
              </div>
              <div className="addr-full">
                {addr.province}{addr.city}{addr.district}{addr.detail}
              </div>
              <div className="address-actions">
                {!addr.isDefault && <button className="text-btn" onClick={() => handleSetDefault(addr.id)}>设为默认</button>}
                <button className="text-btn" onClick={() => handleEdit(addr)}>编辑</button>
                <button className="text-btn danger" onClick={() => handleDelete(addr.id)}>删除</button>
              </div>
            </div>
          ))}
          {addresses.length === 0 && (
            <div className="empty-state-box">
              <div className="empty-icon">📍</div>
              <p>暂无收货地址</p>
              <p className="sub">点击右上角添加</p>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
