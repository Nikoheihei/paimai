export const PAYMENT_WINDOW_SECONDS = 5 * 60

export function paymentDeadlineMs(createdAt?: string | null, now = Date.now()): number {
  const createdMs = createdAt ? Date.parse(createdAt) : Number.NaN
  if (Number.isFinite(createdMs)) {
    return createdMs + PAYMENT_WINDOW_SECONDS * 1000
  }
  return now + PAYMENT_WINDOW_SECONDS * 1000
}

export function remainingPaymentSeconds(deadlineMs: number, now = Date.now()): number {
  if (!Number.isFinite(deadlineMs) || deadlineMs <= 0) return 0
  return Math.max(0, Math.ceil((deadlineMs - now) / 1000))
}

export function formatPaymentCountdown(seconds: number): string {
  const safeSeconds = Math.max(0, seconds)
  return `${Math.floor(safeSeconds / 60)}:${String(safeSeconds % 60).padStart(2, '0')}`
}
