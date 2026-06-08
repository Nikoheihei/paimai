import { getToken, clearToken } from '../api/client'

export function parseUserIdFromToken(): number {
  const token = getToken()
  if (!token) return 0
  try {
    const payload = JSON.parse(atob(token.split('.')[1]))
    return payload.userId || 0
  } catch {
    return 0
  }
}

export function parseRoleFromToken(): string {
  const token = getToken()
  if (!token) return ''
  try {
    const payload = JSON.parse(atob(token.split('.')[1]))
    return payload.role || ''
  } catch {
    return ''
  }
}

export function handleUnauthorized() {
  clearToken()
  window.location.hash = '#/login' // Fallback until router is fully integrated
}
