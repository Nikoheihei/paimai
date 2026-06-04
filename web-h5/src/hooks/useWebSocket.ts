/**
 * useWebSocket — 竞拍系统 WebSocket 连接 Hook
 *
 * 职责：
 *  - 连接 / 自动重连（指数退避）
 *  - 心跳保活（ping/pong）
 *  - 消息分发（按 type 回调）
 *  - JWT token 携带（优先 Authorization 头，兼容 query 参数 fallback）
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { getToken } from '../api/client'

export type WsMessage = {
  type: string
  data: unknown
}

export type WsHandlers = {
  onMessage?: (msg: WsMessage) => void
  onConnected?: () => void
  onDisconnected?: () => void
}

const RECONNECT_BASE_MS = 1000
const RECONNECT_MAX_MS = 30000
const PING_INTERVAL_MS = 25000

/**
 * useWebSocket — 连接竞拍直播间的 WebSocket，自动处理重连和心跳。
 *
 * JWT token 通过 query 参数 ?token=xxx 传递（WebSocket 无法自定义请求头）。
 */
export function useWebSocket(
  roomId: number | undefined,
  userId: number | undefined,
  handlers: WsHandlers,
) {
  const [connected, setConnected] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectAttemptRef = useRef(0)
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const pingTimerRef = useRef<ReturnType<typeof setInterval> | undefined>(undefined)
  const handlersRef = useRef(handlers)
  handlersRef.current = handlers

  const cleanup = useCallback(() => {
    if (pingTimerRef.current !== undefined) {
      clearInterval(pingTimerRef.current)
      pingTimerRef.current = undefined
    }
    if (reconnectTimerRef.current !== undefined) {
      clearTimeout(reconnectTimerRef.current)
      reconnectTimerRef.current = undefined
    }
    if (wsRef.current) {
      wsRef.current.onopen = null
      wsRef.current.onclose = null
      wsRef.current.onmessage = null
      wsRef.current.onerror = null
      wsRef.current.close()
      wsRef.current = null
    }
  }, [])

  const connect = useCallback(() => {
    if (!roomId || !userId) return
    cleanup()

    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const host = window.location.host || 'localhost:8080'

    // 优先携带 JWT token（WebSocket 不支持自定义头，通过 query 传递）
    const token = getToken()
    const tokenParam = token ? `&token=${encodeURIComponent(token)}` : ''
    const url = `${protocol}//${host}/api/rooms/${roomId}/ws?userId=${userId}${tokenParam}`

    const ws = new WebSocket(url)
    wsRef.current = ws

    ws.onopen = () => {
      reconnectAttemptRef.current = 0
      setConnected(true)
      handlersRef.current.onConnected?.()

      pingTimerRef.current = setInterval(() => {
        if (ws.readyState === WebSocket.OPEN) {
          ws.send(JSON.stringify({ type: 'ping' }))
        }
      }, PING_INTERVAL_MS)
    }

    ws.onclose = () => {
      setConnected(false)
      handlersRef.current.onDisconnected?.()

      // 指数退避重连
      const attempt = reconnectAttemptRef.current
      const delay = Math.min(
        RECONNECT_BASE_MS * Math.pow(2, attempt),
        RECONNECT_MAX_MS,
      )
      reconnectAttemptRef.current = attempt + 1

      reconnectTimerRef.current = setTimeout(() => {
        connect()
      }, delay)
    }

    ws.onmessage = (event) => {
      try {
        const msg: WsMessage = JSON.parse(event.data)
        handlersRef.current.onMessage?.(msg)
      } catch {
        // 忽略非 JSON 消息
      }
    }

    ws.onerror = () => {
      // onerror 之后一定会触发 onclose，重连在 onclose 中处理
    }
  }, [roomId, userId, cleanup])

  useEffect(() => {
    connect()
    return cleanup
  }, [connect, cleanup])

  return { connected, reconnect: connect }
}
