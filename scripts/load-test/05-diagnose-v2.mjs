/**
 * 05-diagnose-v2.mjs — 压测诊断脚本 v2（多房间多拍卖）
 *
 * 监控指标:
 *   Consumer Lag: XINFO GROUPS auction:events
 *   Redis Stream 长度: XLEN auction:events
 *   Outbox 积压: SELECT COUNT(*) FROM outbox_events WHERE status='pending'
 *   MySQL 线程/锁
 *   Go goroutine: pprof
 *   Redis OPS/连接/拒绝
 *   Server CPU/MEM
 */

import { execSync } from 'child_process'

const INTERVAL = 1000
const DURATION = 30000

async function redisCmd(...args) {
  try {
    return execSync(`docker exec paimai-redis-master redis-cli ${args.join(' ')} 2>/dev/null`, { encoding: 'utf-8', timeout: 2000 }).trim()
  } catch { return '' }
}

async function mysqlQuery(query) {
  try {
    return execSync(`docker exec paimai-mysql mysql -uroot -prootpassword -N -e "${query}" paimai 2>/dev/null`, { encoding: 'utf-8', timeout: 2000 }).trim()
  } catch { return '' }
}

async function collect() {
  const ts = new Date().toISOString()

  // MySQL
  const threadsRunning = (await mysqlQuery("SHOW STATUS LIKE 'Threads_running'")).split('\t')[1] || '-'
  const slowQueries = (await mysqlQuery("SHOW STATUS LIKE 'Slow_queries'")).split('\t')[1] || '-'
  const innodbLockCurrent = (await mysqlQuery("SHOW STATUS LIKE 'Innodb_row_lock_current_waits'")).split('\t')[1] || '-'
  const outboxPending = await mysqlQuery("SELECT COUNT(*) FROM outbox_events WHERE status='pending'") || '-'

  // Redis Stream
  const streamLen = await redisCmd('XLEN', 'auction:events') || '-'

  // Consumer Lag
  let consumerPending = '', consumerLag = ''
  try {
    const groups = await redisCmd('XINFO', 'GROUPS', 'auction:events')
    const pm = groups.match(/pending:(\d+)/)
    const lm = groups.match(/lag:(\d+)/)
    if (pm) consumerPending = pm[1]
    if (lm) consumerLag = lm[1]
  } catch {}

  // Redis
  const redisStats = await redisCmd('INFO', 'stats')
  const redisClientsInfo = await redisCmd('INFO', 'clients')
  const redisOps = (redisStats.match(/instantaneous_ops_per_sec:(\S+)/) || [])[1] || '-'
  const redisClients = (redisClientsInfo.match(/connected_clients:(\S+)/) || [])[1] || '-'
  const redisRejected = (redisStats.match(/rejected_connections:(\S+)/) || [])[1] || '-'

  // Go goroutine
  let goroutines = '-'
  try {
    const res = await fetch('http://localhost:8080/debug/pprof/goroutine?debug=1', { signal: AbortSignal.timeout(2000) })
    const text = await res.text()
    goroutines = String(text.split('\n').filter(l => l.startsWith('goroutine')).length)
  } catch {}

  // Server
  let cpu = '-', mem = '-'
  try {
    const s = execSync('docker stats paimai-server --no-stream --format "{{.CPUPerc}}|{{.MemUsage}}" 2>/dev/null', { encoding: 'utf-8', timeout: 2000 }).trim()
    const p = s.split('|')
    cpu = p[0]?.trim() || '-'
    mem = p[1]?.trim() || '-'
  } catch {}

  return { ts, threadsRunning, slowQueries, innodbLockCurrent, outboxPending, streamLen, consumerPending, consumerLag, redisOps, redisClients, redisRejected, goroutines, cpu, mem }
}

async function main() {
  console.log('诊断v2启动（每秒采集，30秒）')
  console.log('ts,threads_running,slow,innodb_lock_cur,outbox_pending,stream_len,consumer_pending,consumer_lag,redis_ops,redis_clients,redis_rejected,goroutines,cpu,mem')

  const start = Date.now()
  const samples = []
  while (Date.now() - start < DURATION) {
    const d = await collect()
    console.log([d.ts, d.threadsRunning, d.slowQueries, d.innodbLockCurrent, d.outboxPending, d.streamLen, d.consumerPending, d.consumerLag, d.redisOps, d.redisClients, d.redisRejected, d.goroutines, d.cpu, d.mem].join(','))
    samples.push(d)
    await new Promise(r => setTimeout(r, INTERVAL - (Date.now() - start) % INTERVAL))
  }

  // 汇总
  console.log('')
  console.log('=== 汇总 ===')
  const maxVal = (samples, key) => Math.max(...samples.map(s => parseInt(s[key]) || 0))
  console.log('MySQL 最大运行线程:', maxVal(samples, 'threadsRunning'))
  console.log('Outbox 最大未消费:', maxVal(samples, 'outboxPending'))
  console.log('Stream 最大长度:', maxVal(samples, 'streamLen'))
  console.log('Consumer 最大 pending:', maxVal(samples, 'consumerPending'))
  console.log('Consumer 最大 lag:', maxVal(samples, 'consumerLag'))
  console.log('Redis 最大 OPS:', maxVal(samples, 'redisOps'))
  console.log('Go 最大 goroutines:', maxVal(samples, 'goroutines'))
  console.log('Redis 拒绝:', samples[samples.length - 1].redisRejected)
  console.log('慢查询增长:', (parseInt(samples[samples.length - 1].slowQueries) || 0) - (parseInt(samples[0].slowQueries) || 0))
}

main().catch(err => console.error('诊断失败:', err.message))
