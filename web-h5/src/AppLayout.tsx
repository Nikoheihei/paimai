import { Outlet, NavLink } from 'react-router-dom'
import ErrorBoundary from './components/ErrorBoundary'
import { useAuthStore } from './store/useAuthStore'
import './App.css'

export default function AppLayout() {
  const logout = useAuthStore(state => state.logout)
  const userInfo = useAuthStore(state => state.userInfo)

  return (
    <div className="app">
      <ErrorBoundary>
        <div className="page-scrollable">
          <nav className="h5-nav">
            <NavLink to="/" className={({ isActive }) => isActive ? 'active' : ''} end>首页</NavLink>
            <NavLink to="/agents" className={({ isActive }) => isActive ? 'active' : ''}>我的 Agent</NavLink>
            <NavLink to="/pacts" className={({ isActive }) => isActive ? 'active' : ''}>赢拍审批</NavLink>
            <NavLink to="/orders" className={({ isActive }) => isActive ? 'active' : ''}>我的订单</NavLink>
            <NavLink to="/address" className={({ isActive }) => isActive ? 'active' : ''}>地址</NavLink>
            <span className="h5-user-chip">{userInfo?.nickname || userInfo?.username || '已登录'}</span>
            <button className="logout-link" onClick={logout}>退出</button>
          </nav>
          
          <Outlet />
        </div>
      </ErrorBoundary>
    </div>
  )
}
