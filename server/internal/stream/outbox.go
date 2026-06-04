package stream

import (
	"context"
	"encoding/json"
	"log"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"paimai/internal/model"
	"paimai/internal/repository"
)

// RedisXAdder 是 OutboxPoller 依赖的 Redis 写入接口，用于测试 mock。
type RedisXAdder interface {
	XAdd(ctx context.Context, a *goredis.XAddArgs) *goredis.StringCmd
}

// OutboxPoller 轮询 MySQL outbox 表，将 pending 事件投递到 Redis Stream。
type OutboxPoller struct {
	adminStore repository.AdminStore
	redis      RedisXAdder
	streamKey  string
	interval   time.Duration
	batchSize  int
}

// NewOutboxPoller 创建 outbox 轮询器。
func NewOutboxPoller(store repository.AdminStore, redis RedisXAdder) *OutboxPoller {
	return &OutboxPoller{
		adminStore: store,
		redis:      redis,
		streamKey:  "auction:events",
		interval:   100 * time.Millisecond,
		batchSize:  50,
	}
}

// Start 在独立 goroutine 中轮询 outbox 表。
func (p *OutboxPoller) Start(ctx context.Context) {
	log.Println("[outbox] poller started (interval=100ms)")
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[outbox] poller stopped")
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

// pollOnce 执行一次轮询：取 pending 事件 → XADD → 标记 done。
// 这是 at-least-once 语义，消费端通过 eventUUID 做幂等去重。
func (p *OutboxPoller) pollOnce(ctx context.Context) {
	events, err := p.adminStore.PickPendingOutboxEvents(ctx, p.batchSize)
	if err != nil {
		log.Printf("[outbox] pick events error: %v", err)
		return
	}
	if len(events) == 0 {
		return
	}

	for _, evt := range events {
		if err := p.publishEvent(ctx, &evt); err != nil {
			log.Printf("[outbox] publish event %d failed: %v", evt.ID, err)
			_ = p.adminStore.MarkOutboxEventFailed(ctx, evt.ID)
			continue
		}
		if err := p.adminStore.MarkOutboxEventDone(ctx, evt.ID); err != nil {
			log.Printf("[outbox] mark event %d done failed (will retry): %v", evt.ID, err)
		}
	}
}

// publishEvent 将 outbox 事件写入 Redis Stream。
func (p *OutboxPoller) publishEvent(ctx context.Context, evt *model.OutboxEvent) error {
	// 解析 payload 获取 roomId
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(evt.Payload), &payload); err != nil {
		return err
	}

	roomID, _ := payload["roomId"].(float64)

	event := Event{
		Type:      evt.EventType,
		RoomID:    uint64(roomID),
		AuctionID: uint64(0), // from payload if needed
		Payload:   json.RawMessage(evt.Payload),
	}

	payloadBytes, err := json.Marshal(event)
	if err != nil {
		return err
	}

	return p.redis.XAdd(ctx, &goredis.XAddArgs{
		Stream: p.streamKey,
		Values: map[string]interface{}{
			"payload": string(payloadBytes),
		},
		MaxLen: 10000,
	}).Err()
}
