/**
 * Toast — Admin 端轻提示（复用 H5 的 Toast 实现）
 */

let containerEl: HTMLDivElement | null = null

function getContainer(): HTMLDivElement {
  if (!containerEl) {
    containerEl = document.createElement('div')
    containerEl.id = 'admin-toast-container'
    containerEl.setAttribute(
      'style',
      'position:fixed;top:20px;right:20px;z-index:99999;display:flex;flex-direction:column;gap:8px;pointer-events:none;',
    )
    document.body.appendChild(containerEl)
  }
  return containerEl
}

type ToastType = 'success' | 'error' | 'info'

function createToast(message: string, type: ToastType = 'info', duration = 2500): HTMLDivElement {
  const el = document.createElement('div')
  const colorMap: Record<ToastType, string> = {
    success: '#4caf50',
    error: '#f44336',
    info: '#2196f3',
  }
  el.innerHTML = message
  el.setAttribute(
    'style',
    `background:${colorMap[type]};color:#fff;padding:10px 20px;border-radius:8px;font-size:14px;
     font-weight:600;box-shadow:0 4px 12px rgba(0,0,0,.2);animation:toastIn .25s ease;pointer-events:auto;
     max-width:360px;`,
  )

  if (!document.getElementById('admin-toast-styles')) {
    const style = document.createElement('style')
    style.id = 'admin-toast-styles'
    style.textContent = '@keyframes toastIn{from{opacity:0;transform:translateX(24px)}to{opacity:1;transform:translateX(0)}} @keyframes toastOut{from{opacity:1}to{opacity:0;transform:translateX(24px)}}'
    document.head.appendChild(style)
  }

  getContainer().appendChild(el)
  setTimeout(() => {
    el.style.animation = 'toastOut .25s ease forwards'; setTimeout(() => el.remove(), 250)
  }, duration)
  return el
}

const Toast = {
  show(message: string, type?: ToastType, duration?: number) { return createToast(message, type, duration) },
  success(message: string, duration?: number) { return createToast(message, 'success', duration) },
  error(message: string, duration?: number)   { return createToast(message, 'error', duration) },
  info(message: string, duration?: number)    { return createToast(message, 'info', duration) },
}

export default Toast
