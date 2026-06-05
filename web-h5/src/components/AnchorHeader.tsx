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
    <div className="anchor-header">
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
        <div className="anchor-meta-row">
          {isPlaceholder && (
            <>
              <span className="anchor-online-dot" style={{background:'var(--gold)'}} />
              <span style={{color:'var(--gold)'}}>&#9888; 主播信息加载中</span>
            </>
          )}
          {!isPlaceholder && (
            <>
              <span className="anchor-online-dot" />
              <span>在线</span>
              {viewerCount > 0 && (
                <span>{'\u{1F441}'} {viewerCount>=10000?`${(viewerCount/10000).toFixed(1)}万`:String(viewerCount)} 人在看</span>
              )}
            </>
          )}
        </div>
      </div>

      {/* 关注按钮 */}
      <button className={`anchor-follow-btn ${followed?'followed':''}`} onClick={handleFollow}>
        {followed ? '已关注' : '+关注'}
      </button>

      {/* 更多直播 */}
      {onMoreRooms && (
        <button className="anchor-more-link" onClick={onMoreRooms}>
          更多 &rsaquo;
        </button>
      )}
    </div>
  )
}
