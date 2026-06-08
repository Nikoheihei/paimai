package stream

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	ws "paimai/internal/websocket"
)

// RedisStateWriter 封装 Pipeline 中的状态写入操作，用于测试 mock。
type RedisStateWriter interface {
	HSet(ctx context.Context, key string, values ...interface{}) *goredis.IntCmd
	Expire(ctx context.Context, key string, expiration time.Duration) *goredis.BoolCmd
	ZAdd(ctx context.Context, key string, members ...goredis.Z) *goredis.IntCmd
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *goredis.StatusCmd
	Exec(ctx context.Context) error
}

// RedisStreamClient 是 Consumer 依赖的 Redis 操作接口，用于测试 mock。
type RedisStreamClient interface {
	Do(ctx context.Context, args ...interface{}) *goredis.Cmd
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *goredis.BoolCmd
	NewStateWriter() RedisStateWriter
	XAck(ctx context.Context, stream, group string, messageID ...string) *goredis.IntCmd
	XReadGroup(ctx context.Context, a *goredis.XReadGroupArgs) *goredis.XStreamSliceCmd
}

var consumerName = fmt.Sprintf("instance-%d", os.Getpid())

const (
	streamKey     = "auction:events"
	consumerGroup = "auction:processors"
	maxStreamLen  = 10000
	pollInterval  = 100 * time.Millisecond
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
// goredisStateWriter 包装 *goredis.Client 的 Pipeline。
type goredisStateWriter struct {
	pipe goredis.Pipeliner
}

func (w *goredisStateWriter) HSet(ctx context.Context, key string, values ...interface{}) *goredis.IntCmd {
	return w.pipe.HSet(ctx, key, values...)
}
func (w *goredisStateWriter) Expire(ctx context.Context, key string, expiration time.Duration) *goredis.BoolCmd {
	return w.pipe.Expire(ctx, key, expiration)
}
func (w *goredisStateWriter) ZAdd(ctx context.Context, key string, members ...goredis.Z) *goredis.IntCmd {
	return w.pipe.ZAdd(ctx, key, members...)
}
func (w *goredisStateWriter) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *goredis.StatusCmd {
	return w.pipe.Set(ctx, key, value, expiration)
}
func (w *goredisStateWriter) Exec(ctx context.Context) error {
	_, err := w.pipe.Exec(ctx)
	return err
}

// goredisClientAdapter 包装 *goredis.Client 使其满足 RedisStreamClient 接口。
type goredisClientAdapter struct {
	client *goredis.Client
}

func (a *goredisClientAdapter) Do(ctx context.Context, args ...interface{}) *goredis.Cmd {
	return a.client.Do(ctx, args...)
}
func (a *goredisClientAdapter) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) *goredis.BoolCmd {
	return a.client.SetNX(ctx, key, value, expiration)
}
func (a *goredisClientAdapter) NewStateWriter() RedisStateWriter {
	return &goredisStateWriter{pipe: a.client.Pipeline()}
}
func (a *goredisClientAdapter) XAck(ctx context.Context, stream, group string, messageID ...string) *goredis.IntCmd {
	return a.client.XAck(ctx, stream, group, messageID...)
}
func (a *goredisClientAdapter) XReadGroup(ctx context.Context, args *goredis.XReadGroupArgs) *goredis.XStreamSliceCmd {
	return a.client.XReadGroup(ctx, args)
}

type Consumer struct {
	client RedisStreamClient
	hub    *ws.Hub
}

// NewConsumer 创建 Stream 事件消费者。
func NewConsumer(client *goredis.Client, hub *ws.Hub) *Consumer {
	return &Consumer{client: &goredisClientAdapter{client: client}, hub: hub}
}

