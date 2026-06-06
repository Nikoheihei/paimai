import { useState, useEffect, useRef, useCallback } from 'react'
import { serverNow } from '../utils/serverTime'

function formatTime(ms: number): string {
  if (ms <= 0) return '00:00:00'
  const totalSec = Math.floor(ms / 1000)
  const h = Math.floor(totalSec / 3600)
  const m = Math.floor((totalSec % 3600) / 60)
  const s = totalSec % 60
  const pad = (n: number) => String(n).padStart(2, '0')
  return h > 0 ? `${pad(h)}:${pad(m)}:${pad(s)}` : `${pad(m)}:${pad(s)}`
}

type Props = {
  endAt: string
  onEnd?: () => void
}

export default function Countdown({ endAt, onEnd }: Props) {
  const [left, setLeft] = useState(() => Math.max(0, new Date(endAt).getTime() - serverNow()))
  const onEndRef = useRef(onEnd)
  onEndRef.current = onEnd

  // 当 endAt 变化时重新开始倒计时
  const endTime = useRef(new Date(endAt).getTime())
  endTime.current = new Date(endAt).getTime()

  const tick = useCallback(() => {
    const remaining = Math.max(0, endTime.current - serverNow())
    setLeft(remaining)
    if (remaining <= 0) {
      onEndRef.current?.()
    }
  }, [])

  useEffect(() => {
    // endAt 变化时，重置剩余时间并重启定时器
    setLeft(Math.max(0, endTime.current - serverNow()))
    const timer = setInterval(tick, 1000)
    return () => clearInterval(timer)
  }, [endAt, tick])

  const urgent = left < 30000 // 30秒内红色闪烁

  return (
    <span className={`countdown ${urgent ? 'urgent' : ''}`}>
      {formatTime(left)}
    </span>
  )
}
