/**
 * Skeleton — 加载骨架屏组件
 *
 * 用于数据加载时的占位，减少用户感知等待时间。
 */

export function SkeletonText({ width = '100%', height = 14 }: { width?: string; height?: number }) {
  return <div className="skeleton skeleton-text" style={{ width, height }} />
}

export function SkeletonTitle({ width = '60%' }: { width?: string }) {
  return <div className="skeleton skeleton-title" style={{ width }} />
}

export function SkeletonCircle({ size = 48 }: { size?: number }) {
  return <div className="skeleton skeleton-circle" style={{ width: size, height: size }} />
}

export function SkeletonCard({ height = 180 }: { height?: number }) {
  return <div className="skeleton skeleton-card" style={{ height }} />
}

export function RoomListSkeleton() {
  return (
    <div style={{ padding: '4px 0' }}>
      <SkeletonTitle width="40%" />
      <SkeletonCard height={160} />
      <SkeletonCard height={160} />
      <SkeletonCard height={160} />
    </div>
  )
}

export function AuctionPanelSkeleton() {
  return (
    <div style={{ padding: '20px' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginBottom: 16 }}>
        <SkeletonCircle size={54} />
        <div style={{ flex: 1 }}>
          <SkeletonTitle width="70%" />
          <SkeletonText width="40%" />
        </div>
      </div>
      <SkeletonCard height={80} />
      <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
        <SkeletonText width="30%" />
        <SkeletonText width="30%" />
        <SkeletonText width="30%" />
      </div>
    </div>
  )
}
