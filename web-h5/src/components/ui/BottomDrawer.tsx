import { useEffect, useState } from 'react'
import { createPortal } from 'react-dom'
import './BottomDrawer.css'

type Props = {
  open: boolean
  onClose: () => void
  title?: string
  children: React.ReactNode
}

export default function BottomDrawer({ open, onClose, title, children }: Props) {
  const [mounted, setMounted] = useState(false)
  const [visible, setVisible] = useState(false)

  useEffect(() => {
    if (open) {
      setMounted(true)
      // Slight delay to allow DOM render before applying visible class for transition
      requestAnimationFrame(() => setVisible(true))
    } else {
      setVisible(false)
      const timer = setTimeout(() => setMounted(false), 300) // matches transition duration
      return () => clearTimeout(timer)
    }
  }, [open])

  if (!mounted) return null

  return createPortal(
    <div className={`drawer-overlay ${visible ? 'visible' : ''}`} onClick={onClose}>
      <div 
        className={`drawer-content ${visible ? 'visible' : ''}`} 
        onClick={e => e.stopPropagation()}
      >
        <div className="drawer-handle" />
        {title && (
          <div className="drawer-header">
            <h3>{title}</h3>
            <button className="drawer-close" onClick={onClose}>×</button>
          </div>
        )}
        <div className="drawer-body">
          {children}
        </div>
      </div>
    </div>,
    document.body
  )
}
