import { useEffect } from 'react'
import { RouterProvider } from 'react-router-dom'
import { router } from './router'
import { syncServerTime } from './utils/serverTime'
import { useAuthStore } from './store/useAuthStore'
import './App.css'

function App() {
  const isAuthed = useAuthStore(state => state.isAuthed)
  const fetchUserInfo = useAuthStore(state => state.fetchUserInfo)

  useEffect(() => {
    syncServerTime()
  }, [])

  useEffect(() => {
    if (isAuthed) {
      fetchUserInfo()
    }
  }, [isAuthed, fetchUserInfo])

  return <RouterProvider router={router} />
}

export default App
