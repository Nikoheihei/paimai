/**
 * VideoPlayer — 沉浸式直播视频区域
 *
 * 仿抖音/快手直播间风格：
 * - 全屏渐变背景（模拟直播间灯光氛围）
 * - 左上 LIVE 角标 + 右上观看人数
 * - 底部房间标题栏
 * - 中央商品展示区
 */

import { useState, useEffect } from 'react'

type Props = {
  coverUrl?: string
  videoUrl?: string
  isLive: boolean
  viewerCount?: number
  roomTitle?: string
}

export default function VideoPlayer({ coverUrl, videoUrl, isLive, viewerCount = 0, roomTitle }: Props) {
  const [playing, setPlaying] = useState(false)
  // 模拟动态光效
  const [glowPhase, setGlowPhase] = useState(0)

  useEffect(() => {
    if (!playing) return
    const t = setInterval(() => setGlowPhase(p => (p + 1) % 360), 50)
    return () => clearInterval(t)
  }, [playing])

  return (
    <div className="video-player">
      {/* 视频画面 / 渐变背景 */}
      <div className="vp-container" onClick={() => setPlaying(p => !p)}>
        {videoUrl ? (
          <video
            src={videoUrl}
            autoPlay
            muted
            loop
            playsInline
            controls={false}
            style={{ width: '100%', height: '100%', objectFit: 'cover' }}
          />
        ) : coverUrl ? (
          <img src={coverUrl} alt={roomTitle || '直播'} style={{ width:'100%',height:'100%',objectFit:'cover' }} />
        ) : (
          <div className="vp-showcase">
            {/* 动态光晕效果 */}
            <div style={{
              position: 'absolute', width: 200, height: 200, borderRadius: '50%',
              background: `radial-gradient(circle, rgba(254,44,85,.15) 0%, transparent 70%)`,
              transform: `translateX(${Math.sin(glowPhase * Math.PI / 180) * 30}px)`,
              transition: 'transform .5s linear',
            }} />
            <div className="vp-showcase-text">直播拍卖中</div>
          </div>
        )}
      </div>

      {/* 观看人数 */}
      {isLive && viewerCount > 0 && (
        <div className="vp-viewers">
          <span className="vp-eye-icon">&#128065;</span>
          {viewerCount >= 10000 ? `${(viewerCount/10000).toFixed(1)}万` : viewerCount}
        </div>
      )}
    </div>
  )
}
