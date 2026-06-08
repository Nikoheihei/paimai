import { createHashRouter, Navigate } from 'react-router-dom'
import AppLayout from '../AppLayout'
import RoomListPage from '../pages/RoomListPage'
import LiveRoomPage from '../pages/LiveRoomPage'
import AuctionDetailPage from '../pages/AuctionDetailPage'
import OrderPage from '../pages/OrderPage'
import AddressListPage from '../pages/AddressListPage'
import LoginPage from '../pages/LoginPage'
import { useAuthStore } from '../store/useAuthStore'

// A wrapper to protect routes
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthed = useAuthStore(state => state.isAuthed)
  if (!isAuthed) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

export const router = createHashRouter([
  {
    path: '/login',
    element: <LoginPage />
  },
  {
    path: '/',
    element: (
      <ProtectedRoute>
        <AppLayout />
      </ProtectedRoute>
    ),
    children: [
      {
        index: true,
        element: <RoomListPage />
      },
      {
        path: 'orders',
        element: <OrderPage />
      },
      {
        path: 'address',
        element: <AddressListPage />
      }
    ]
  },
  {
    path: '/rooms/:roomId',
    element: (
      <ProtectedRoute>
        <LiveRoomPage />
      </ProtectedRoute>
    )
  },
  {
    path: '/auctions/:auctionId',
    element: (
      <ProtectedRoute>
        <AuctionDetailPage />
      </ProtectedRoute>
    )
  }
])
