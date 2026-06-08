import { useState, useEffect, memo } from 'react'
import BarrageLayer, { randomBarrage, type BarrageItem } from '../BarrageLayer'

type Props = {
  active: boolean
  userMessages: BarrageItem[]
}

const BarrageSimLayer = memo(({ active, userMessages }: Props) => {
  const [barrages, setBarrages] = useState<BarrageItem[]>([])

  useEffect(() => {
    if (!active) return
    const barrageTimer = setInterval(() => {
      setBarrages(prev => [...prev, randomBarrage(prev.length)])
    }, 3000 + Math.floor(Math.random() * 5000))
    return () => clearInterval(barrageTimer)
  }, [active])

  // Merge user messages and simulated messages
  const allMessages = [...barrages, ...userMessages].sort((a, b) => a.id.localeCompare(b.id))

  return <BarrageLayer messages={allMessages} />
})

export default BarrageSimLayer
