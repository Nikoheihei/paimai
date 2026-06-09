/**
 * AgentDetailPage — Agent 决策回放（审计时间线）
 *
 * 入口：Agent 列表点击卡片 / 「决策回放」。
 * 展示：策略、状态、激活/暂停、append-only 审计时间线。
 */

import { useCallback, useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import type { AgentProfile, AgentAuditLog, AgentStrategy } from '../api/client'
import { listBuyerAgents, getAgentAudit, activateAgent, pauseAgent } from '../api/client'

function formatYuan(cents: number) {
  return (cents / 100).toFixed(2)
}

const statusLabel: Record<string, string> = {
  draft: '草稿', active: '运行中', paused: '已暂停', stopped_after_win: '赢拍已停', expired: '已过期',
}

const strategyLabel: Record<string, string> = {
  conservative: '保守',
  follow_up: '跟价',
  reserve_then_follow: '保留价优先',
  cap_only: '预算封顶',
  custom: '自定义',
}

const triggerLabel: Record<string, string> = {
  lead: '主动出价',
  follow: '跟价模式',
}

const paceLabel: Record<string, string> = {
  min_step: '最小步长',
  reserve: '保留价优先',
}

/** 策略显示：优先用新3维度字段，兼容旧策略名 */
function strategyDisplay(s: AgentStrategy): string {
  if (s.trigger || s.pace || s.stopRatio) {
    const t = triggerLabel[s.trigger || 'lead'] || '主动出价'
    const p = paceLabel[s.pace || 'min_step'] || '最小步长'
    const stop = s.stopRatio && s.stopRatio > 0 && s.stopRatio < 1
      ? `${Math.round(s.stopRatio * 100)}%预算停止`
      : '预算上限停止'
    return `${t} · ${p} · ${stop}`
  }
  return strategyLabel[s.strategy || ''] || s.strategy || '主动出价'
}

// 审计动作中文标签 + 图标
const actionMeta: Record<string, { label: string; icon: string }> = {
  'agent.created': { label: 'Agent 创建', icon: '🤖' },
  'agent.intent.parsed': { label: '意图解析', icon: '🧠' },
  'agent.activated': { label: '激活', icon: '▶️' },
  'agent.paused': { label: '暂停', icon: '⏸️' },
  'agent.auction.matched': { label: '匹配竞拍', icon: '🎯' },
  'agent.bid.submitted': { label: '提交出价', icon: '💰' },
  'agent.bid.won': { label: '赢得竞拍', icon: '🏆' },
  'pact.created': { label: 'Pact 创建', icon: '📜' },
  'pact.approved': { label: 'Pact 批准', icon: '✅' },
  'pact.rejected': { label: 'Pact 拒绝', icon: '❌' },
  'pact.payment_gate.passed': { label: '支付校验通过', icon: '🔓' },
  'order.paid': { label: '订单已支付', icon: '🎉' },
  'order.payment_failed': { label: '支付失败', icon: '⚠️' },
}

function parseStrategy(json: string): AgentStrategy {
  try { return JSON.parse(json || '{}') as AgentStrategy } catch { return {} }
}

function prettyPayload(json: string): string {
  try { return JSON.stringify(JSON.parse(json), null, 2) } catch { return json }
}

export default function AgentDetailPage() {
  const { agentId } = useParams<{ agentId: string }>()
  const id = Number(agentId)
  const navigate = useNavigate()

  const [agent, setAgent] = useState<AgentProfile | null>(null)
  const [logs, setLogs] = useState<AgentAuditLog[]>([])
  const [loading, setLoading] = useState(true)
  const [msg, setMsg] = useState('')
  const [expanded, setExpanded] = useState<number | null>(null)

  const load = useCallback(() => {
    Promise.all([listBuyerAgents(), getAgentAudit(id, 200)])
      .then(([agents, auditLogs]) => {
        setAgent(agents.find(a => a.id === id) || null)
        setLogs(auditLogs)
      })
      .catch(err => setMsg(err.message || '加载失败'))
      .finally(() => setLoading(false))
  }, [id])

  useEffect(() => {
    load()
    const timer = window.setInterval(load, 3000)
    return () => window.clearInterval(timer)
  }, [load])

  const handleActivate = async () => {
    try { await activateAgent(id); load() } catch (err: any) { setMsg(err.message || '激活失败') }
  }
  const handlePause = async () => {
    try { await pauseAgent(id); load() } catch (err: any) { setMsg(err.message || '暂停失败') }
  }

  if (loading && !agent) return <div className="empty-state-box">加载中…</div>

  const strategy = agent ? parseStrategy(agent.strategyJson) : {}

  return (
    <div className="room-list-page" style={{ paddingTop: 16 }}>
      <div className="page-header-row">
        <button className="back-btn" onClick={() => navigate('/agents')}>←</button>
        <h2 style={{ margin: 0, marginLeft: 8 }}>Agent 详情</h2>
      </div>

      {msg && <div className="toast-wrap"><div className="toast-item" onClick={() => setMsg('')}>{msg}</div></div>}

      {agent && (
        <div className="agent-card">
          <div className="agent-card-title-row">
            <h3 style={{ margin: 0, color: 'var(--text-primary)' }}>Agent #{agent.id}</h3>
            <span style={{ fontWeight: 700, fontSize: 12, background: 'rgba(255,255,255,0.06)', padding: '2px 8px', borderRadius: 8 }}>{statusLabel[agent.status] || agent.status}</span>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 14, marginTop: 14 }}>
            <div>
              <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>意图</div>
              <div style={{ fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.5 }}>{agent.prompt || '—'}</div>
            </div>
            <div>
              <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>预算上限</div>
              <div style={{ fontSize: 18, fontWeight: 700, color: 'var(--primary)', fontStyle: 'italic' }}>¥{formatYuan(agent.maxBudgetCents)}</div>
            </div>
            <div>
              <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>出价策略</div>
              <div style={{ fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.5 }}>
                {strategyDisplay(strategy)} · {strategy.maxBidTimes || 100} 次 · {strategy.minIntervalMs || 3000} ms
              </div>
            </div>
            {(strategy.roomId || strategy.auctionId) && (
              <div>
                <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 4 }}>竞拍范围</div>
                <div style={{ fontSize: 14, color: 'var(--text-primary)', lineHeight: 1.5 }}>
                  {strategy.roomId ? `房间 #${strategy.roomId}` : '任意房间'} · {strategy.auctionId ? `拍品 #${strategy.auctionId}` : '任意拍品'}
                </div>
              </div>
            )}
            <div>
              <div style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 6 }}>关键词</div>
              {strategy.productKeywords && strategy.productKeywords.length > 0 ? (
                <div style={{ display: 'flex', flexWrap: 'wrap' }}>
                  {strategy.productKeywords.map((k, i) => (
                    <span key={i} className="agent-tag">{k}</span>
                  ))}
                </div>
              ) : (
                <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>广义匹配（无关键词限制）</span>
              )}
            </div>
          </div>
          <div style={{ display: 'flex', gap: 10, marginTop: 16 }}>
            {agent.status !== 'active' && agent.status !== 'expired' && agent.status !== 'stopped_after_win' && (
              <button className="agent-btn-primary" style={{ flex: 1 }} onClick={handleActivate}>激活</button>
            )}
            {agent.status === 'active' && (
              <button className="agent-btn-secondary" style={{ flex: 1 }} onClick={handlePause}>暂停</button>
            )}
            <button className="agent-btn-secondary" style={{ flex: 1 }} onClick={() => navigate('/pacts')}>赢拍审批 →</button>
          </div>
        </div>
      )}

      <div className="agent-card">
        <h4 style={{ marginTop: 0, marginBottom: 16, color: 'var(--text-primary)', fontSize: 15 }}>决策回放（{logs.length}）</h4>
        {logs.length === 0 ? (
          <div className="empty-state-box" style={{ padding: 16 }}>
            <p className="empty-title">暂无决策记录</p>
            <p className="sub">激活后 Agent 的每一步匹配、出价、赢拍都会记录在这里</p>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column' }}>
            {logs.map((log, idx) => {
              const meta = actionMeta[log.actionType] || { label: log.actionType, icon: '•' }
              const isOpen = expanded === log.id
              return (
                <div key={log.id} className="agent-timeline-item">
                  <div className="agent-timeline-icon-wrap">
                    <div className="agent-timeline-icon">{meta.icon}</div>
                    {idx < logs.length - 1 && <div className="agent-timeline-line" />}
                  </div>
                  <div className="agent-timeline-content">
                    <div className="agent-timeline-header">
                      <span className="agent-timeline-title">{meta.label}</span>
                      <span className="agent-timeline-time">{new Date(log.timestampMs).toLocaleString('zh-CN')}</span>
                    </div>
                    <div className="agent-timeline-meta">
                      操作者：{log.operator} · trace {log.traceId.slice(0, 14)}…
                    </div>
                    <button
                      className="agent-timeline-details-btn"
                      onClick={() => setExpanded(isOpen ? null : log.id)}
                    >
                      {isOpen ? '收起详情' : '查看详情'}
                    </button>
                    {isOpen && (
                      <pre className="agent-timeline-pre">
                        {prettyPayload(log.payloadJson)}
                      </pre>
                    )}
                  </div>
                </div>
              )
            })}
          </div>
        )}
      </div>
    </div>
  )
}
