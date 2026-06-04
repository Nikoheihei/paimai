import { useState } from 'react'
import { login, register, setToken } from '../api/client'

type Props = { onLogin: () => void }

export default function AdminLoginPage({ onLogin }: Props) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [nickname, setNickname] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError(''); setLoading(true)
    try {
      const result = mode === 'login' ? await login(username, password) : await register(username, password, nickname || undefined)
      setToken(result.token)
      onLogin()
    } catch (err: any) { setError(err.message || '操作失败') }
    finally { setLoading(false) }
  }

  return (
    <div className="admin-login-page">
      <div className="admin-login-card">
        <h1>商家管理后台</h1>
        <p className="subtitle">{mode === 'login' ? '登录' : '注册'}后管理直播间和竞拍</p>
        <form onSubmit={handleSubmit}>
          <div className="field"><label>用户名</label><input type="text" value={username} onChange={e => setUsername(e.target.value)} required minLength={3} /></div>
          <div className="field"><label>密码</label><input type="password" value={password} onChange={e => setPassword(e.target.value)} required minLength={8} /></div>
          {mode === 'register' && <div className="field"><label>昵称（可选）</label><input type="text" value={nickname} onChange={e => setNickname(e.target.value)} /></div>}
          {error && <p className="form-error">{error}</p>}
          <button className="admin-btn primary" type="submit" disabled={loading}>{loading ? '处理中…' : mode === 'login' ? '登录' : '注册'}</button>
        </form>
        <p className="switch">
          {mode === 'login' ? <>没有账号？<button className="link" onClick={() => setMode('register')}>注册</button></> : <>已有账号？<button className="link" onClick={() => setMode('login')}>登录</button></>}
        </p>
      </div>
    </div>
  )
}
