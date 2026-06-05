/**
 * Toast — 全局轻提示
 * 用法：
 *   import Toast from './components/Toast'
 *   Toast.show('操作成功')
 *   Toast.show('失败', 'error')
 *   Toast.show('警告', 'warning', 3000)
 */

let containerEl: HTMLDivElement | null = null

function getContainer(): HTMLDivElement {
  if (!containerEl) {
    containerEl = document.createElement('div')
    containerEl.id = 'toast-container'
    containerEl.setAttribute(
      'style',
      'position:fixed;top:20px;left:50%;transform:translateX(-50%);z-index:99999;display:flex;flex-direction:column;gap:8px;pointer-events:none;',
    )
    document.body.appendChild(containerEl)
  }
  return containerEl
}

type ToastType = 'success' | 'error' | 'info' | 'warning'

function createToast(message: string, type: ToastType = 'info', duration = 2000): HTMLDivElement {
  const el = document.createElement('div')
  const iconMap: Record<ToastType, string> = {
    success: '&#10003;',
    error: '&#10007;',
    info: '&#8505;',
    warning: '&#9888;',
  }
  const colorMap: Record<ToastType, string> = {
    success: '#4caf50',
    error: '#f44336',
    info: '#2196f3',
    warning: '#ff9800',
  }

  el.innerHTML = `<span style="margin-right:6px">${iconMap[type]}</span>${message}`
  el.setAttribute(
    'style',
    `background:${colorMap[type]};color:#fff;padding:10px 20px;border-radius:10px;font-size:14px;
     font-weight:600;box-shadow:0 4px 12px rgba(0,0,0,.3);animation:toastIn .25s ease;pointer-events:auto;
     max-width:360px;text-align:center;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;`,
  )

  // 注入动画样式（只做一次）
  if (!document.getElementById('toast-styles')) {
    const style = document.createElement('style')
    style.id = 'toast-styles'
    style.textContent = `
      @keyframes toastIn { from{opacity:0;transform:translateY(-12px)} to{opacity:1;transform:translateY(0)} }
      @keyframes toastOut { from{opacity:1} to{opacity:0;transform:translateY(-8px)} }
    `
    document.head.appendChild(style)
  }

  getContainer().appendChild(el)

  // 自动消失
  setTimeout(() => {
    el.style.animation = 'toastOut .25s ease forwards'
    setTimeout(() => el.remove(), 250)
  }, duration)

  return el
}

const Toast = {
  show(message: string, type: ToastType = 'info', duration?: number) {
    return createToast(message, type, duration)
  },
  success(message: string, duration?: number) { return createToast(message, 'success', duration) },
  error(message: string, duration?: number)   { return createToast(message, 'error', duration) },
  info(message: string, duration?: number)    { return createToast(message, 'info', duration) },
  warning(message: string, duration?: number) { return createToast(message, 'warning', duration) },
}

export default Toast
