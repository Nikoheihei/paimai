package websocket

import (
	"encoding/json"
	"log"
	"math"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// 写入 WebSocket 的超时时间，防止慢客户端拖慢整个 Hub。
	writeWait = 10 * time.Second

	// pongWait 是服务端等待客户端 pong 响应的最长时长。
	pongWait = 60 * time.Second

	// pingPeriod 是服务端发送 ping 的间隔，必须小于 pongWait。
	pingPeriod = (pongWait * 9) / 10

	// maxMessageSize 是允许接收的最大消息大小（字节）。
	maxMessageSize = 4096
)

// Client 表示一个 WebSocket 连接，绑定了所属房间和用户信息。
type Client struct {
	hub       *Hub
	roomID    uint64
	userID    uint64
	conn      *websocket.Conn
	send      chan []byte
	closeOnce sync.Once // 确保 close(send) 只调用一次
}

// writePumpStats 全局统计 writePump 的写耗时和吞吐量（通过 atomic + 采样窗口）。
var writePumpStats struct {
	WriteCount   int64   // 累计 WriteMessage 次数
	WriteTotalMs int64   // 累计 WriteMessage 耗时 (ms)
	MsgCount     int64   // 累计从 send channel 收到的消息数
	mu           sync.Mutex
	writeCosts   []float64 // WriteMessage 耗时样本 (ms)
	loopCosts    []float64 // writePump 单条消息处理总耗时样本 (ms)
}

// RecordWritePumpStats 记录一次 writePump 的写操作统计。
func RecordWritePumpStats(writeCostMs, loopCostMs float64) {
	atomic.AddInt64(&writePumpStats.WriteCount, 1)
	atomic.AddInt64(&writePumpStats.WriteTotalMs, int64(writeCostMs))
	atomic.AddInt64(&writePumpStats.MsgCount, 1)
	writePumpStats.mu.Lock()
	if len(writePumpStats.writeCosts) < 10000 {
		writePumpStats.writeCosts = append(writePumpStats.writeCosts, writeCostMs)
	}
	if len(writePumpStats.loopCosts) < 10000 {
		writePumpStats.loopCosts = append(writePumpStats.loopCosts, loopCostMs)
	}
	writePumpStats.mu.Unlock()
}

// GetWritePumpStats 返回 writePump 诊断指标。
func GetWritePumpStats() (writeCount, writeTotalMs, msgCount int64, writeP50, writeP95, writeP99, loopP50, loopP95, loopP99 float64) {
	writeCount = atomic.LoadInt64(&writePumpStats.WriteCount)
	writeTotalMs = atomic.LoadInt64(&writePumpStats.WriteTotalMs)
	msgCount = atomic.LoadInt64(&writePumpStats.MsgCount)

	writePumpStats.mu.Lock()
	wc := make([]float64, len(writePumpStats.writeCosts))
	lc := make([]float64, len(writePumpStats.loopCosts))
	copy(wc, writePumpStats.writeCosts)
	copy(lc, writePumpStats.loopCosts)
	writePumpStats.writeCosts = writePumpStats.writeCosts[:0]
	writePumpStats.loopCosts = writePumpStats.loopCosts[:0]
	writePumpStats.mu.Unlock()

	sort.Float64s(wc)
	sort.Float64s(lc)

	p := func(data []float64, v float64) float64 {
		if len(data) == 0 {
			return 0
		}
		idx := int(math.Ceil(float64(len(data))*v)) - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(data) {
			idx = len(data) - 1
		}
		return data[idx]
	}

	return writeCount, writeTotalMs, msgCount,
		p(wc, 0.50), p(wc, 0.95), p(wc, 0.99),
		p(lc, 0.50), p(lc, 0.95), p(lc, 0.99)
}

// NewClient 创建一个新的 WebSocket 客户端实例，并启动读写协程。
func NewClient(hub *Hub, roomID uint64, userID uint64, conn *websocket.Conn) *Client {
	return &Client{
		hub:    hub,
		roomID: roomID,
		userID: userID,
		conn:   conn,
		send:   make(chan []byte, 256),
	}
}

// readPump 从 WebSocket 连接读取消息（当前仅处理 ping/pong 和关闭），
// 客户端发送的消息保留给未来扩展（如聊天、自定义订阅）。
func (c *Client) ReadPump() {
	defer func() {
		select {
		case c.hub.events <- HubEvent{Type: EventUnregister, Client: c}:
		default:
			log.Printf("[websocket] unregister channel full, client %d dropped", c.userID)
		}
		c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[websocket] client %d read error: %v", c.userID, err)
			}
			break
		}
		// 目前不处理客户端发来的消息，仅保持连接用于接收广播。
	}
}

// writePump 从 send channel 读取消息并写入 WebSocket 连接。
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			loopStart := time.Now()
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			writeStart := time.Now()
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[websocket] client %d write error: %v", c.userID, err)
				return
			}
			writeCost := time.Since(writeStart)
			loopCost := time.Since(loopStart)
			RecordWritePumpStats(
				float64(writeCost)/float64(time.Millisecond),
				float64(loopCost)/float64(time.Millisecond),
			)
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// WsMessage 是服务端推送给客户端的 WebSocket 消息结构。
type WsMessage struct {
	Type         string          `json:"type"`         // 消息类型：bid.accepted / ranking.updated / auction.ended / timer.sync / outbid
	Data         json.RawMessage `json:"data"`         // 消息体，类型由 Type 决定
	ServerSentAt int64           `json:"serverSentAt"` // 服务端广播时间戳（毫秒），用于客户端计算端到端延迟
}

// marshalStats 用于统计 json.Marshal 次数和耗时（通过 atomic 操作）。
var marshalStats struct {
	Count   int64
	TotalMs int64
}

// GetMarshalStats 返回序列化统计（count, totalMs）。
func GetMarshalStats() (int64, int64) {
	return atomic.LoadInt64(&marshalStats.Count), atomic.LoadInt64(&marshalStats.TotalMs)
}

// NewWsMessage 创建一条 WebSocket 消息的 JSON 字节。
// serverSentAt 在消息进入 Hub.Broadcast 前生成，用于客户端计算端到端延迟。
func NewWsMessage(msgType string, data interface{}) ([]byte, error) {
	start := time.Now()
	defer func() {
		atomic.AddInt64(&marshalStats.Count, 1)
		atomic.AddInt64(&marshalStats.TotalMs, int64(time.Since(start)/time.Millisecond))
	}()
	dataRaw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(WsMessage{
		Type:         msgType,
		Data:         dataRaw,
		ServerSentAt: time.Now().UnixMilli(),
	})
}
