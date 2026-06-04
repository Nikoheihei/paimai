/**
 * Admin 管理后台 — Hash 路由
 * #/login       登录/注册
 * #/rooms       直播间列表
 * #/rooms/:id   直播间详情（商品/竞拍管理）
 * #/orders      订单列表
 */

import { useEffect, useState } from 'react'
import { isLoggedIn, clearToken } from './api/client'
import AdminLoginPage from './pages/LoginPage'
import RoomListPage from './pages/RoomListPage'
import RoomDetailPage from './pages/RoomDetailPage'
import OrderListPage from './pages/OrderListPage'
import './App.css'

type Route = { page: string; roomId?: number }

function parseHash(): Route {
  const hash = window.location.hash.slice(1) || '/rooms'
  const parts = hash.split('/').filter(Boolean)
  if (parts[0] === 'rooms' && parts[1]) return { page: 'room-detail', roomId: parseInt(parts[1]) }
  if (parts[0] === 'orders') return { page: 'orders' }
  if (parts[0] === 'login') return { page: 'login' }
  return { page: 'rooms' }
}

function App() {
  const [authed, setAuthed] = useState(() => isLoggedIn())
  const [route, setRoute] = useState<Route>(parseHash)

  useEffect(() => {
    const handler = () => setRoute(parseHash())
    window.addEventListener('hashchange', handler)
    return () => window.removeEventListener('hashchange', handler)
  }, [])

  const handleLogin = () => {
    setAuthed(true)
    window.location.hash = '#/rooms'
  }

  const handleLogout = () => {
    clearToken()
    setAuthed(false)
    window.location.hash = '#/login'
  }

  if (!authed) return <AdminLoginPage onLogin={handleLogin} />

  return (
    <div className="admin-app">
      <nav className="admin-nav">
        <a href="#/rooms" className={route.page === 'rooms' || route.page === 'room-detail' ? 'active' : ''}>直播间</a>
        <a href="#/orders" className={route.page === 'orders' ? 'active' : ''}>订单</a>
        <button className="logout-btn" onClick={handleLogout}>退出</button>
      </nav>
      <main>
        {route.page === 'rooms' && <RoomListPage />}
        {route.page === 'room-detail' && <RoomDetailPage roomId={route.roomId!} onBack={() => window.location.hash = '#/rooms'} />}
        {route.page === 'orders' && <OrderListPage />}
      </main>
    </div>
  )
}

export default App
