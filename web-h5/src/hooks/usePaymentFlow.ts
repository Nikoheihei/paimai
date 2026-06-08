import { useState, useCallback, useRef, useEffect } from 'react'
import { listBuyerOrders, payBuyerOrder, type Order } from '../api/client'
import { remainingPaymentSeconds, paymentDeadlineMs, PAYMENT_WINDOW_SECONDS } from '../utils/paymentDeadline'
import Toast from '../components/Toast'

function sleep(ms: number): Promise<void> {
  return new Promise(resolve => window.setTimeout(resolve, ms))
}

export function usePaymentFlow(auctionId: number, currentUserId: number, initialWinnerUserId: number | null) {
  const [showPayDrawer, setShowPayDrawer] = useState(false)
  const [payCountdown, setPayCountdown] = useState(PAYMENT_WINDOW_SECONDS)
  const [payOrder, setPayOrder] = useState<Order | null>(null)
  const [payLoading, setPayLoading] = useState(false)
  const [paidAuctionIds, setPaidAuctionIds] = useState<number[]>([])
  
  const payTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const payTriggeredRef = useRef<number | null>(null)
  const payDeadlineRef = useRef(0)

  const stopPayCountdown = useCallback(() => {
    if (payTimerRef.current) {
      clearInterval(payTimerRef.current)
      payTimerRef.current = null
    }
    payDeadlineRef.current = 0
    setPayCountdown(0)
  }, [])

  const startPayCountdown = useCallback((deadlineMs: number) => {
    stopPayCountdown()
    payDeadlineRef.current = deadlineMs

    const tick = () => {
      const remaining = remainingPaymentSeconds(payDeadlineRef.current)
      setPayCountdown(remaining)
      if (remaining <= 0) {
        if (payTimerRef.current) clearInterval(payTimerRef.current)
        payTimerRef.current = null
        payDeadlineRef.current = 0
      }
    }
    tick()
    if (remainingPaymentSeconds(deadlineMs) > 0) {
      payTimerRef.current = setInterval(tick, 1000)
    }
  }, [stopPayCountdown])

  const loadPaymentOrder = useCallback(async (targetAuctionId: number): Promise<Order | null> => {
    const orders = await listBuyerOrders(targetAuctionId)
    return orders.find(order => order.auctionId === targetAuctionId) || orders[0] || null
  }, [])

  const waitPaymentOrder = useCallback(async (targetAuctionId: number): Promise<Order | null> => {
    for (let attempt = 0; attempt < 20; attempt++) {
      const order = await loadPaymentOrder(targetAuctionId)
      if (order) return order
      await sleep(300)
    }
    return null
  }, [loadPaymentOrder])

  // 成交后触发
  const triggerPaymentFlow = useCallback((soldAuctionId: number) => {
    if (paidAuctionIds.includes(soldAuctionId)) return
    if (payTriggeredRef.current === soldAuctionId && payOrder?.auctionId === soldAuctionId) return

    waitPaymentOrder(soldAuctionId)
      .then(order => {
        if (!order) {
          Toast.error('订单同步中，请稍后到我的订单查看')
          return
        }
        if (order.status === 'paid') {
          setPayOrder(order)
          payTriggeredRef.current = soldAuctionId
          setPaidAuctionIds(prev => prev.includes(soldAuctionId) ? prev : [...prev, soldAuctionId])
          stopPayCountdown()
          return
        }
        if (order.status === 'closed') {
          setPayOrder(order)
          payTriggeredRef.current = soldAuctionId
          stopPayCountdown()
          Toast.error('支付超时，订单已关闭')
          return
        }
        setPayOrder(order)
        payTriggeredRef.current = soldAuctionId
        startPayCountdown(paymentDeadlineMs(order.createdAt))
        setShowPayDrawer(true)
      })
      .catch((err: any) => {
        Toast.error(err.message || '加载订单失败')
      })
  }, [paidAuctionIds, payOrder?.auctionId, waitPaymentOrder, stopPayCountdown, startPayCountdown])

  useEffect(() => () => stopPayCountdown(), [stopPayCountdown])

  const openPayDrawer = useCallback(async () => {
    if (paidAuctionIds.includes(auctionId)) return
    try {
      const order = payOrder?.auctionId === auctionId ? payOrder : await waitPaymentOrder(auctionId)
      if (!order) {
        Toast.error('订单同步中，请稍后到我的订单查看')
        return
      }
      if (order.status === 'paid') {
        setPayOrder(order)
        setPaidAuctionIds(prev => prev.includes(auctionId) ? prev : [...prev, auctionId])
        Toast.success('支付成功！')
        return
      }
      if (order.status === 'closed') {
        setPayOrder(order)
        stopPayCountdown()
        Toast.error('支付超时，订单已关闭')
        return
      }
      setPayOrder(order)
      startPayCountdown(paymentDeadlineMs(order.createdAt))
      setShowPayDrawer(true)
    } catch (err: any) {
      Toast.error(err.message || '加载订单失败')
    }
  }, [auctionId, paidAuctionIds, payOrder, waitPaymentOrder, startPayCountdown, stopPayCountdown])

  const closePayDrawer = useCallback(() => setShowPayDrawer(false), [])

  const executePay = async (selectedAddressId: number | null, addressSnapshot: string, onPaid?: () => void) => {
    if (!selectedAddressId) {
      Toast.error('请先选择收货地址')
      return
    }
    if (payCountdown === 0) {
      Toast.error('支付已超时')
      return
    }
    setPayLoading(true)
    try {
      const order = payOrder?.auctionId === auctionId ? payOrder : await waitPaymentOrder(auctionId)
      if (!order) {
        Toast.error('订单同步中，请稍后到我的订单查看')
        setPayLoading(false)
        return
      }
      if (order.status === 'paid') {
        setPayOrder(order)
        setPaidAuctionIds(prev => prev.includes(auctionId) ? prev : [...prev, auctionId])
        Toast.success('支付成功！')
        setShowPayDrawer(false)
        stopPayCountdown()
        onPaid?.()
        return
      }
      const paidOrder = await payBuyerOrder(order.id, selectedAddressId, addressSnapshot)
      setPayOrder(paidOrder)
      setPaidAuctionIds(prev => prev.includes(auctionId) ? prev : [...prev, auctionId])
      Toast.success('支付成功！')
      setShowPayDrawer(false)
      stopPayCountdown()
      payTriggeredRef.current = auctionId
      window.dispatchEvent(new CustomEvent('order:refresh'))
      onPaid?.()
    } catch (err: any) {
      Toast.error(err.message || '支付失败')
      if (String(err.message || '').includes('timeout')) {
        setShowPayDrawer(false)
        stopPayCountdown()
        window.dispatchEvent(new CustomEvent('order:refresh'))
      }
    } finally {
      setPayLoading(false)
    }
  }

  const handleWsOrderEvent = useCallback((type: string, eventAuctionId?: number, eventBuyerId?: number, onStateChanged?: () => void) => {
    if (eventAuctionId && eventAuctionId !== auctionId) return
    onStateChanged?.()
    if (type === 'order.paid' && eventBuyerId === currentUserId) {
      setPaidAuctionIds(prev => prev.includes(auctionId) ? prev : [...prev, auctionId])
      setShowPayDrawer(false)
      stopPayCountdown()
    }
    if ((type === 'order.closed' || type === 'auction.payment_timeout') && (initialWinnerUserId === currentUserId || eventBuyerId === currentUserId)) {
      setShowPayDrawer(false)
      stopPayCountdown()
      Toast.error('支付超时，订单已关闭')
      window.dispatchEvent(new CustomEvent('order:refresh'))
    }
  }, [auctionId, currentUserId, initialWinnerUserId, stopPayCountdown])

  return {
    showPayDrawer,
    payCountdown,
    payOrder,
    payLoading,
    paidAuctionIds,
    triggerPaymentFlow,
    openPayDrawer,
    closePayDrawer,
    executePay,
    handleWsOrderEvent
  }
}
