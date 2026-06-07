/**
 * diagnose.mjs — 压测诊断脚本 v3（WS + goroutine + GC）
 *
 * 采集:
 *   /api/ws-stats: connections, rooms, broadcast_count/msgs, slow_broadcasts, slow_clients, event_queue
 *   /debug/pprof/goroutine?debug=1: goroutine count
 *   /debug/pprof/heap?debug=1: heap stats
 *   MySQL: threads, outbox pending, slow queries
 *   Redis Stream: XLEN, consumer lag/pending
 *   Redis INFO
 *   Docker stats: CPU/MEM
 */

import { execSync } from 'child_process'
import { writeFileSync } from 'fs'

const INTERVAL = 1000
const DURATION = 35000

async function redisCmd(...args) {
  try { return execSync(`docker exec paimai-redis-master redis-cli ${args.join(' ')} 2>/dev/null`, { encoding: 'utf-8', timeout: 2000 }).trim() } catch { return '' }
}

async function mysqlQuery(query) {
  try { return execSync(`docker exec paimai-mysql mysql -uroot -prootpassword -N -e "${query}" paimai 2>/dev/null`, { encoding: 'utf-8', timeout: 2000 }).trim() } catch { return '' }
}

async function fetchJson(url) {
  try {
    const res = await fetch(url, { signal: AbortSignal.timeout(2000) })
    return await res.json()
  } catch { return null }
}

async function collect() {
  const ts = new Date().toISOString()

  // WS stats
  const ws = await fetchJson('http://localhost:8080/api/ws-stats')
  const wsData = ws?.data || {}

  // pprof goroutines (端口 6060，不走 Gin 中间件)
  let goroutines = '-', heapAlloc = '-', numGC = '-', gcPause = '-'
  try {
    const grRes = await fetch('http://localhost:6060/debug/pprof/goroutine?debug=1', { signal: AbortSignal.timeout(2000) })
    const grText = await grRes.text()
    goroutines = String(grText.split('\n').filter(l => l.startsWith('goroutine')).length)
  } catch {}
  try {
    const heapRes = await fetch('http://localhost:6060/debug/pprof/heap?debug=1', { signal: AbortSignal.timeout(2000) })
    const heapText = await heapRes.text()
    const allocMatch = heapText.match(/HeapAlloc = (\d+)/)
    const gcMatch = heapText.match(/NumGC = (\d+)/)
    const pauseMatch = heapText.match(/PauseNs = (\d+)/)
    if (allocMatch) heapAlloc = allocMatch[1]
    if (gcMatch) numGC = gcMatch[1]
    if (pauseMatch) gcPause = pauseMatch[1]
  } catch {}

  // MySQL
  const threadsRunning = (await mysqlQuery("SHOW STATUS LIKE 'Threads_running'")).split('\t')[1] || '-'
  const slowQueries = (await mysqlQuery("SHOW STATUS LIKE 'Slow_queries'")).split('\t')[1] || '-'
  const outboxPending = await mysqlQuery("SELECT COUNT(*) FROM outbox_events WHERE status='pending'") || '-'

  // Redis Stream
  const streamLen = await redisCmd('XLEN', 'auction:events') || '-'
  let consumerPending = '', consumerLag = ''
  try {
    const groups = await redisCmd('XINFO', 'GROUPS', 'auction:events')
    const pm = groups.match(/pending:(\d+)/); if (pm) consumerPending = pm[1]
    const lm = groups.match(/lag:(\d+)/); if (lm) consumerLag = lm[1]
  } catch {}

  // Redis
  const redisStats = await redisCmd('INFO', 'stats')
  const redisClientsInfo = await redisCmd('INFO', 'clients')
  const redisOps = (redisStats.match(/instantaneous_ops_per_sec:(\S+)/) || [])[1] || '-'
  const redisClients = (redisClientsInfo.match(/connected_clients:(\S+)/) || [])[1] || '-'
  const redisRejected = (redisStats.match(/rejected_connections:(\S+)/) || [])[1] || '-'

  // Docker
  let cpu = '-', mem = '-'
  try {
    const s = execSync('docker stats paimai-server --no-stream --format "{{.CPUPerc}}|{{.MemUsage}}" 2>/dev/null', { encoding: 'utf-8', timeout: 2000 }).trim()
    const p = s.split('|'); cpu = p[0]?.trim() || '-'; mem = p[1]?.trim() || '-'
  } catch {}

  return {
    ts, ws_conns: wsData.connections || '-', ws_rooms: wsData.rooms || '-',
    ws_bcast: wsData.broadcast_count || '-', ws_msgs: wsData.broadcast_msgs || '-',
    ws_slow: wsData.slow_broadcasts || '-', ws_slow_clients: wsData.slow_clients_dropped || '-',
    ws_queue: wsData.event_queue_len || '-',
    goroutines, heapAlloc, numGC, gcPause,
    threadsRunning, slowQueries, outboxPending,
    streamLen, consumerPending, consumerLag,
    redisOps, redisClients, redisRejected, cpu, mem,
  }
}

