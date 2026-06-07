/**
 * 04-diagnose.mjs — 压测诊断脚本
 *
 * 用法: node scripts/load-test/04-diagnose.mjs
 *
 * 压测同时采集:
 *   MySQL: Threads_running, Threads_connected, InnoDB row lock waits, 慢查询
 *   Redis: connected_clients, instantaneous_ops_per_sec, rejected_connections
 *   Outbox: 未消费数量
 *   服务: CPU / MEM
 */

import { execSync } from 'child_process'

const INTERVAL = 2000 // 每 2 秒采集一次
const DURATION = 60000 // 采集 60 秒（足够覆盖压测）

async function mysqlQuery(query) {
  try {
    return execSync(
      `docker exec paimai-mysql mysql -uroot -prootpassword -N -e "${query}" paimai 2>/dev/null`,
      { encoding: 'utf-8', timeout: 5000 },
    ).trim()
  } catch { return '' }
}

async function redisInfo(section) {
  try {
    return execSync(
      `docker exec paimai-redis-master redis-cli INFO ${section} 2>/dev/null`,
      { encoding: 'utf-8', timeout: 5000 },
    ).trim()
  } catch { return '' }
}

async function collect() {
  const ts = new Date().toISOString()

  // MySQL
  const threadsRunning = await mysqlQuery("SHOW STATUS LIKE 'Threads_running'")
  const threadsConnected = await mysqlQuery("SHOW STATUS LIKE 'Threads_connected'")
  const slowQueries = await mysqlQuery("SHOW STATUS LIKE 'Slow_queries'")
  const questions = await mysqlQuery("SHOW STATUS LIKE 'Questions'")
  const innodbRowLockWaits = await mysqlQuery("SHOW STATUS LIKE 'Innodb_row_lock_waits'")
  const innodbRowLockTime = await mysqlQuery("SHOW STATUS LIKE 'Innodb_row_lock_time'")
  const innodbRowLockCurrentWaits = await mysqlQuery("SHOW STATUS LIKE 'Innodb_row_lock_current_waits'")

  // Outbox 未消费数量
  const outboxPending = await mysqlQuery("SELECT COUNT(*) FROM outbox_events WHERE status='pending'")
  const outboxTotal = await mysqlQuery("SELECT COUNT(*) FROM outbox_events")

  // Redis
  const redisStats = await redisInfo('stats')
  const redisClients = await redisInfo('clients')
  const redisCPU = await redisInfo('cpu')

  const parseRedisVal = (section, key) => {
    const m = section.match(new RegExp(`${key}:(\\S+)`))
    return m ? m[1] : '-'
  }

  // 服务资源
  let cpu = '-', mem = '-'
  try {
    const dockerStats = execSync(
      'docker stats paimai-server --no-stream --format "{{.CPUPerc}}|{{.MemUsage}}" 2>/dev/null',
      { encoding: 'utf-8', timeout: 5000 },
    ).trim()
    const parts = dockerStats.split('|')
    cpu = parts[0]?.trim() || '-'
    mem = parts[1]?.trim() || '-'
  } catch {}

  // WebSocket 连接数（通过 admin stats API）
  let wsConnections = '-'
  try {
    const roomsRes = await fetch('http://localhost:8080/api/admin/rooms', {
      headers: { Authorization: 'Bearer PLACEHOLDER' },
    })
    wsConnections = 'N/A'
  } catch { wsConnections = 'N/A' }

  return {
    ts,
    mysql: {
      threads_running: threadsRunning.split('\t')[1] || '-',
      threads_connected: threadsConnected.split('\t')[1] || '-',
      slow_queries: slowQueries.split('\t')[1] || '-',
      questions: questions.split('\t')[1] || '-',
      innodb_row_lock_waits: innodbRowLockWaits.split('\t')[1] || '-',
      innodb_row_lock_time: innodbRowLockTime.split('\t')[1] || '-',
      innodb_row_lock_current_waits: innodbRowLockCurrentWaits.split('\t')[1] || '-',
    },
    outbox: {
      pending: outboxPending || '-',
      total: outboxTotal || '-',
    },
    redis: {
      ops_per_sec: parseRedisVal(redisStats, 'instantaneous_ops_per_sec'),
      connected_clients: parseRedisVal(redisClients, 'connected_clients'),
      rejected_connections: parseRedisVal(redisStats, 'rejected_connections'),
      used_cpu_sys: parseRedisVal(redisCPU, 'used_cpu_sys'),
    },
    server: { cpu, mem },
  }
}

async function main() {
  console.log('压测诊断采集器启动（每 2 秒一次，共 60 秒）')
  console.log('请在另一个终端启动压测脚本...')
  console.log('')

  // CSV 表头
  const keys = [
    'ts', 'threads_running', 'threads_connected', 'slow_queries',
    'innodb_row_lock_waits', 'innodb_row_lock_time', 'innodb_row_lock_current',
    'outbox_pending', 'outbox_total',
    'redis_ops', 'redis_clients', 'redis_rejected',
    'server_cpu', 'server_mem',
  ]
  console.log(keys.join(','))

  const startTime = Date.now()
  const samples = []

  while (Date.now() - startTime < DURATION) {
    const data = await collect()
    const row = [
      data.ts,
      data.mysql.threads_running,
      data.mysql.threads_connected,
      data.mysql.slow_queries,
      data.mysql.innodb_row_lock_waits,
      data.mysql.innodb_row_lock_time,
      data.mysql.innodb_row_lock_current_waits,
      data.outbox.pending,
      data.outbox.total,
      data.redis.ops_per_sec,
      data.redis.connected_clients,
      data.redis.rejected_connections,
      data.server.cpu,
      data.server.mem,
    ]
    console.log(row.join(','))
    samples.push(data)

    await new Promise(r => setTimeout(r, INTERVAL - (Date.now() - startTime) % INTERVAL))
  }

  // 汇总
  console.log('')
  console.log('=== 诊断汇总 ===')
  const maxThreadsRunning = Math.max(...samples.map(s => parseInt(s.mysql.threads_running) || 0))
  const maxInnodbLockWaits = Math.max(...samples.map(s => parseInt(s.mysql.innodb_row_lock_waits) || 0))
  const maxOutboxPending = Math.max(...samples.map(s => parseInt(s.outbox.pending) || 0))
  const maxRedisOps = Math.max(...samples.map(s => parseInt(s.redis.ops_per_sec) || 0))
  const maxRedisClients = Math.max(...samples.map(s => parseInt(s.redis.connected_clients) || 0))

  console.log(`  MySQL 最大运行线程: ${maxThreadsRunning}`)
  console.log(`  MySQL InnoDB 行锁等待(累计): ${maxInnodbLockWaits}`)
  console.log(`  Outbox 最大未消费: ${maxOutboxPending}`)
  console.log(`  Redis 最大 OPS: ${maxRedisOps}`)
  console.log(`  Redis 最大连接客户端: ${maxRedisClients}`)
  console.log(`  Redis 拒绝连接: ${samples[samples.length - 1].redis.rejected_connections}`)
  console.log(`  慢查询增长: ${(parseInt(samples[samples.length - 1].mysql.slow_queries) || 0) - (parseInt(samples[0].mysql.slow_queries) || 0)}`)
}

main().catch(err => console.error('诊断失败:', err.message))
