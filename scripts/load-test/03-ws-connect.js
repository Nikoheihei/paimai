/**
 * 03-ws-connect.js — k6 WebSocket 连接压测
 *
 * 用法:
 *   k6 run -e SETUP_FILE=setup.json -e VUS=100 -e DURATION=30s scripts/load-test/03-ws-connect.js
 *
 * 模拟大量用户连接同一房间的 WebSocket，验证广播延迟和消息丢失。
 */

import ws from 'k6/ws'
import { check, sleep } from 'k6'
import { Counter, Trend, Rate } from 'k6/metrics'
import { SharedArray } from 'k6/data'

const wsConnected = new Counter('ws_connected')
const wsDisconnected = new Counter('ws_disconnected')
const wsMsgReceived = new Counter('ws_msg_received')
const wsLatency = new Trend('ws_latency', true)
const wsErrorRate = new Rate('ws_errors')

const setupData = new SharedArray('setup', function () {
  return JSON.parse(open(__ENV.SETUP_FILE || 'setup.json'))
})

export const options = {
  vus: parseInt(__ENV.VUS || '100'),
  duration: __ENV.DURATION || '30s',
}

export default function () {
  const data = setupData[0]
  const roomId = data.room.id
  const buyer = data.buyers[__VU % data.buyers.length]
  const token = buyer.token

  // WebSocket 带 token 参数
  const wsUrl = `ws://${new URL(data.baseUrl).host}/api/rooms/${roomId}/ws?token=${token}`

  const startTime = Date.now()

  const response = ws.connect(wsUrl, null, function (socket) {
    wsConnected.add(1)

    socket.on('open', () => {
      // 连接成功后发送 ping
      socket.setInterval(() => {
        socket.ping()
      }, 5000)
    })

    socket.on('message', (msg) => {
      wsMsgReceived.add(1)
      const elapsed = Date.now() - startTime
      wsLatency.add(elapsed)

      try {
        const data = JSON.parse(msg)
        // 检查消息类型
        if (data.type) {
          check(data, {
            'valid message type': (d) =>
              ['bid.accepted', 'ranking.updated', 'auction.ended', 'timer.sync', 'outbid'].includes(d.type),
          })
        }
      } catch (_) {
        wsErrorRate.add(1)
      }
    })

    socket.on('close', () => {
      wsDisconnected.add(1)
    })

    socket.on('error', () => {
      wsErrorRate.add(1)
    })

    // 保持连接一段时间
    socket.setTimeout(() => {
      socket.close()
    }, Math.random() * 15000 + 5000)
  })

  check(response, {
    'ws connected': (r) => r && r.status === 101,
  })

  if (!response || response.status !== 101) {
    wsErrorRate.add(1)
  }

  sleep(1)
}

export function handleSummary(data) {
  const summary = {
    timestamp: new Date().toISOString(),
    config: { vus: options.vus, duration: options.duration },
    ws: {
      connected: data.metrics.ws_connected?.values?.count || 0,
      disconnected: data.metrics.ws_disconnected?.values?.count || 0,
      messages_received: data.metrics.ws_msg_received?.values?.count || 0,
      latency_avg: data.metrics.ws_latency?.values?.avg?.toFixed(2),
      error_rate: data.metrics.ws_errors?.values?.rate,
    },
  }
  return {
    'stdout': JSON.stringify(summary, null, 2),
    'results/ws-summary.json': JSON.stringify(summary, null, 2),
  }
}
