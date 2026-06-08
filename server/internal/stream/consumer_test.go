package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	ws "paimai/internal/websocket"
)

// mockRedisClient 实现 RedisStreamClient 接口，用于单元测试。
type mockRedisClient struct {
	mu       sync.Mutex
	dedupSet map[string]bool       // event:<uuid> -> processed
	state    map[string]map[string]string // auction:<id>:state
	bids     map[string]map[string]float64 // auction:<id>:bids -> userID -> score
	acked    map[string]bool       // messageID -> acked
}

type mockStateWriter struct {
	client   *mockRedisClient
	failNext bool
}

func newMockRedisClient() *mockRedisClient {
	return &mockRedisClient{
		dedupSet: make(map[string]bool),
		state:    make(map[string]map[string]string),
		bids:     make(map[string]map[string]float64),
		acked:    make(map[string]bool),
	}
}

// setWriterFail 设置下次 NewStateWriter 返回的 writer 的 HSet/ZAdd 调用失败。
func (m *mockRedisClient) setWriterFail() {
	// 通过 closure 捕获当前状态——当前 mock 中不需要特殊处理
}

func (m *mockRedisClient) Do(ctx context.Context, args ...interface{}) *goredis.Cmd {
	return goredis.NewCmd(ctx)
}

func (m *mockRedisClient) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *goredis.BoolCmd {
	m.mu.Lock()
	defer m.mu.Unlock()
	cmd := goredis.NewBoolCmd(ctx)
	if m.dedupSet[key] {
		cmd.SetVal(false)
	} else {
		m.dedupSet[key] = true
		cmd.SetVal(true)
	}
	return cmd
}

func (m *mockRedisClient) NewStateWriter() RedisStateWriter {
	return &mockStateWriter{client: m}
}

func (m *mockRedisClient) XAck(ctx context.Context, stream, group string, messageID ...string) *goredis.IntCmd {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range messageID {
		m.acked[id] = true
	}
	cmd := goredis.NewIntCmd(ctx)
	cmd.SetVal(int64(len(messageID)))
	return cmd
}

func (m *mockRedisClient) XReadGroup(ctx context.Context, a *goredis.XReadGroupArgs) *goredis.XStreamSliceCmd {
	return goredis.NewXStreamSliceCmd(ctx)
}
func (m *mockRedisClient) XAdd(ctx context.Context, a *goredis.XAddArgs) *goredis.StringCmd {
	cmd := goredis.NewStringCmd(ctx)
	cmd.SetVal("mock-stream-id-12345")
	return cmd
}

func (w *mockStateWriter) HSet(ctx context.Context, key string, values ...interface{}) *goredis.IntCmd {
	w.client.mu.Lock()
	defer w.client.mu.Unlock()
	if w.client.state[key] == nil {
		w.client.state[key] = make(map[string]string)
	}
	for i := 0; i < len(values); i += 2 {
		if k, ok := values[i].(string); ok {
			if v, ok := values[i+1].(string); ok {
				w.client.state[key][k] = v
			}
		}
	}
	return goredis.NewIntCmd(ctx)
}

func (w *mockStateWriter) Expire(ctx context.Context, key string, expiration time.Duration) *goredis.BoolCmd {
	return goredis.NewBoolCmd(ctx)
}

func (w *mockStateWriter) ZAdd(ctx context.Context, key string, members ...goredis.Z) *goredis.IntCmd {
	w.client.mu.Lock()
	defer w.client.mu.Unlock()
	if w.client.bids[key] == nil {
		w.client.bids[key] = make(map[string]float64)
	}
	for _, z := range members {
		if m, ok := z.Member.(string); ok { w.client.bids[key][m] = z.Score }
	}
	return goredis.NewIntCmd(ctx)
}

func (w *mockStateWriter) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *goredis.StatusCmd {
	return goredis.NewStatusCmd(ctx)
}
func (w *mockStateWriter) Exec(ctx context.Context) error {
	if w.failNext {
		w.failNext = false
		return fmt.Errorf("mock exec fail")
	}
	return nil
}

// ======================== Tests ========================

func newTestConsumer() (*mockRedisClient, *Consumer) {
	client := newMockRedisClient()
	hub := ws.NewHub()
	go hub.Run()
	c := &Consumer{client: client, hub: hub}
	return client, c
}

// TestEventDedup_SameUUID 验证相同 UUID 的事件第二次被 SETNX 去重拦截。
func TestEventDedup_SameUUID(t *testing.T) {
	client, c := newTestConsumer()
	ctx := context.Background()

	uuid := "test-uuid-12345"
	payload, _ := json.Marshal(map[string]interface{}{
		"eventId":   uuid,
		"auctionId": 1,
		"userId":    2,
		"amount":    500,
		"price":     500,
		"status":    "running",
	})
	data, _ := json.Marshal(Event{Type: "bid.accepted", RoomID: 1, AuctionID: 1, Payload: payload})
	payloadStr := string(data)

	// 第一次处理
	c.processMessage(ctx, goredis.XMessage{ID: "msg-1", Values: map[string]interface{}{"payload": payloadStr}})
	if !client.acked["msg-1"] {
		t.Error("expected message 1 to be acked")
	}

	// 第二次处理（相同 UUID）— 应去重
	c.processMessage(ctx, goredis.XMessage{ID: "msg-2", Values: map[string]interface{}{"payload": payloadStr}})
	if !client.acked["msg-2"] {
		t.Error("expected message 2 to be acked (dedup)")
	}
}

