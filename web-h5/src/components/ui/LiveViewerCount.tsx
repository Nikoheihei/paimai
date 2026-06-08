import { useState, useEffect } from 'react'

export default function LiveViewerCount() {
  const [viewerCount, setViewerCount] = useState(128 + Math.floor(Math.random() * 500))

  useEffect(() => {
    const t = setInterval(() => {
      setViewerCount(v => v + Math.floor(Math.random() * 7) - 2)
    }, 5000)
    return () => clearInterval(t)
  }, [])

  return <>{viewerCount}</>
}
