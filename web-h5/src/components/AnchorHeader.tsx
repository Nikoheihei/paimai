/**
 * AnchorHeader — 浮动主播信息卡（毛玻璃风格）
 *
 * 仿抖音直播间主播卡片：
 * - 圆角头像 + 在线状态 + 关注按钮
 * - 毛玻璃背景，浮在视频上方
 */

import { useState } from 'react'
import type { UserInfo } from '../shared/types'

type Props = {
  info: UserInfo
  viewerCount?: number
  onFollow?: () => void
  onMoreRooms?: () => void
  isPlaceholder?: boolean
}

export default function AnchorHeader({ info, viewerCount=0, onFollow, onMoreRooms, isPlaceholder=false }: Props) {
  const [followed, setFollowed] = useState(false)

  const handleFollow = () => {
    setFollowed(f=>!f)
    onFollow?.()
  }

  return (
    <div className={`anchor-header ${isPlaceholder ? 'placeholder' : ''}`}>
      {/* 头像 */}
      <div className="anchor-avatar">
        {info.avatarUrl ? (
          <img src={info.avatarUrl} alt={info.nickname} />
        ) : (
          <span>{(info.nickname||'U').charAt(0).toUpperCase()}</span>
        )}
      </div>

      {/* 信息区 */}
      <div className="anchor-info">
        <div className="anchor-name">{info.nickname || `用户${info.userId}`}</div>
        <div className="anchor-viewers">{viewerCount.toLocaleString('zh-CN')} 在线</div>
      </div>

      {/* 关注按钮 */}
      <button className={`anchor-follow-btn ${followed?'followed':''}`} onClick={handleFollow}>
        {followed ? '已关注' : '+关注'}
      </button>
      {onMoreRooms && (
        <button className="anchor-more-btn" onClick={onMoreRooms} title="更多直播间">
          ...
        </button>
      )}
    </div>
  )
}
