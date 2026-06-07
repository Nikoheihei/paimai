package websocket

import (
	"log"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// Hub 管理所有直播间及其 WebSocket 客户端连接。
// 每个 roomId 对应一个 Room，出价事件按 roomId 隔离广播。
// HubEventType 表示 Hub 事件队列中的事件类型。
type HubEventType int

const (
	EventRegister HubEventType = iota
	EventUnregister
	EventDead
)

// HubEvent 是 Hub 事件队列中的一条事件，由 Hub.Run 单 goroutine 串行处理。
type HubEvent struct {
	Type   HubEventType
	Client *Client
}

// HubStats 记录 WebSocket Hub 的运行时指标。
type HubStats struct {
	TotalConnections   int64   // 当前总连接数
	TotalRooms         int64   // 当前房间数
	BroadcastCount     int64   // 累计广播次数
	BroadcastMsgs      int64   // 累计广播消息数（消息数 = 广播次数 × 房间客户端数）
	SlowBroadcasts     int64   // 耗时 > 100ms 的广播次数
	SlowClientsDropped int64   // 因 send channel 满被标记 Dead 的客户端数
	BroadcastWaitP50   float64 // 广播事件从进入 Hub 到开始发送的等待时间 P50 (ms)
	BroadcastWaitP95   float64 // 同上 P95
	BroadcastWaitP99   float64 // 同上 P99
	BroadcastCostP50   float64 // 单次广播总耗时 P50 (ms)
	BroadcastCostP95   float64 // 同上 P95
	BroadcastCostP99   float64 // 同上 P99
	SendChannelFull    int64   // send channel 满的次数
	MarshalCount       int64   // json.Marshal 次数
	MarshalTotalMs     int64   // json.Marshal 总耗时 (ms)
	// writePump 诊断指标
	WritePumpCount   int64   // 累计 WriteMessage 次数
	WritePumpTotalMs int64   // 累计 WriteMessage 耗时 (ms)
	WritePumpMsgCount int64  // 累计从 send channel 收到的消息数
	WriteCostP50     float64 // WriteMessage 耗时 P50 (ms)
	WriteCostP95     float64 // WriteMessage 耗时 P95 (ms)
	WriteCostP99     float64 // WriteMessage 耗时 P99 (ms)
	WriteLoopP50     float64 // writePump 单条消息处理总耗时 P50 (ms)
	WriteLoopP95     float64 // 同上 P95
	WriteLoopP99     float64 // 同上 P99
}

// Hub 管理所有直播间及其 WebSocket 客户端连接。
type Hub struct {
	mu     sync.RWMutex
	rooms  map[uint64]*Room
	events chan HubEvent
	stats  HubStats
	// 诊断指标采样窗口（由 sampleMu 保护，与业务锁分离）
	sampleMu       sync.Mutex
	broadcastWaits []float64 // 广播等待时间样本 (ms)
	broadcastCosts []float64 // 广播总耗时样本 (ms)
	roomCosts      []float64 // 单个 room 广播耗时样本 (ms)
}

// Room 表示一个直播间内的所有 WebSocket 客户端集合。
type Room struct {
	mu      sync.RWMutex
	clients map[*Client]bool
}

// NewHub 创建 WebSocket Hub 实例，后台不启动 goroutine（由 ServeHTTP 启动点控制）。
func NewHub() *Hub {
	return &Hub{
		rooms:          make(map[uint64]*Room),
		events:         make(chan HubEvent, 512),
		broadcastWaits: make([]float64, 0, 10000),
		broadcastCosts: make([]float64, 0, 10000),
		roomCosts:      make([]float64, 0, 10000),
	}
}

// Run 在独立的 goroutine 中处理客户端的注册与注销。
// 确保同一时刻只有这个 goroutine 修改 Hub.rooms map 结构。
// Room.clients 的修改由各自 room.mu 保护。
// 这是 Hub 的唯一 state owner，所有对 rooms map 的修改都在这里完成。
func (h *Hub) Run() {
	for e := range h.events {
		switch e.Type {
		case EventRegister:
			h.mu.Lock()
			room, ok := h.rooms[e.Client.roomID]
			if !ok {
				room = &Room{clients: make(map[*Client]bool)}
				h.rooms[e.Client.roomID] = room
				atomic.StoreInt64(&h.stats.TotalRooms, int64(len(h.rooms)))
			}
			h.mu.Unlock()

			room.mu.Lock()
			room.clients[e.Client] = true
			room.mu.Unlock()
			atomic.AddInt64(&h.stats.TotalConnections, 1)
			log.Printf("[websocket] client %d joined room %d (total conns: %d, rooms: %d)", e.Client.userID, e.Client.roomID, atomic.LoadInt64(&h.stats.TotalConnections), atomic.LoadInt64(&h.stats.TotalRooms))

		case EventUnregister:
			h.cleanupClient(e.Client, "left")

		case EventDead:
			h.cleanupClient(e.Client, "slow")
			atomic.AddInt64(&h.stats.SlowClientsDropped, 1)
		}
	}
}

// cleanupClient 从 room 中移除 client 并关闭 send channel。
// 必须在 Hub.Run goroutine 中调用。
func (h *Hub) cleanupClient(client *Client, reason string) {
	h.mu.RLock()
	room, ok := h.rooms[client.roomID]
	h.mu.RUnlock()
	if !ok {
		return
	}

	room.mu.Lock()
	_, exists := room.clients[client]
	if exists {
		delete(room.clients, client)
	}
	room.mu.Unlock()

	if exists {
		client.closeOnce.Do(func() { close(client.send) })
		atomic.AddInt64(&h.stats.TotalConnections, -1)
		log.Printf("[websocket] client %d %s room %d (total conns: %d)", client.userID, reason, client.roomID, atomic.LoadInt64(&h.stats.TotalConnections))

		room.mu.RLock()
		roomEmpty := len(room.clients) == 0
		room.mu.RUnlock()

		if roomEmpty {
			h.mu.Lock()
			// 再次确认 room 仍为空，防止并发修改
			if r, ok := h.rooms[client.roomID]; ok {
				r.mu.RLock()
				stillEmpty := len(r.clients) == 0
				r.mu.RUnlock()
				if stillEmpty {
					delete(h.rooms, client.roomID)
					atomic.StoreInt64(&h.stats.TotalRooms, int64(len(h.rooms)))
					log.Printf("[websocket] room %d closed (no clients)", client.roomID)
				}
			}
			h.mu.Unlock()
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
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()
	if !ok {
		return RoomStats{RoomID: roomID, OnlineCount: 0}
	}
	room.mu.RLock()
	count := len(room.clients)
	room.mu.RUnlock()
	return RoomStats{RoomID: roomID, OnlineCount: count}
}

func (h *Hub) Broadcast(roomID uint64, message []byte) {
	enterTime := time.Now()
	start := time.Now()
	defer func() {
		cost := time.Since(start)
		waitMs := float64(time.Since(enterTime)-cost) / float64(time.Millisecond)
		if waitMs < 0 {
			waitMs = 0
		}
		costMs := float64(cost) / float64(time.Millisecond)
		atomic.AddInt64(&h.stats.BroadcastCount, 1)
		if cost > 100*time.Millisecond {
			atomic.AddInt64(&h.stats.SlowBroadcasts, 1)
			log.Printf("[websocket] slow broadcast room=%d cost=%v wait=%v conns=%d", roomID, cost, time.Since(enterTime)-cost, atomic.LoadInt64(&h.stats.TotalConnections))
		}
		// 采样窗口（最多保留 10000 个样本）
		h.sampleMu.Lock()
		if len(h.broadcastWaits) < 10000 {
			h.broadcastWaits = append(h.broadcastWaits, waitMs)
		}
		if len(h.broadcastCosts) < 10000 {
			h.broadcastCosts = append(h.broadcastCosts, costMs)
		}
		h.sampleMu.Unlock()
	}()

	// Step 1: 用 Hub 全局锁找到 room 指针，立即释放
	h.mu.RLock()
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()
	if !ok {
		return
	}

	// Step 2: 用 room 独立锁拷贝客户端列表，立即释放
	room.mu.RLock()
	clients := make([]*Client, 0, len(room.clients))
	for c := range room.clients {
		clients = append(clients, c)
	}
	room.mu.RUnlock()

	// Step 3: 在无锁状态下逐个非阻塞发送
	roomStart := time.Now()
	msgCount := int64(len(clients))
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
				msgCount--
				atomic.AddInt64(&h.stats.SendChannelFull, 1)
				select {
				case h.events <- HubEvent{Type: EventDead, Client: client}:
				default:
					log.Printf("[websocket] dead channel full, client %d dropped", client.userID)
				}
			}
		}()
	}
	roomCostMs := float64(time.Since(roomStart)) / float64(time.Millisecond)
	h.sampleMu.Lock()
	if len(h.roomCosts) < 10000 {
		h.roomCosts = append(h.roomCosts, roomCostMs)
	}
	h.sampleMu.Unlock()
	atomic.AddInt64(&h.stats.BroadcastMsgs, msgCount)
}

