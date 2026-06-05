/**
 * EmptyState — 空状态占位组件
 */

import type { ReactNode } from 'react'

type Props = {
  icon?: ReactNode
  title: string
  description?: string
  actionLabel?: string
  onAction?: () => void
}

export default function EmptyState({ icon, title, description, actionLabel, onAction }: Props) {
  return (
    <div className="empty-state">
      {icon && <div className="empty-icon">{icon}</div>}
      <p className="empty-title">{title}</p>
      {description && <p className="empty-desc">{description}</p>}
      {actionLabel && onAction && (
        <button className="empty-action-btn" onClick={onAction}>
          {actionLabel}
        </button>
      )}
    </div>
  )
}
