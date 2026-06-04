/**
 * App — H5 根组件
 *
 * 路由（Hash）：
 * #/               直播间列表（首页）
 * #/rooms/:id      直播间
 * #/orders         我的订单
 *
 * 无 token → 登录页
 */

import { useState, useEffect, useCallback } from 'react'
import LoginPage from './pages/LoginPage'
import RoomListPage from './pages/RoomListPage'
import RoomPage from './pages/RoomPage'
import OrderPage from './pages/OrderPage'
import { isLoggedIn, clearToken } from './api/client'
import './App.css'

type Route = { page: string; roomId?: number }

function parseHash(): Route {
  const hash = window.location.hash.slice(1)
  if (!hash || hash === '/') return { page: 'rooms' }
  const parts = hash.split('/').filter(Boolean)
  if (parts[0] === 'rooms' && parts[1]) return { page: 'room', roomId: parseInt(parts[1]) }
  if (parts[0] === 'orders') return { page: 'orders' }
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

  const handleLogin = useCallback((_userId: number) => {
    setAuthed(true)
    window.location.hash = '#/'
  }, [])

  const handleLogout = useCallback(() => {
    clearToken()
    setAuthed(false)
    window.location.hash = '#/login'
  }, [])

  if (!authed) {
    return (
      <div className="app">
        <LoginPage onLogin={handleLogin} />
      </div>
    )
  }

  const showNav = route.page === 'rooms' || route.page === 'orders'
  const navActive = (p: string) => route.page === p ? 'active' : ''

  return (
    <div className="app">
      {showNav && (
        <nav className="h5-nav">
          <a href="#/" className={navActive('rooms')}>首页</a>
          <a href="#/orders" className={navActive('orders')}>我的订单</a>
          <button className="logout-link" onClick={handleLogout}>退出</button>
        </nav>
      )}

      {route.page === 'rooms' && <RoomListPage />}
      {route.page === 'room' && <RoomPage roomId={route.roomId!} onBack={() => window.location.hash = '#/'} />}
      {route.page === 'orders' && <OrderPage />}
    </div>
  )
}

export default App
