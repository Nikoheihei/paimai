/**
 * AgentListPage — 买家 Agent 列表与创建
 *
 * 入口：顶部导航「我的 Agent」。
 * 功能：创建 Agent（3维度策略配置）、激活/暂停、查看决策回放、跳转 Pact 审批。
 */

import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import type { AgentProfile, AgentStrategy } from '../api/client'
import { listBuyerAgents, createBuyerAgent, activateAgent, pauseAgent } from '../api/client'

function formatYuan(cents: number) {
  return (cents / 100).toFixed(2)
}

const statusLabel: Record<string, string> = {
  draft: '草稿',
  active: '运行中',
  paused: '已暂停',
  stopped_after_win: '赢拍已停',
  expired: '已过期',
}

const statusColor: Record<string, string> = {
  draft: 'var(--text-muted)',
  active: 'var(--green)',
  paused: '#ffa726',
  stopped_after_win: '#4eaaf0',
  expired: 'var(--text-muted)',
}

/** 策略显示标签：从3维度字段推导 */
function strategyDisplayLabel(s: AgentStrategy): string {
  const trigger = s.trigger || 'lead'
  const pace = s.pace || 'min_step'
  const stopRatio = s.stopRatio || 0

  const triggerLabel = trigger === 'follow' ? '跟价' : '主动'
  const paceLabel = pace === 'reserve' ? '保留价优先' : '最小步长'
  const stopLabel = stopRatio > 0 && stopRatio < 1
    ? `${Math.round(stopRatio * 100)}%预算停止`
    : '预算上限停止'

  return `${triggerLabel} · ${paceLabel} · ${stopLabel}`
}

/** 旧策略名兼容标签 */
const legacyStrategyLabel: Record<string, string> = {
  conservative: '保守',
  follow_up: '跟价',
  reserve_then_follow: '保留价优先',
  cap_only: '允许超预算',
  custom: '自定义',
}

function parseStrategy(json: string): AgentStrategy {
  try {
    return JSON.parse(json || '{}') as AgentStrategy
  } catch {
    return {}
  }
}

