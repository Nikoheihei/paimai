import { useState, useEffect } from 'react'

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
  const [left, setLeft] = useState(() => Math.max(0, new Date(endAt).getTime() - Date.now()))

  useEffect(() => {
    const timer = setInterval(() => {
      const remaining = Math.max(0, new Date(endAt).getTime() - Date.now())
      setLeft(remaining)
      if (remaining <= 0) { clearInterval(timer); onEnd?.() }
    }, 1000)
    return () => clearInterval(timer)
  }, [endAt, onEnd])

  const urgent = left < 30000 // 30秒内红色闪烁

  return (
    <span className={`countdown ${urgent ? 'urgent' : ''}`}>
      {formatTime(left)}
    </span>
  )
}
