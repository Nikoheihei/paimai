package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	goredis "github.com/redis/go-redis/v9"

	ws "paimai/internal/websocket"
)

const (
	// streamKey 是出价事件的 Redis Stream 名称。
	streamKey = "auction:events"

	// consumerGroup 是 Redis Stream 消费者组名称。
	consumerGroup = "auction:ws-pusher"

	// consumerName 是当前实例的消费者名称（单实例用固定名称）。
	consumerName = "instance-1"

	// maxStreamLen 是 Stream 的最大长度，超过后自动裁剪旧消息。
	maxStreamLen = 10000

	// pollInterval 是每次 XREADGROUP 阻塞等待的超时时间。
	pollInterval = 2 * time.Second
)

// Event 是 Stream 中每条消息的载荷结构。
type Event struct {
	Type      string          `json:"type"`      // 事件类型：bid.accepted
	RoomID    uint64          `json:"roomId"`    // 直播间 ID
	AuctionID uint64          `json:"auctionId"` // 竞拍 ID
	Payload   json.RawMessage `json:"payload"`   // 事件详情
}

// Publisher 负责向 Redis Stream 写入事件。
type Publisher struct {
	client *goredis.Client
}

// NewPublisher 创建 Stream 事件发布器。
func NewPublisher(client *goredis.Client) *Publisher {
	return &Publisher{client: client}
}

// Publish 向 Redis Stream 写入一条事件消息。
func (p *Publisher) Publish(ctx context.Context, evt Event) error {
	payload, err := json.Marshal(evt)
	if err != nil {
		return fmt.Errorf("stream marshal event: %w", err)
	}
	return p.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: streamKey,
		Values: map[string]interface{}{
			"payload": string(payload),
		},
		MaxLen: maxStreamLen,
	}).Err()
}

// Consumer 负责消费 Redis Stream 中的事件，并通过 WebSocket Hub 广播。
type Consumer struct {
	client *goredis.Client
	hub    *ws.Hub
}

// NewConsumer 创建 Stream 事件消费者。
func NewConsumer(client *goredis.Client, hub *ws.Hub) *Consumer {
	return &Consumer{client: client, hub: hub}
}

// ensureGroup 确保 Stream 消费者组存在；如果不存在则自动创建。
func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.client.Do(ctx, "XGROUP", "CREATE", streamKey, consumerGroup, "0", "MKSTREAM").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Start 在独立的 goroutine 中循环消费 Stream 事件。
// ctx 取消时退出循环，适合作为后台协程启动。
func (c *Consumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx); err != nil {
		log.Printf("[stream] 创建消费者组失败: %v", err)
		return
	}

	log.Println("[stream] 消费者已启动，等待事件...")

	for {
		select {
		case <-ctx.Done():
			log.Println("[stream] 消费者已退出")
			return
		default:
			c.poll(ctx)
		}
	}
}

// poll 执行一次 XREADGROUP 阻塞读取并处理事件。
func (c *Consumer) poll(ctx context.Context) {
	result, err := c.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    consumerGroup,
		Consumer: consumerName,
		Streams:  []string{streamKey, ">"},
		Count:    10,
		Block:    pollInterval,
	}).Result()
	if err != nil {
		return
	}

	if len(result) == 0 {
		return
	}

	for _, stream := range result {
		for _, message := range stream.Messages {
			c.processMessage(ctx, message)
		}
	}
}

// processMessage 解析单条 Stream 消息并调用 Hub 广播。
func (c *Consumer) processMessage(ctx context.Context, message goredis.XMessage) {
	log.Printf("[stream] 处理消息: id=%s", message.ID)
	payloadStr, ok := message.Values["payload"].(string)
	if !ok {
		log.Printf("[stream] 跳过无效消息: %v", message.Values)
		c.ack(ctx, message.ID)
		return
	}

	var evt Event
	if err := json.Unmarshal([]byte(payloadStr), &evt); err != nil {
		log.Printf("[stream] 解析事件失败: %v", err)
		c.ack(ctx, message.ID)
		return
	}

	// 将事件原封不动通过 WebSocket 推送到对应房间
	wsMsg, err := ws.NewWsMessage(evt.Type, evt)
	if err != nil {
		log.Printf("[stream] 构造 WS 消息失败: %v", err)
		c.ack(ctx, message.ID)
		return
	}

	c.hub.Broadcast(evt.RoomID, wsMsg)
	c.ack(ctx, message.ID)
}

// ack 确认消息已处理完成，避免重复消费。
func (c *Consumer) ack(ctx context.Context, messageID string) {
	if err := c.client.XAck(ctx, streamKey, consumerGroup, messageID).Err(); err != nil {
		log.Printf("[stream] ack 消息 %s 失败: %v", messageID, err)
	}
}
