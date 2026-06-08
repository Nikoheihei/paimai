/**
 * Admin 管理后台 — Hash 路由
 * #/login       登录/注册
 * #/            Dashboard 数据概览
 * #/rooms       直播间列表
 * #/rooms/:id   直播间详情（商品库/上架计划）
 * #/products    独立商品管理
 * #/orders      订单列表
 */

import { useEffect, useState } from 'react'
import { isLoggedIn, clearToken, getMe, type MeResult } from './api/client'
import AdminLoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import RoomListPage from './pages/RoomListPage'
import RoomDetailPage from './pages/RoomDetailPage'
import ProductListPage from './pages/ProductListPage'
import OrderListPage from './pages/OrderListPage'
import './App.css'

type Route = { page: string; roomId?: number }

function parseHash(): Route {
  const hash = window.location.hash.slice(1) || '/'
  const parts = hash.split('/').filter(Boolean)
  if (parts[0] === 'rooms' && parts[1]) return { page: 'room-detail', roomId: parseInt(parts[1]) }
  if (parts[0] === 'rooms') return { page: 'rooms' }
  if (parts[0] === 'products') return { page: 'products' }
  if (parts[0] === 'orders') return { page: 'orders' }
  if (parts[0] === 'login') return { page: 'login' }
  return { page: 'dashboard' }
}

function App() {
  const [authed, setAuthed] = useState(() => isLoggedIn())
  const [route, setRoute] = useState<Route>(parseHash)
  const [me, setMe] = useState<MeResult | null>(null)

  useEffect(() => {
    const handler = () => setRoute(parseHash())
    window.addEventListener('hashchange', handler)
    return () => window.removeEventListener('hashchange', handler)
  }, [])

  useEffect(() => {
    if (!authed) {
      setMe(null)
      return
    }
    getMe().then(setMe).catch(() => {
      clearToken()
      setAuthed(false)
      window.location.hash = '#/login'
    })
  }, [authed])

  const handleLogin = () => {
    setAuthed(true)
    window.location.hash = '#/'
  }

  const handleLogout = () => {
    clearToken()
    setAuthed(false)
    setMe(null)
    window.location.hash = '#/login'
  }

  if (!authed) return <AdminLoginPage onLogin={handleLogin} />

  const navActive = (p: string) => route.page === p ? 'active' : ''

  return (
    <div className="admin-app">
      <nav className="admin-nav">
        <a href="#/" className={navActive('dashboard')}>概览</a>
        <a href="#/rooms" className={navActive('rooms') || navActive('room-detail') ? 'active' : ''}>直播间</a>
        <a href="#/products" className={navActive('products')}>商品</a>
        <a href="#/orders" className={navActive('orders')}>订单</a>
        <span className="admin-user-chip">{me?.nickname || me?.username || '已登录'}</span>
        <button className="logout-btn" onClick={handleLogout}>退出</button>
      </nav>
      <main>
        {route.page === 'dashboard' && <DashboardPage />}
        {route.page === 'rooms' && <RoomListPage />}
        {route.page === 'room-detail' && <RoomDetailPage roomId={route.roomId!} onBack={() => window.location.hash = '#/rooms'} />}
        {route.page === 'products' && <ProductListPage />}
        {route.page === 'orders' && <OrderListPage />}
      </main>
    </div>
  )
}

export default App
