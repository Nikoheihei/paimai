/**
 * ConfirmModal — 通用确认弹窗
 */

import { useEffect, useCallback } from 'react'
import type { ReactNode } from 'react'

type Props = {
  open: boolean
  title: string
  message?: ReactNode
  confirmText?: string
  cancelText?: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}

export default function ConfirmModal({
  open,
  title,
  message,
  confirmText = '确认',
  cancelText = '取消',
  danger = false,
  onConfirm,
  onCancel,
}: Props) {
  // ESC 关闭
  const handleKeyDown = useCallback(
    (e: KeyboardEvent) => {
      if (e.key === 'Escape' && open) onCancel()
    },
    [open, onCancel],
  )

  useEffect(() => {
    document.addEventListener('keydown', handleKeyDown)
    return () => document.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])

  if (!open) return null

  return (
    <div className="modal-overlay" onClick={(e) => { if (e.target === e.currentTarget) onCancel() }}>
      <div className="modal-content">
        <h3 className="modal-title">{title}</h3>
        {message && <div className="modal-message">{message}</div>}
        <div className="modal-actions">
          <button className="modal-btn modal-btn-cancel" onClick={onCancel}>
            {cancelText}
          </button>
          <button
            className={`modal-btn modal-btn-confirm ${danger ? 'btn-danger' : ''}`}
            onClick={onConfirm}
          >
            {confirmText}
          </button>
        </div>
      </div>
    </div>
  )
}
