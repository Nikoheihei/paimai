package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	ws "paimai/internal/websocket"
)

const (
	streamKey       = "auction:events"
	consumerGroup   = "auction:processors"
	consumerName    = "instance-1"
	maxStreamLen    = 10000
	pollInterval    = 2 * time.Second
)

// Event 是 Stream 中每条消息的载荷结构。
type Event struct {
	Type      string          `json:"type"`
	RoomID    uint64          `json:"roomId"`
	AuctionID uint64          `json:"auctionId"`
	Payload   json.RawMessage `json:"payload"`
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

// Consumer 负责消费 Redis Stream 中的事件，更新 Redis 状态和通过 WebSocket Hub 广播。
type Consumer struct {
	client *goredis.Client
	hub    *ws.Hub
}

// NewConsumer 创建 Stream 事件消费者。
func NewConsumer(client *goredis.Client, hub *ws.Hub) *Consumer {
	return &Consumer{client: client, hub: hub}
}

// ensureGroup 确保 Stream 消费者组存在。
func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.client.Do(ctx, "XGROUP", "CREATE", streamKey, consumerGroup, "0", "MKSTREAM").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

// Start 在独立 goroutine 中循环消费 Stream 事件。
func (c *Consumer) Start(ctx context.Context) {
	if err := c.ensureGroup(ctx); err != nil {
		log.Printf("[stream] 创建消费者组失败: %v", err)
		return
	}
	log.Println("[stream] consumer started, waiting for events...")

	for {
		select {
		case <-ctx.Done():
			log.Println("[stream] consumer stopped")
			return
		default:
			c.poll(ctx)
		}
	}
}

// poll 执行一次 XREADGROUP 读取并处理事件。
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

// processMessage 解析单条 Stream 消息并执行：更新 Redis 状态 + WS 广播。
func (c *Consumer) processMessage(ctx context.Context, message goredis.XMessage) {
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

	// 根据事件类型处理
	switch evt.Type {
	case "bid.accepted":
		c.handleBidAccepted(ctx, &evt)
	default:
		log.Printf("[stream] 未知事件类型: %s", evt.Type)
	}

	// WS 广播（所有类型的事件都推送给房间客户端）
	wsMsg, err := ws.NewWsMessage(evt.Type, evt)
	if err == nil {
		c.hub.Broadcast(evt.RoomID, wsMsg)
	}

	c.ack(ctx, message.ID)
}

// handleBidAccepted 处理出价事件：更新 Redis 竞拍状态和排行榜。
func (c *Consumer) handleBidAccepted(ctx context.Context, evt *Event) {
	var payload map[string]interface{}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		log.Printf("[stream] 解析 bid.accepted payload 失败: %v", err)
		return
	}

	auctionID := uint64(0)
	if id, ok := payload["auctionId"].(float64); ok {
		auctionID = uint64(id)
	}
	userID := uint64(0)
	if id, ok := payload["userId"].(float64); ok {
		userID = uint64(id)
	}
	amount := int64(0)
	if a, ok := payload["amount"].(float64); ok {
		amount = int64(a)
	}
	status, _ := payload["status"].(string)
	price := int64(0)
	if p, ok := payload["price"].(float64); ok {
		price = int64(p)
	}

	stateKey := fmt.Sprintf("auction:%d:state", auctionID)
	bidsKey := fmt.Sprintf("auction:%d:bids", auctionID)

	pipe := c.client.Pipeline()

	// 更新竞拍状态（HSET）
	pipe.HSet(ctx, stateKey, map[string]interface{}{
		"status":             status,
		"currentPriceCents":  strconv.FormatInt(price, 10),
		"leaderUserId":       strconv.FormatUint(userID, 10),
	})
	pipe.Expire(ctx, stateKey, 86400*time.Second)

	// 更新排行榜（ZSet — 以最高出价为 score）
	pipe.ZAdd(ctx, bidsKey, goredis.Z{
		Score:  float64(amount),
		Member: strconv.FormatUint(userID, 10),
	})
	pipe.Expire(ctx, bidsKey, 86400*time.Second)

	// 记录最后出价时间（频率检查用）
	lastTsKey := fmt.Sprintf("auction:%d:last_bid_ts:%d", auctionID, userID)
	pipe.Set(ctx, lastTsKey, time.Now().UnixMilli(), 86400*time.Second)

	if _, err := pipe.Exec(ctx); err != nil {
		log.Printf("[stream] 更新 Redis 状态失败 (auction=%d): %v", auctionID, err)
		return
	}

	// sold 状态已在 Pipeline 的 HSET 中设置（payload.status 为 "sold"），无需重复写入
}

// ack 确认消息处理完成。
func (c *Consumer) ack(ctx context.Context, messageID string) {
	if err := c.client.XAck(ctx, streamKey, consumerGroup, messageID).Err(); err != nil {
		log.Printf("[stream] ack 消息 %s 失败: %v", messageID, err)
	}
}
