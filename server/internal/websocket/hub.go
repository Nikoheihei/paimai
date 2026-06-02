package websocket

import (
	"log"
	"sync"
)

// Hub 管理所有直播间及其 WebSocket 客户端连接。
// 每个 roomId 对应一个 Room，出价事件按 roomId 隔离广播。
type Hub struct {
	mu    sync.RWMutex
	rooms map[uint64]*Room

	// register / unregister 通道用于在 Hub goroutine 中安全地增删客户端。
	register   chan *Client
	unregister chan *Client
}

// Room 表示一个直播间内的所有 WebSocket 客户端集合。
type Room struct {
	clients map[*Client]bool
}

// NewHub 创建 WebSocket Hub 实例，后台不启动 goroutine（由 ServeHTTP 启动点控制）。
func NewHub() *Hub {
	return &Hub{
		rooms:      make(map[uint64]*Room),
		register:   make(chan *Client, 256),
		unregister: make(chan *Client, 256),
	}
}

// Run 在独立的 goroutine 中处理客户端的注册与注销。
// 确保同一时刻只有这个 goroutine 修改 Hub.clients 和 Room.clients 结构。
func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			room, ok := h.rooms[client.roomID]
			if !ok {
				room = &Room{clients: make(map[*Client]bool)}
				h.rooms[client.roomID] = room
			}
			room.clients[client] = true
			h.mu.Unlock()
			log.Printf("[websocket] client %d joined room %d", client.userID, client.roomID)

		case client := <-h.unregister:
			h.mu.Lock()
			if room, ok := h.rooms[client.roomID]; ok {
				if _, exists := room.clients[client]; exists {
					delete(room.clients, client)
					close(client.send)
					log.Printf("[websocket] client %d left room %d", client.userID, client.roomID)
					if len(room.clients) == 0 {
						delete(h.rooms, client.roomID)
						log.Printf("[websocket] room %d closed (no clients)", client.roomID)
					}
				}
			}
			h.mu.Unlock()
		}
	}
}

// Broadcast 向指定房间内的所有客户端发送消息。
// Register 将客户端注册到 Hub，由 Hub.Run 的 goroutine 处理。
func (h *Hub) Register(client *Client) {
	h.register <- client
}

// Unregister 将客户端从 Hub 注销。
func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}
func (h *Hub) Broadcast(roomID uint64, message []byte) {
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()

	if !ok {
		return
	}

	// 遍历客户端发送消息，不阻塞慢客户端
	for client := range room.clients {
		select {
		case client.send <- message:
		default:
			// 客户端 send buffer 满了，说明写入过慢，断开连接
			h.mu.Lock()
			delete(room.clients, client)
			close(client.send)
			h.mu.Unlock()
			log.Printf("[websocket] client %d dropped (send buffer full)", client.userID)
		}
	}
}

// GetRoomClientCount 返回指定房间内的在线客户端数量。
func (h *Hub) GetRoomClientCount(roomID uint64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if room, ok := h.rooms[roomID]; ok {
		return len(room.clients)
	}
	return 0
}
