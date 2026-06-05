let offset = 0
let synced = false

export async function syncServerTime(): Promise<void> {
  const clientStart = Date.now()
  const res = await fetch('/api/server-time')
  const data = await res.json()
  const clientEnd = Date.now()
  const clientMid = (clientStart + clientEnd) / 2
  offset = data.serverTime - clientMid
  synced = true
}

export function serverNow(): number {
  if (!synced) return Date.now()
  return Date.now() + offset
}