async function main() {
  console.log('诊断v3启动（每秒采集，35秒）')
  console.log('ts,ws_conns,ws_rooms,ws_bcast,ws_msgs,ws_slow,ws_slow_clients,ws_queue,goroutines,heapAlloc,numGC,gcPause,threads,slow,outbox,stream_len,cons_pend,cons_lag,redis_ops,redis_clients,redis_reject,cpu,mem')

  const start = Date.now()
  const samples = []
  while (Date.now() - start < DURATION) {
    const d = await collect()
    console.log([d.ts, d.ws_conns, d.ws_rooms, d.ws_bcast, d.ws_msgs, d.ws_slow, d.ws_slow_clients, d.ws_queue, d.goroutines, d.heapAlloc, d.numGC, d.gcPause, d.threadsRunning, d.slowQueries, d.outboxPending, d.streamLen, d.consumerPending, d.consumerLag, d.redisOps, d.redisClients, d.redisRejected, d.cpu, d.mem].join(','))
    samples.push(d)
    await new Promise(r => setTimeout(r, INTERVAL - (Date.now() - start) % INTERVAL))
  }

  const maxVal = (samples, key) => Math.max(...samples.map(s => parseInt(s[key]) || 0))
  console.log('')
  console.log('=== 汇总 ===')
  console.log('WS 最大连接:', maxVal(samples, 'ws_conns'))
  console.log('WS 最大房间:', maxVal(samples, 'ws_rooms'))
  console.log('WS 累计广播次数:', maxVal(samples, 'ws_bcast'))
  console.log('WS 累计广播消息:', maxVal(samples, 'ws_msgs'))
  console.log('WS 慢广播次数:', maxVal(samples, 'ws_slow'))
  console.log('WS 慢客户端:', maxVal(samples, 'ws_slow_clients'))
  console.log('WS 事件队列最大长度:', maxVal(samples, 'ws_queue'))
  console.log('Go 最大 goroutines:', maxVal(samples, 'goroutines'))
  console.log('GC 次数:', samples[samples.length - 1].numGC)
  console.log('Outbox 最大未消费:', maxVal(samples, 'outboxPending'))
  console.log('Stream 最大长度:', maxVal(samples, 'streamLen'))
  console.log('Consumer 最大 pending:', maxVal(samples, 'consumerPending'))
  console.log('Consumer 最大 lag:', maxVal(samples, 'consumerLag'))
  console.log('MySQL 最大运行线程:', maxVal(samples, 'threadsRunning'))
  console.log('Redis 最大 OPS:', maxVal(samples, 'redisOps'))
  console.log('Redis 拒绝:', samples[samples.length - 1].redisRejected)
  console.log('慢查询增长:', (parseInt(samples[samples.length - 1].slowQueries) || 0) - (parseInt(samples[0].slowQueries) || 0))
}

main().catch(err => console.error('诊断失败:', err.message))
