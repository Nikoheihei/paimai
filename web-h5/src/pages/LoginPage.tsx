/**
 * LoginPage — 登录/注册合一页面
 *
 * 无 token 时显示此页，用户注册或登录后保存 token 并自动跳转到直播间。
 */

import { useState } from 'react'
import { login, register, setToken } from '../api/client'

type Props = {
  onLogin: (userId: number) => void
}

export default function LoginPage({ onLogin }: Props) {
  const [mode, setMode] = useState<'login' | 'register'>('login')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    setError('')
    setLoading(true)
    try {
      const result = mode === 'login'
        ? await login(username, password)
        : await register(username, password)
      setToken(result.token)
      onLogin(result.userId)
    } catch (err: any) {
      setError(err.message || '操作失败')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="login-page">
      <div className="login-card">
        <h1 className="login-title">实时竞拍</h1>
        <p className="login-subtitle">{mode === 'login' ? '登录' : '注册'}后进入直播间</p>

        <form onSubmit={handleSubmit}>
          <div className="form-group">
            <label>用户名</label>
            <input
              type="text"
              placeholder="3-32位字母/数字/下划线"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              required
              minLength={3}
              maxLength={32}
            />
          </div>

          <div className="form-group">
            <label>密码</label>
            <input
              type="password"
              placeholder="8-64位，含字母和数字"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
              minLength={8}
              maxLength={64}
            />
          </div>

          {error && <p className="form-error">{error}</p>}

          <button className="login-btn" type="submit" disabled={loading}>
            {loading ? '处理中…' : mode === 'login' ? '登录' : '注册'}
          </button>
        </form>

        <p className="switch-mode">
          {mode === 'login' ? (
            <>没有账号？<button className="link-btn" onClick={() => { setMode('register'); setError('') }}>注册</button></>
          ) : (
            <>已有账号？<button className="link-btn" onClick={() => { setMode('login'); setError('') }}>登录</button></>
          )}
        </p>
      </div>
    </div>
  )
}
