package websocket

import (
	"log"
	"sync"
)

// Hub 管理所有直播间及其 WebSocket 客户端连接。
// 每个 roomId 对应一个 Room，出价事件按 roomId 隔离广播。
// HubEventType 表示 Hub 事件队列中的事件类型。
type HubEventType int

const (
	EventRegister   HubEventType = iota
	EventUnregister
	EventDead
)

// HubEvent 是 Hub 事件队列中的一条事件，由 Hub.Run 单 goroutine 串行处理。
type HubEvent struct {
	Type   HubEventType
	Client *Client
}

// Hub 管理所有直播间及其 WebSocket 客户端连接。
type Hub struct {
	mu     sync.RWMutex
	rooms  map[uint64]*Room
	events chan HubEvent
}

// Room 表示一个直播间内的所有 WebSocket 客户端集合。
type Room struct {
	clients map[*Client]bool
}

// NewHub 创建 WebSocket Hub 实例，后台不启动 goroutine（由 ServeHTTP 启动点控制）。
func NewHub() *Hub {
	return &Hub{
		rooms:  make(map[uint64]*Room),
		events: make(chan HubEvent, 512),
	}
}

// Run 在独立的 goroutine 中处理客户端的注册与注销。
// 确保同一时刻只有这个 goroutine 修改 Hub.clients 和 Room.clients 结构。
// Run 在独立的 goroutine 中串行消费事件队列。
// 这是 Hub 的唯一 state owner，所有对 rooms 和 client.send 的修改都在这里完成。
func (h *Hub) Run() {
	for e := range h.events {
		switch e.Type {
		case EventRegister:
			h.mu.Lock()
			room, ok := h.rooms[e.Client.roomID]
			if !ok {
				room = &Room{clients: make(map[*Client]bool)}
				h.rooms[e.Client.roomID] = room
			}
			room.clients[e.Client] = true
			h.mu.Unlock()
			log.Printf("[websocket] client %d joined room %d", e.Client.userID, e.Client.roomID)

		case EventUnregister:
			h.cleanupClient(e.Client, "left")

		case EventDead:
			h.cleanupClient(e.Client, "slow")
		}
	}
}

// cleanupClient 从 room 中移除 client 并关闭 send channel。
// 必须在 Hub.Run goroutine 中调用。
func (h *Hub) cleanupClient(client *Client, reason string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if room, ok := h.rooms[client.roomID]; ok {
		if _, exists := room.clients[client]; exists {
			delete(room.clients, client)
			client.closeOnce.Do(func() { close(client.send) })
			log.Printf("[websocket] client %d %s room %d", client.userID, reason, client.roomID)
			if len(room.clients) == 0 {
				delete(h.rooms, client.roomID)
				log.Printf("[websocket] room %d closed (no clients)", client.roomID)
			}
		}
	}
}

// Register 发送注册事件到 Hub 事件队列，由 Hub.Run 异步处理。
func (h *Hub) Register(client *Client) {
	h.events <- HubEvent{Type: EventRegister, Client: client}
}

// Unregister 发送注销事件到 Hub 事件队列，由 Hub.Run 异步处理。
func (h *Hub) Unregister(client *Client) {
	h.events <- HubEvent{Type: EventUnregister, Client: client}
}

// RoomStats 返回指定房间的实时连接统计。
type RoomStats struct {
	RoomID      uint64 `json:"roomId"`
	OnlineCount int    `json:"onlineCount"`
}

// GetRoomStats 查询指定房间的 WS 连接数。
func (h *Hub) GetRoomStats(roomID uint64) RoomStats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	room, ok := h.rooms[roomID]
	if !ok {
		return RoomStats{RoomID: roomID, OnlineCount: 0}
	}
	return RoomStats{RoomID: roomID, OnlineCount: len(room.clients)}
}

func (h *Hub) Broadcast(roomID uint64, message []byte) {
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	if !ok {
		h.mu.RUnlock()
		return
	}
	// 在 RLock 保护下拷贝客户端列表，避免遍历时 map 被修改
	clients := make([]*Client, 0, len(room.clients))
	for c := range room.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	// 遍历拷贝后的列表发送消息
	// 注意：遍历期间其他 goroutine 可能通过 Hub.Run 关闭 client.send，
	// 用 recover 捕获 send on closed channel panic 安全忽略。
	for _, client := range clients {
		func() {
			defer func() {
				if r := recover(); r != nil {
					// client 可能在遍历开始后被 Hub.Run 关闭，忽略即可
				}
			}()
			select {
			case client.send <- message:
			default:
				// 慢客户端：仅发送到 dead channel，不处理生命周期
				// Hub.Run 会统一 delete + close(send)
				select {
				case h.events <- HubEvent{Type: EventDead, Client: client}:
				default:
					log.Printf("[websocket] dead channel full, client %d dropped", client.userID)
				}
			}
		}()
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