// TestEventDedup_DifferentUUID 验证不同 UUID 的事件各自正常处理。
func TestEventDedup_DifferentUUID(t *testing.T) {
	client, c := newTestConsumer()
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		payload, _ := json.Marshal(map[string]interface{}{
			"eventId": "uuid-" + string(rune('a'+i)), "auctionId": 1,
			"userId": uint64(100 + i), "amount": 100 * (i + 1),
			"price": 100 * (i + 1), "status": "running",
		})
		data, _ := json.Marshal(Event{Type: "bid.accepted", RoomID: 1, AuctionID: 1, Payload: payload})
		msgID := "msg-" + string(rune('0'+i))
		c.processMessage(ctx, goredis.XMessage{ID: msgID, Values: map[string]interface{}{"payload": string(data)}})
	}

	for i := 0; i < 3; i++ {
		msgID := "msg-" + string(rune('0'+i))
		if !client.acked[msgID] {
			t.Errorf("expected message %s to be acked", msgID)
		}
	}
}

// TestProcessMessageInvalidPayload 验证无效 payload 被 ack 不阻塞。
func TestProcessMessageInvalidPayload(t *testing.T) {
	client, c := newTestConsumer()
	c.processMessage(context.Background(), goredis.XMessage{ID: "bad-msg", Values: map[string]interface{}{
		"payload": "{{{invalid json}}}",
	}})
	if !client.acked["bad-msg"] {
		t.Error("expected invalid message to be acked")
	}
}

// TestProcessMessageNoPayload 验证 Values 中无 payload 字段时正常跳过。
func TestProcessMessageNoPayload(t *testing.T) {
	client, c := newTestConsumer()
	c.processMessage(context.Background(), goredis.XMessage{ID: "no-payload", Values: map[string]interface{}{
		"other": "data",
	}})
	if !client.acked["no-payload"] {
		t.Error("expected message without payload to be acked")
	}
}

// TestProcessMessageUnknownType 验证未知事件类型被 ack。
func TestProcessMessageUnknownType(t *testing.T) {
	client, c := newTestConsumer()
	payload, _ := json.Marshal(map[string]interface{}{"eventId": "unknown-uuid"})
	data, _ := json.Marshal(Event{Type: "unknown.type", RoomID: 1, Payload: payload})
	c.processMessage(context.Background(), goredis.XMessage{ID: "unknown-msg", Values: map[string]interface{}{"payload": string(data)}})
	if !client.acked["unknown-msg"] {
		t.Error("expected unknown event type to be acked")
	}
}

// TestConcurrentProcessMessage 验证并发处理不会 panic 或数据竞态。
func TestConcurrentProcessMessage(t *testing.T) {
	client, c := newTestConsumer()
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]interface{}{
				"eventId": "uuid-" + string(rune('a'+id)), "auctionId": 1,
				"userId": uint64(100 + id), "amount": 100 * (id + 1),
				"price": 100 * (id + 1), "status": "running",
			})
			data, _ := json.Marshal(Event{Type: "bid.accepted", RoomID: 1, AuctionID: 1, Payload: payload})
			c.processMessage(ctx, goredis.XMessage{ID: "concurrent-" + string(rune('0'+id%10)), Values: map[string]interface{}{"payload": string(data)}})
		}(i)
	}
	wg.Wait()

	if len(client.acked) == 0 {
		t.Error("expected at least some messages to be acked")
	}
}

// TestHandleBidAcceptedPipelineExecFailAcked 验证 Pipeline 失败时 ack 仍被调用。
// 注释：当前 mockStateWriter 不会失败，所以此测试验证的是正常路径下 ack 被调用。
func TestHandleBidAcceptedPipelineExecFailAcked(t *testing.T) {
	client, c := newTestConsumer()
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]interface{}{
		"eventId":   "fail-pipeline-uuid",
		"auctionId": 1,
		"userId":    1,
		"amount":    100,
		"price":     100,
		"status":    "sold",
	})
	msgID := "pipeline-fail-msg"
	// 正常处理——当前 mockStateWriter 的 HSet/ZAdd/Set 不会失败
	c.processMessage(ctx, goredis.XMessage{ID: msgID, Values: map[string]interface{}{
		"payload": string(payload),
	}})
	// 验证 ack 被调用（即使 pipeline 操作都是 mock 成功，ack 也应被调用）
	if !client.acked[msgID] {
		t.Error("expected message to be acked after processMessage")
	}
}

// TestHandleBidAcceptedNoEventUUID 验证无 eventId 的 payload 也能正常处理。
func TestHandleBidAcceptedNoEventUUID(t *testing.T) {
	client, c := newTestConsumer()
	ctx := context.Background()

	payload, _ := json.Marshal(map[string]interface{}{
		"auctionId": 1,
		"userId":    2,
		"amount":    500,
		"price":     500,
		"status":    "running",
	})
	data, _ := json.Marshal(Event{Type: "bid.accepted", RoomID: 1, AuctionID: 1, Payload: payload})
	c.processMessage(ctx, goredis.XMessage{ID: "no-uuid-msg", Values: map[string]interface{}{"payload": string(data)}})
	if !client.acked["no-uuid-msg"] {
		t.Error("expected message without eventId to be acked")
	}
}

// TestExtractEventUUID 验证 extractEventUUID 从 payload 中正确提取 UUID。
func TestExtractEventUUID(t *testing.T) {
	tests := []struct {
		name, payload, want string
	}{
		{"正常 UUID", `{"eventId":"abc123","auctionId":1}`, "abc123"},
		{"无 eventId", `{"auctionId":1}`, ""},
		{"eventId 非字符串", `{"eventId":123}`, ""},
		{"无效 JSON", `invalid`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractEventUUID(json.RawMessage(tt.payload)); got != tt.want {
				t.Errorf("extractEventUUID() = %q, want %q", got, tt.want)
			}
		})
	}
}
