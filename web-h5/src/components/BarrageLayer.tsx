/**
 * BarrageLayer — 弹幕/评论展示层（纯前端模拟）
 *
 * 仿抖音直播间弹幕效果：
 * - 弹幕从右向左飘过
 * - 自己发的弹幕高亮（金色边框）
 * - 支持系统公告（居中红色）
 * - 最多同时显示 15 条，避免性能问题
 */

import { useEffect, useRef, useState, useCallback } from 'react'

export type BarrageItem = {
  id: string
  text: string
  isSelf: boolean
  isSystem?: boolean
  color?: string
}

type Props = {
  messages: BarrageItem[]
  maxVisible?: number
}

export default function BarrageLayer({ messages, maxVisible = 15 }: Props) {
  const [visible, setVisible] = useState<BarrageItem[]>([])
  const timers = useRef<Record<string, ReturnType<typeof setTimeout>>>({})

  const removeItem = useCallback((id: string) => {
    setVisible(prev => prev.filter(m => m.id !== id))
    delete timers.current[id]
  }, [])

  useEffect(() => {
    if (messages.length === 0) return
    const latest = messages[messages.length - 1]

    setVisible(prev => {
      if (prev.some(v => v.id === latest.id)) return prev
      const next = [...prev, latest]
      if (next.length > maxVisible) next.shift()
      return next
    })

    // 6秒后自动移除
    timers.current[latest.id] = setTimeout(() => removeItem(latest.id), 6000)
  }, [messages, maxVisible, removeItem])

  useEffect(() => {
    const currentTimers = timers.current
    return () => {
      Object.values(currentTimers).forEach(clearTimeout)
    }
  }, [])

  return (
    <div className="barrage-layer">
      {visible.map((msg, idx) => (
        <div
          key={msg.id}
          className={`barrage-item ${msg.isSelf ? 'self' : ''} ${msg.isSystem ? 'system' : ''}`}
          style={{
            top: `${(idx % 8) * 12 + 8}%`,
            animationDuration: `${5 + Math.random() * 3}s`,
            color: msg.color || (msg.isSystem ? '#ff4d4f' : undefined),
          }}
        >
          {msg.isSystem ? (
            <span className="barrage-system-text">{msg.text}</span>
          ) : (
            <>
              <span className="barrage-name">{msg.isSelf ? '我' : `用户${msg.id.slice(-4)}`}</span>
              <span className="barrage-sep">：</span>
              <span className="barrage-text">{msg.text}</span>
            </>
          )}
        </div>
      ))}
    </div>
  )
}

/** 生成随机弹幕（模拟其他观众发言） */
export function randomBarrage(seed: number): BarrageItem {
  const texts = [
    '这个真不错', '出价了出价了', '好贵啊', '冲冲冲', '想要这个',
    '主播讲解一下', '还有多久结束', '我出500', '竞争激烈', '捡漏了',
    '这个值', '观望中', '价格还行', '再加一手', '拿下了吗',
  ]
  return {
    id: `sim-${Date.now()}-${seed}`,
    text: texts[Math.floor(Math.random() * texts.length)],
    isSelf: false,
  }
}