// percentile 计算分位数（假设 data 已排序）
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(float64(len(sorted))*p)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

// Stats 返回 Hub 运行时指标的快照（包含诊断分位数）。
func (h *Hub) Stats() HubStats {
	h.sampleMu.Lock()
	waits := make([]float64, len(h.broadcastWaits))
	costs := make([]float64, len(h.broadcastCosts))
	rooms := make([]float64, len(h.roomCosts))
	copy(waits, h.broadcastWaits)
	copy(costs, h.broadcastCosts)
	copy(rooms, h.roomCosts)
	// 清空采样窗口，避免重复统计
	h.broadcastWaits = h.broadcastWaits[:0]
	h.broadcastCosts = h.broadcastCosts[:0]
	h.roomCosts = h.roomCosts[:0]
	h.sampleMu.Unlock()

	sort.Float64s(waits)
	sort.Float64s(costs)
	sort.Float64s(rooms)

	wpCount, wpTotalMs, wpMsgCount, wpCostP50, wpCostP95, wpCostP99, wpLoopP50, wpLoopP95, wpLoopP99 := GetWritePumpStats()

	return HubStats{
		TotalConnections:   atomic.LoadInt64(&h.stats.TotalConnections),
		TotalRooms:         atomic.LoadInt64(&h.stats.TotalRooms),
		BroadcastCount:     atomic.LoadInt64(&h.stats.BroadcastCount),
		BroadcastMsgs:      atomic.LoadInt64(&h.stats.BroadcastMsgs),
		SlowBroadcasts:     atomic.LoadInt64(&h.stats.SlowBroadcasts),
		SlowClientsDropped: atomic.LoadInt64(&h.stats.SlowClientsDropped),
		BroadcastWaitP50:   percentile(waits, 0.50),
		BroadcastWaitP95:   percentile(waits, 0.95),
		BroadcastWaitP99:   percentile(waits, 0.99),
		BroadcastCostP50:   percentile(costs, 0.50),
		BroadcastCostP95:   percentile(costs, 0.95),
		BroadcastCostP99:   percentile(costs, 0.99),
		SendChannelFull:    atomic.LoadInt64(&h.stats.SendChannelFull),
		MarshalCount:       atomic.LoadInt64(&h.stats.MarshalCount),
		MarshalTotalMs:     atomic.LoadInt64(&h.stats.MarshalTotalMs),
		WritePumpCount:     wpCount,
		WritePumpTotalMs:   wpTotalMs,
		WritePumpMsgCount:  wpMsgCount,
		WriteCostP50:       wpCostP50,
		WriteCostP95:       wpCostP95,
		WriteCostP99:       wpCostP99,
		WriteLoopP50:       wpLoopP50,
		WriteLoopP95:       wpLoopP95,
		WriteLoopP99:       wpLoopP99,
	}
}

// EventQueueLen 返回事件队列当前长度。
func (h *Hub) EventQueueLen() int {
	return len(h.events)
}

func (h *Hub) BroadcastAll(message []byte) {
	// Step 1: 收集所有 room 指针
	h.mu.RLock()
	rooms := make([]*Room, 0, len(h.rooms))
	for _, room := range h.rooms {
		rooms = append(rooms, room)
	}
	h.mu.RUnlock()

	// Step 2: 逐个 room 加锁拷贝 clients
	clients := make([]*Client, 0)
	for _, room := range rooms {
		room.mu.RLock()
		for client := range room.clients {
			clients = append(clients, client)
		}
		room.mu.RUnlock()
	}

	// Step 3: 无锁发送
	for _, client := range clients {
		func() {
			defer func() {
				if r := recover(); r != nil {
				}
			}()
			select {
			case client.send <- message:
			default:
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
	room, ok := h.rooms[roomID]
	h.mu.RUnlock()
	if !ok {
		return 0
	}
	room.mu.RLock()
	count := len(room.clients)
	room.mu.RUnlock()
	return count
}