export default function AgentListPage() {
  const navigate = useNavigate()
  const [agents, setAgents] = useState<AgentProfile[]>([])
  const [loading, setLoading] = useState(true)
  const [msg, setMsg] = useState('')

  // --- 创建表单状态 ---
  const [prompt, setPrompt] = useState('')
  const [budgetYuan, setBudgetYuan] = useState('')
  const [keywords, setKeywords] = useState('')
  const [maxBidTimes, setMaxBidTimes] = useState('100')
  const [minIntervalMs, setMinIntervalMs] = useState('3000')
  const [roomId, setRoomId] = useState('')
  const [auctionId, setAuctionId] = useState('')

  // --- 3维度策略配置 ---
  const [trigger, setTrigger] = useState<'lead' | 'follow'>('lead')
  const [pace, setPace] = useState<'min_step' | 'reserve'>('min_step')
  const [stopRatio, setStopRatio] = useState('')    // 空字符串=0(仅预算硬约束)
  const [creating, setCreating] = useState(false)

  const load = useCallback(() => {
    setLoading(true)
    listBuyerAgents()
      .then(setAgents)
      .catch(err => setMsg(err.message || '加载失败'))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => { load() }, [load])

  const handleCreate = async () => {
    if (!prompt.trim()) {
      setMsg('请先输入你的拍卖意图，例如：帮我拍一件翡翠，最高 200 元')
      return
    }
    setCreating(true)
    try {
      const input: any = {
        prompt: prompt.trim(),
        trigger,
        pace,
        requireHumanPay: true,
      }
      const budget = Number(budgetYuan)
      if (budgetYuan.trim() && !Number.isNaN(budget) && budget > 0) {
        input.maxBudgetCents = Math.round(budget * 100)
      }
      const kw = keywords.split(/[,，\s]+/).map(s => s.trim()).filter(Boolean)
      if (kw.length > 0) input.productKeywords = kw
      const maxTimes = Number(maxBidTimes)
      if (!Number.isNaN(maxTimes) && maxTimes > 0) input.maxBidTimes = Math.floor(maxTimes)
      const interval = Number(minIntervalMs)
      if (!Number.isNaN(interval) && interval >= 0) input.minIntervalMs = Math.floor(interval)
      const room = Number(roomId)
      if (roomId.trim() && !Number.isNaN(room) && room > 0) input.roomId = Math.floor(room)
      const auction = Number(auctionId)
      if (auctionId.trim() && !Number.isNaN(auction) && auction > 0) input.auctionId = Math.floor(auction)
      // 停止比例
      const ratio = Number(stopRatio)
      if (stopRatio.trim() && !Number.isNaN(ratio) && ratio > 0 && ratio <= 1) {
        input.stopRatio = ratio
      }

      const agent = await createBuyerAgent(input)
      setMsg(`已创建 Agent #${agent.id}，预算 ¥${formatYuan(agent.maxBudgetCents)}。激活后将自动出价。`)
      // 重置表单
      setPrompt('')
      setBudgetYuan('')
      setKeywords('')
      setMaxBidTimes('100')
      setMinIntervalMs('3000')
      setRoomId('')
      setAuctionId('')
      setTrigger('lead')
      setPace('min_step')
      setStopRatio('')
      load()
    } catch (err: any) {
      setMsg(err.message || '创建失败')
    } finally {
      setCreating(false)
    }
  }

  const handleActivate = async (id: number) => {
    try { await activateAgent(id); load() } catch (err: any) { setMsg(err.message || '激活失败') }
  }
  const handlePause = async (id: number) => {
    try { await pauseAgent(id); load() } catch (err: any) { setMsg(err.message || '暂停失败') }
  }

  return (
    <div className="room-list-page agent-page-shell">
      <div className="agent-hero-panel">
        <div>
          <div className="agent-hero-kicker">A2A Live Auction</div>
          <h2>我的 Agent</h2>
          <p>设定预算和出价策略，Agent 替你竞拍；赢拍后仍由你审批 Pact。</p>
        </div>
        <button className="agent-btn-secondary agent-hero-action" onClick={() => navigate('/pacts')}>赢拍审批 →</button>
      </div>

      {msg && <div className="toast-wrap"><div className="toast-item" onClick={() => setMsg('')}>{msg}</div></div>}

      {/* 创建表单 */}
      <div className="agent-card agent-form-panel">
        <div className="agent-section-title">新建 Agent</div>
        <textarea
          className="agent-input"
          placeholder="例如：帮我拍一件翡翠，最高 200 元，超过预算不要拍"
          value={prompt}
          onChange={e => setPrompt(e.target.value)}
          rows={3}
          style={{ resize: 'vertical' }}
        />

        {/* 3维度策略配置 */}
        <div className="agent-dimension-group">
          <div className="agent-dimension-label">出价触发</div>
          <div className="agent-dimension-options">
            <label className={`agent-dimension-option ${trigger === 'lead' ? 'active' : ''}`}>
              <input type="radio" name="trigger" value="lead" checked={trigger === 'lead'} onChange={() => setTrigger('lead')} />
              <span className="agent-dimension-icon">🎯</span>
              <span className="agent-dimension-text">
                <strong>主动出价</strong>
                <small>有人出价也出，没人出价也出</small>
              </span>
            </label>
            <label className={`agent-dimension-option ${trigger === 'follow' ? 'active' : ''}`}>
              <input type="radio" name="trigger" value="follow" checked={trigger === 'follow'} onChange={() => setTrigger('follow')} />
              <span className="agent-dimension-icon">👋</span>
              <span className="agent-dimension-text">
                <strong>跟价模式</strong>
                <small>必须有人先出价才跟</small>
              </span>
            </label>
          </div>
        </div>

        <div className="agent-dimension-group">
          <div className="agent-dimension-label">出价节奏</div>
          <div className="agent-dimension-options">
            <label className={`agent-dimension-option ${pace === 'min_step' ? 'active' : ''}`}>
              <input type="radio" name="pace" value="min_step" checked={pace === 'min_step'} onChange={() => setPace('min_step')} />
              <span className="agent-dimension-icon">📏</span>
              <span className="agent-dimension-text">
                <strong>最小步长</strong>
                <small>每次加一个加价幅度</small>
              </span>
            </label>
            <label className={`agent-dimension-option ${pace === 'reserve' ? 'active' : ''}`}>
              <input type="radio" name="pace" value="reserve" checked={pace === 'reserve'} onChange={() => setPace('reserve')} />
              <span className="agent-dimension-icon">🔒</span>
              <span className="agent-dimension-text">
                <strong>保留价优先</strong>
                <small>先一次出到保留价，再按步长跟</small>
              </span>
            </label>
          </div>
        </div>

        <div className="agent-dimension-group">
          <div className="agent-dimension-label">停止条件</div>
          <div className="agent-dimension-row">
            <input
              className="agent-input"
              placeholder="预算停止比例，如 0.6=60%时停止，留空=仅预算上限"
              value={stopRatio}
              onChange={e => setStopRatio(e.target.value)}
              inputMode="decimal"
              style={{ flex: 1 }}
            />
            {stopRatio && !Number.isNaN(Number(stopRatio)) && Number(stopRatio) > 0 && Number(stopRatio) <= 1 && (
              <span className="agent-dimension-hint">预算{Math.round(Number(stopRatio)*100)}%时停止出价</span>
            )}
          </div>
        </div>

        <div className="agent-field-grid">
          <input
            className="agent-input"
            placeholder="预算上限(元)"
            value={budgetYuan}
            onChange={e => setBudgetYuan(e.target.value)}
            inputMode="decimal"
          />
          <input
            className="agent-input"
            placeholder="最多出价次数"
            value={maxBidTimes}
            onChange={e => setMaxBidTimes(e.target.value)}
            inputMode="numeric"
          />
          <input
            className="agent-input"
            placeholder="最小间隔(ms)"
            value={minIntervalMs}
            onChange={e => setMinIntervalMs(e.target.value)}
            inputMode="numeric"
          />
          <input
            className="agent-input"
            placeholder="房间ID(可选)"
            value={roomId}
            onChange={e => setRoomId(e.target.value)}
            inputMode="numeric"
          />
          <input
            className="agent-input"
            placeholder="拍品ID(可选)"
            value={auctionId}
            onChange={e => setAuctionId(e.target.value)}
            inputMode="numeric"
          />
          <input
            className="agent-input"
            placeholder="关键词(逗号分隔)"
            value={keywords}
            onChange={e => setKeywords(e.target.value)}
          />
        </div>

        <button className="agent-btn-primary agent-full-btn" disabled={creating} onClick={handleCreate}>
          {creating ? '创建中…' : '创建 Agent'}
        </button>
        <p className="agent-form-note">
          创建后默认草稿状态，点击「激活」后常驻运行器会自动匹配竞拍并在预算内出价；赢拍后需你人工审批 Pact 才能支付。
        </p>
      </div>

      {/* 列表 */}
      {loading ? (
        <div className="empty-state-box">加载中…</div>
      ) : agents.length === 0 ? (
        <div className="empty-state-box">
          <div className="empty-icon">🤖</div>
          <p className="empty-title">还没有 Agent</p>
          <p className="sub">在上方用一句话描述你的拍卖意图来创建第一个 Agent</p>
        </div>
      ) : (
        <div className="card-list">
          {agents.map(a => {
            const strategy = parseStrategy(a.strategyJson)
            return (
              <div key={a.id} className="agent-card agent-card-clickable" style={{ cursor: 'pointer' }} onClick={() => navigate(`/agents/${a.id}`)}>
                <div className="agent-card-title-row">
                  <span className="agent-card-title">Agent #{a.id}</span>
                  <span className="agent-status-pill" style={{ color: statusColor[a.status] || 'var(--text-muted)' }}>
                    {statusLabel[a.status] || a.status}
                  </span>
                </div>
                <div style={{ fontSize: 13, color: 'var(--text-secondary)', margin: '6px 0 12px', lineHeight: 1.4 }}>{a.prompt || '（无描述）'}</div>
                
                <div className="agent-info-row">
                  <span className="agent-info-label">预算上限</span>
                  <span className="agent-info-price">¥{formatYuan(a.maxBudgetCents)}</span>
                </div>
                <div className="agent-info-row">
                  <span className="agent-info-label">出价策略</span>
                  <span className="agent-info-value">
                    {strategy.trigger || strategy.pace || strategy.stopRatio
                      ? strategyDisplayLabel(strategy)
                      : legacyStrategyLabel[strategy.strategy || ''] || strategy.strategy || '主动'}
                  </span>
                </div>
                <div className="agent-info-row">
                  <span className="agent-info-label">出价限制</span>
                  <span className="agent-info-value">{strategy.maxBidTimes || 100} 次 · {strategy.minIntervalMs || 3000} ms</span>
                </div>
                
                {strategy.productKeywords && strategy.productKeywords.length > 0 && (
                  <div style={{ display: 'flex', flexWrap: 'wrap', marginTop: 8 }}>
                    {strategy.productKeywords.map((k, i) => (
                      <span key={i} className="agent-tag">{k}</span>
                    ))}
                  </div>
                )}
                <div style={{ display: 'flex', gap: 10, marginTop: 16 }} onClick={e => e.stopPropagation()}>
                  {a.status !== 'active' && a.status !== 'expired' && a.status !== 'stopped_after_win' && (
                    <button className="agent-btn-primary" style={{ flex: 1 }} onClick={() => handleActivate(a.id)}>激活</button>
                  )}
                  {a.status === 'active' && (
                    <button className="agent-btn-secondary" style={{ flex: 1 }} onClick={() => handlePause(a.id)}>暂停</button>
                  )}
                  <button className="agent-btn-secondary" style={{ flex: 1 }} onClick={() => navigate(`/agents/${a.id}`)}>决策回放 →</button>
                </div>
              </div>
            )
          })}
        </div>
      )}
    </div>
  )
}
