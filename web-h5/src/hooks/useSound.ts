/**
 * useSound — 出价音效 Hook
 *
 * 使用 Web Audio API 播放简单的提示音，无需外部音频文件。
 * 支持用户手动开关。
 */

import { useCallback, useRef, useState } from 'react'

export function useSound() {
  const [enabled, setEnabled] = useState(() => {
    return localStorage.getItem('paimai_sound') !== 'off'
  })
  const ctxRef = useRef<AudioContext | null>(null)

  const getCtx = useCallback(() => {
    if (!ctxRef.current) {
      ctxRef.current = new (window.AudioContext || (window as any).webkitAudioContext)()
    }
    return ctxRef.current
  }, [])

  const playTone = useCallback((freq: number, duration: number, type: OscillatorType = 'sine') => {
    if (!enabled) return
    try {
      const ctx = getCtx()
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      osc.type = type
      osc.frequency.setValueAtTime(freq, ctx.currentTime)
      gain.gain.setValueAtTime(0.08, ctx.currentTime)
      gain.gain.exponentialRampToValueAtTime(0.001, ctx.currentTime + duration)
      osc.connect(gain)
      gain.connect(ctx.destination)
      osc.start()
      osc.stop(ctx.currentTime + duration)
    } catch {
      // 忽略音频播放失败
    }
  }, [enabled, getCtx])

  const playBidSuccess = useCallback(() => {
    // 出价成功：两声清脆提示
    playTone(880, 0.1, 'sine')
    setTimeout(() => playTone(1100, 0.15, 'sine'), 120)
  }, [playTone])

  const playOutbid = useCallback(() => {
    // 被超越：低沉警示音
    playTone(300, 0.2, 'triangle')
    setTimeout(() => playTone(250, 0.3, 'triangle'), 150)
  }, [playTone])

  const playAuctionEnd = useCallback(() => {
    // 拍卖结束： celebratory 三声
    playTone(523, 0.15, 'sine')
    setTimeout(() => playTone(659, 0.15, 'sine'), 150)
    setTimeout(() => playTone(784, 0.3, 'sine'), 300)
  }, [playTone])

  const toggle = useCallback(() => {
    setEnabled(prev => {
      const next = !prev
      localStorage.setItem('paimai_sound', next ? 'on' : 'off')
      return next
    })
  }, [])

  return { enabled, toggle, playBidSuccess, playOutbid, playAuctionEnd }
}
