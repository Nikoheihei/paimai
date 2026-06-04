//go:build integration
// +build integration

package websocket

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	goredis "github.com/redis/go-redis/v9"
)

// TestWebSocketBroadcastFromStreamIntegration 验证 WS 连接成功后能收到来自 Redis Stream 的广播。
func TestWebSocketBroadcastFromStreamIntegration(t *testing.T) {
	redisClient, err := newRedisForWSTest()
	if err != nil {
		t.Skipf("跳过集成测试（Redis 不可用）: %v", err)
	}

	hub := NewHub()
	go hub.Run()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamKey := "test:ws:int:" + t.Name()
	consumerGroup := "ws:int:cg"
	consumerName := t.Name()

	// 创建消费组
	_ = redisClient.XGroupCreate(ctx, streamKey, consumerGroup, "0").Err()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				entries, err := redisClient.XReadGroup(ctx, &goredis.XReadGroupArgs{
					Group:    consumerGroup,
					Consumer: consumerName,
					Streams:  []string{streamKey, ">"},
					Count:    10,
					Block:    1 * time.Second,
				}).Result()
				if err != nil || len(entries) == 0 {
					continue
				}
				for _, stream := range entries {
					for _, msg := range stream.Messages {
						payloadStr, ok := msg.Values["payload"].(string)
						if !ok {
							continue
						}
						var evt struct {
							RoomID uint64 `json:"roomId"`
						}
						if err := json.Unmarshal([]byte(payloadStr), &evt); err != nil {
							continue
						}
						hub.Broadcast(evt.RoomID, []byte(payloadStr))
						redisClient.XAck(ctx, streamKey, consumerGroup, msg.ID)
					}
				}
			}
		}
	}()

	time.Sleep(200 * time.Millisecond)

	// 启动 WS 服务器
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WS upgrade failed: %v", err)
			return
		}
		client := NewClient(hub, 1, 100, conn)
		hub.Register(client)
		go client.WritePump()
		go client.ReadPump()
	})
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Skipf("跳过 WS 测试（端口绑定失败）: %v", r)
			}
		}()
		server := httptest.NewServer(mux)
		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
		wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("WS 连接失败: %v", err)
		}
		defer wsConn.Close()

		// 通过 Redis 发布事件
		payload := `{"type":"bid.accepted","roomId":1,"auctionId":1,"amount":500}`
		_, err = redisClient.XAdd(ctx, &goredis.XAddArgs{
			Stream: streamKey,
			Values: map[string]interface{}{
				"type":    "bid.accepted",
				"roomId":  "1",
				"payload": payload,
			},
		}).Result()
		if err != nil {
			t.Fatalf("Redis XAdd failed: %v", err)
		}

		// 等待并验证 WS 收到广播
		wsConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, message, err := wsConn.ReadMessage()
		if err != nil {
			t.Fatalf("WS 读消息失败（未收到广播）: %v", err)
		}
		var received map[string]interface{}
		if err := json.Unmarshal(message, &received); err != nil {
			t.Fatalf("消息解析失败: %v", err)
		}
		if received["type"] != "bid.accepted" {
			t.Errorf("expected type 'bid.accepted', got %v", received["type"])
		}
	}()
}

// TestWebSocketConnectionIntegration 验证正常的 WS 连接建立和关闭流程。
func TestWebSocketConnectionIntegration(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WS upgrade failed: %v", err)
			return
		}
		client := NewClient(hub, 2, 200, conn)
		hub.Register(client)
		go client.WritePump()
		go client.ReadPump()
	})

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Skipf("跳过 WS 测试（端口绑定失败）: %v", r)
			}
		}()
		server := httptest.NewServer(mux)
		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
		wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("WS 连接失败: %v", err)
		}

		// 发送 ping
		if err := wsConn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
			t.Fatalf("发送 ping 失败: %v", err)
		}

		wsConn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, err = wsConn.ReadMessage()
		if err != nil {
			t.Logf("ReadMessage after ping (expected timeout): %v", err)
		}

		// 正常关闭
		if err := wsConn.Close(); err != nil {
			t.Logf("WS 关闭: %v", err)
		}
		time.Sleep(200 * time.Millisecond)

		// 验证 hub 中的 client 已清理
		hub.mu.Lock()
		room, ok := hub.rooms[2]
		if ok {
			clientCount := len(room.clients)
			if clientCount != 0 {
				t.Errorf("expected 0 clients after close, got %d", clientCount)
			}
		}
		hub.mu.Unlock()
	}()
}

// newRedisForWSTest 连接测试 Redis。
func newRedisForWSTest() (*goredis.Client, error) {
	addr := os.Getenv("TEST_REDIS_MASTER")
	if addr == "" {
		addr = "localhost:6381"
	}
	rdb := goredis.NewClient(&goredis.Options{Addr: addr})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := rdb.Ping(ctx).Result(); err != nil {
		rdb.Close()
		return nil, err
	}
	return rdb, nil
}