// ensureGroup 确保 Stream 消费者组存在。
func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.client.Do(ctx, "XGROUP", "CREATE", streamKey, consumerGroup, "0", "MKSTREAM").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
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
		Count:    500,
		Block:    pollInterval,
	}).Result()
	if err != nil {
		if err != goredis.Nil {
			// NOGROUP: Stream 或 consumer group 被删除（如 FLUSHDB），自动重建
			if strings.Contains(err.Error(), "NOGROUP") {
				if e := c.ensureGroup(ctx); e != nil {
					log.Printf("[stream] 重建消费者组失败: %v", e)
				}
			} else {
				log.Printf("[stream] poll error: %v", err)
			}
		}
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

	// 从 payload 提取 eventUUID 用于去重
	eventUUID := extractEventUUID(evt.Payload)
	if eventUUID != "" {
		// SETNX 去重：已处理过则直接 ack 跳过
		ok, err := c.client.SetNX(ctx, "event:"+eventUUID, "1", 86400*time.Second).Result()
		if err != nil {
			log.Printf("[stream] 去重检查失败 (uuid=%s): %v", eventUUID, err)
		} else if !ok {
			log.Printf("[stream] 跳过重复事件 (uuid=%s)", eventUUID)
			c.ack(ctx, message.ID)
			return
		}
	}

	// 根据事件类型处理
	switch evt.Type {
	case "bid.accepted":
		c.handleBidAccepted(ctx, &evt, message.ID)
	case "order.created", "order.paid", "order.closed":
		c.handleOrderChanged(ctx, &evt, message.ID)
	case "product.created", "product.offline":
		c.handleProductCreated(ctx, &evt, message.ID)
	case "auction.created", "auction.updated", "auction.payment_timeout":
		// 竞拍列表变化只需要广播给客户端，前端收到后重新拉取权威列表。
	default:
		log.Printf("[stream] 未知事件类型: %s", evt.Type)
	}

	// WS 广播（所有类型的事件都推送给房间客户端）
	wsMsg, err := ws.NewWsMessage(evt.Type, evt)
	if err == nil {
		if evt.RoomID == 0 {
			c.hub.BroadcastAll(wsMsg)
		} else {
			c.hub.Broadcast(evt.RoomID, wsMsg)
		}
	}

	c.ack(ctx, message.ID)
}

// handleBidAccepted 处理出价事件：更新 Redis 竞拍状态和排行榜。
func (c *Consumer) handleBidAccepted(ctx context.Context, evt *Event, messageID string) {
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

	writer := c.client.NewStateWriter()

	// 批量写入 Redis 状态
	writer.HSet(ctx, stateKey,
		"status", status,
		"currentPriceCents", strconv.FormatInt(price, 10),
		"leaderUserId", strconv.FormatUint(userID, 10),
	)
	writer.Expire(ctx, stateKey, 86400*time.Second)

	writer.ZAdd(ctx, bidsKey, goredis.Z{
		Score:  float64(amount),
		Member: strconv.FormatUint(userID, 10),
	})
	writer.Expire(ctx, bidsKey, 86400*time.Second)

	lastTsKey := fmt.Sprintf("auction:%d:last_bid_ts:%d", auctionID, userID)
	writer.Set(ctx, lastTsKey, time.Now().UnixMilli(), 86400*time.Second)

	// 执行 Pipeline，真正写入 Redis
	if err := writer.Exec(ctx); err != nil {
		log.Printf("[stream] handleBidAccepted Pipeline 执行失败: %v", err)
	}

	// sold 状态已在 Pipeline 的 HSET 中设置（payload.status 为 "sold"），无需重复写入
}

// handleOrderChanged 处理订单事件：通过 WS 推送通知用户和商家刷新订单/排行榜。
func (c *Consumer) handleOrderChanged(ctx context.Context, evt *Event, messageID string) {
	var payload map[string]interface{}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		log.Printf("[stream] 解析 %s payload 失败: %v", evt.Type, err)
		return
	}
	// 订单事件不需要写入 Redis，只需通过 WS 广播即可
	// 前端收到后会自动刷新订单列表和支付状态
	_ = payload
}

// handleProductCreated 处理商品创建事件：通过 WS 推送通知客户端刷新商品列表。
func (c *Consumer) handleProductCreated(ctx context.Context, evt *Event, messageID string) {
	var payload map[string]interface{}
	if err := json.Unmarshal(evt.Payload, &payload); err != nil {
		log.Printf("[stream] 解析 product.created payload 失败: %v", err)
		return
	}
	// 商品创建事件不需要写入 Redis，只需通过 WS 广播即可
	// 前端收到后会自动刷新商品列表
	_ = payload
}

// extractEventUUID 从事件 payload 中提取 eventUUID 字段。
func extractEventUUID(raw json.RawMessage) string {
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return ""
	}
	if id, ok := m["eventId"].(string); ok {
		return id
	}
	return ""
}

// ack 确认消息处理完成。
func (c *Consumer) ack(ctx context.Context, messageID string) {
	if err := c.client.XAck(ctx, streamKey, consumerGroup, messageID).Err(); err != nil {
		log.Printf("[stream] ack 消息 %s 失败: %v", messageID, err)
	}
}
