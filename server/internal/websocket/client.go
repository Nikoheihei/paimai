package websocket

import (
	"encoding/json"
	"log"
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
	hub    *Hub
	roomID uint64
	userID uint64
	conn   *websocket.Conn
	send   chan []byte
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
		c.hub.unregister <- c
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
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("[websocket] client %d write error: %v", c.userID, err)
				return
			}
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
	Type string          `json:"type"` // 消息类型：bid.accepted / ranking.updated / auction.ended / timer.sync / outbid
	Data json.RawMessage `json:"data"` // 消息体，类型由 Type 决定
}

// NewWsMessage 创建一条 WebSocket 消息的 JSON 字节。
func NewWsMessage(msgType string, data interface{}) ([]byte, error) {
	dataRaw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(WsMessage{
		Type: msgType,
		Data: dataRaw,
	})
}
