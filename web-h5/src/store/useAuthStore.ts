import { create } from 'zustand'
import { getToken, setToken as apiSetToken, clearToken as apiClearToken, getMe, type MeResult } from '../api/client'
import { parseUserIdFromToken } from '../utils/auth'

type AuthState = {
  isAuthed: boolean
  userId: number
  userInfo: MeResult | null
  login: (token: string, userId: number) => void
  logout: () => void
  fetchUserInfo: () => Promise<void>
}

export const useAuthStore = create<AuthState>((set, get) => ({
  isAuthed: !!getToken(),
  userId: parseUserIdFromToken(),
  userInfo: null,
  login: (token: string, userId: number) => {
    apiSetToken(token)
    set({ isAuthed: true, userId })
  },
  logout: () => {
    apiClearToken()
    set({ isAuthed: false, userId: 0, userInfo: null })
  },
  fetchUserInfo: async () => {
    try {
      const info = await getMe()
      set({ userInfo: info })
    } catch {
      // If fetching fails (e.g. token expired), we might want to log out
      get().logout()
    }
  }
}))
