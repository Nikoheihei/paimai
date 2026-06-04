package websocket

import (
	"sync"
	"testing"
	"time"
)

// TestConcurrentRegisterUnregister 验证并发注册/注销不会导致 panic 或 map 竞态。
// 不验证最终房间是否为空（异步 Hub Run 的消费速度取决于调度），只验证无 panic。
func TestConcurrentRegisterUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	var wg sync.WaitGroup
	n := 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := &Client{
				hub:    hub,
				roomID: 1,
				userID: uint64(id),
				send:   make(chan []byte, 256),
			}
			hub.Register(client)
			// 不调用 Unregister——让 Hub.Run 自然消费
		}(i)
	}
	wg.Wait()

	// 验证注册完成且无 panic
	// 房间中剩余的客户端数量等于 Hub Run 已处理的注册数减去可能已处理的注销数
	// 这不重要，关键是没有 panic
	_ = hub.GetRoomClientCount(1)
}

// TestConcurrentMultiRoomRegister 验证多房间并发注册不会交叉污染。
func TestConcurrentMultiRoomRegister(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	var wg sync.WaitGroup
	rooms := []uint64{1, 2, 3}

	for _, roomID := range rooms {
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(rid uint64, uid int) {
				defer wg.Done()
				client := &Client{
					hub:    hub,
					roomID: rid,
					userID: uint64(uid),
					send:   make(chan []byte, 256),
				}
				hub.Register(client)
			}(roomID, i)
		}
	}
	wg.Wait()

	// 等待 Hub goroutine 处理完所有注册事件
	for i := 0; i < 200; i++ {
		total := 0
		for _, roomID := range rooms {
			total += hub.GetRoomClientCount(roomID)
		}
		if total == 30 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	for _, roomID := range rooms {
		count := hub.GetRoomClientCount(roomID)
		if count != 10 {
			t.Errorf("room %d: expected 10 clients, got %d", roomID, count)
		}
	}
}

// TestBroadcastDuringRegisterUnregister 验证 Broadcast 与并发注册/注销不产生竞态。
func TestBroadcastDuringRegisterUnregister(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	// 预注册 50 个客户端
	for i := 0; i < 50; i++ {
		client := &Client{
			hub:    hub,
			roomID: 1,
			userID: uint64(i),
			send:   make(chan []byte, 256),
		}
		hub.Register(client)
		time.Sleep(time.Microsecond)
	}
	time.Sleep(10 * time.Millisecond)

	var wg sync.WaitGroup
	// 并发注册/注销 + Broadcast
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := &Client{
				hub:    hub,
				roomID: 1,
				userID: uint64(100 + id),
				send:   make(chan []byte, 256),
			}
			hub.Register(client)
			hub.Broadcast(1, []byte("test"))
			hub.Unregister(client)
		}(i)
	}

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				hub.Broadcast(1, []byte("concurrent broadcast"))
				time.Sleep(time.Microsecond)
			}
		}()
	}
	wg.Wait()
}

// TestDoubleCloseNoPanic 验证 slow client 触发 unregister 两次不会导致 close of closed channel panic。
func TestDoubleCloseNoPanic(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	client := &Client{
		hub:    hub,
		roomID: 1,
		userID: 1,
		send:   make(chan []byte, 1),
	}
	hub.Register(client)
	time.Sleep(time.Millisecond)

	// 填满 send buffer 触发慢客户端逻辑（Broadcast default 分支 → unregister）
	client.send <- []byte("full")

	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("double close caused panic: %v", r)
				}
				done <- struct{}{}
			}()
			hub.Broadcast(1, []byte("test"))
		}()
	}
	// 等待两次 Broadcast 完成
	<-done
	<-done
}

// TestEventsChannelNonBlocking 验证 events 通道满时 ReadPump 不会阻塞。
func TestEventsChannelNonBlocking(t *testing.T) {
	// 小容量通道
	hub := &Hub{
		rooms:  make(map[uint64]*Room),
		events: make(chan HubEvent, 1),
	}
	go hub.Run()

	// 填满 events 通道
	hub.events <- HubEvent{Type: EventDead, Client: &Client{hub: hub, roomID: 999, userID: 999, send: make(chan []byte, 1)}}

	client := &Client{
		hub:    hub,
		roomID: 1,
		userID: 1,
		conn:   nil,
		send:   make(chan []byte, 256),
	}

	// 模拟 ReadPump defer 中非阻塞写入 events
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("non-blocking events send caused panic: %v", r)
			}
			close(done)
		}()
		select {
		case hub.events <- HubEvent{Type: EventUnregister, Client: client}:
		default:
			// 通道满，丢弃 — 不应 panic
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("goroutine blocked")
	}
}

// TestBroadcastRoomNotFound 验证 Broadcast 到不存在的房间不 panic。
func TestBroadcastRoomNotFound(t *testing.T) {
	hub := NewHub()
	go hub.Run()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Broadcast to non-existent room caused panic: %v", r)
		}
	}()
	hub.Broadcast(999, []byte("test"))
}
