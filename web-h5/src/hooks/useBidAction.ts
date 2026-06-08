import { useState, useRef, useCallback } from 'react'
import { placeBid, type BidResult } from '../api/client'
import Toast from '../components/Toast'

export function useBidAction(auctionId: number, userId: number, onSuccess?: () => void) {
  const [bidStatus, setBidStatus] = useState<'idle' | 'sending' | 'ok' | 'fail'>('idle')
  const idemCounter = useRef(0)

  const handleBid = useCallback(async (amountCents: number) => {
    if (!auctionId || bidStatus === 'sending') return
    setBidStatus('sending')
    const idem = `bid-${auctionId}-${userId}-${++idemCounter.current}-${Date.now()}`
    try {
      const res: BidResult = await placeBid(auctionId, userId, amountCents, idem)
      if (res.accepted) {
        setBidStatus('ok')
        Toast.success('出价成功！')
        onSuccess?.()
      } else {
        setBidStatus('fail')
        Toast.error(res.tooFrequent ? '出价太频繁' : '出价被拒绝')
      }
    } catch (err: any) {
      setBidStatus('fail')
      Toast.error(err.message || '出价失败')
    }
  }, [auctionId, userId, bidStatus, onSuccess])

  return { bidStatus, handleBid }
}
