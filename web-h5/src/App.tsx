/**
 * App — 根组件
 * 简易路由：通过查询参数 ?roomId=1&userId=1 进入直播间
 */

import { useMemo } from 'react'
import RoomPage from './pages/RoomPage'
import './App.css'

function App() {
  const params = useMemo(() => {
    const sp = new URLSearchParams(window.location.search)
    return {
      roomId: parseInt(sp.get('roomId') || '1', 10),
      userId: parseInt(sp.get('userId') || '1', 10),
    }
  }, [])

  return (
    <div className="app">
      <RoomPage roomId={params.roomId} userId={params.userId} />
    </div>
  )
}

export default App
