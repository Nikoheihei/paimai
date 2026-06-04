//go:build integration
// +build integration

package stream

import (
	"context"
	"os"
	"testing"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
)

// TestOutboxPollerIntegration 验证 OutboxPoller 将 pending 事件发布到 Redis Stream 并标记 done。
func TestOutboxPollerIntegration(t *testing.T) {
	db, redisClient, streamKey := setupOutboxIntegration(t)
	ctx := context.Background()

	adminStore := repository.NewGormAdminStore(db)

	// 写一个 pending outbox 事件
	evt := &model.OutboxEvent{
		EventType: "bid.accepted",
		Payload:   `{"auctionId":1,"userId":1,"amount":500,"eventId":"outbox-int-test-1"}`,
		Status:    "pending",
		EventUUID: "outbox-int-uuid-1-" + t.Name(),
	}
	if err := db.Create(evt).Error; err != nil {
		t.Fatalf("创建 outbox 事件失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(evt) })

	// 创建 Poller 并执行一次轮询
	poller := NewOutboxPoller(adminStore, redisClient)
	poller.streamKey = streamKey
	poller.pollOnce(ctx)

	// 验证 outbox 事件已被标记为 done
	var updated model.OutboxEvent
	if err := db.First(&updated, evt.ID).Error; err != nil {
		t.Fatalf("查询 outbox 事件失败: %v", err)
	}
	if updated.Status != "done" {
		t.Errorf("expected status 'done', got '%s'", updated.Status)
	}

	// 验证 Redis Stream 中存在该事件
	streamEvents, err := redisClient.XRange(ctx, streamKey, "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange 错误: %v", err)
	}
	if len(streamEvents) == 0 {
		t.Fatal("expected at least 1 event in Redis Stream")
	}

	found := false
	for _, se := range streamEvents {
		if payload, ok := se.Values["payload"]; ok {
			if payload == evt.Payload {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("outbox event payload not found in Redis Stream")
	}

	// 清理 Stream
	redisClient.Del(ctx, streamKey)
}

// TestOutboxPollerSkipDoneIntegration 验证已 done 的事件不会重复 XAdd。
func TestOutboxPollerSkipDoneIntegration(t *testing.T) {
	db, redisClient, streamKey := setupOutboxIntegration(t)
	ctx := context.Background()

	adminStore := repository.NewGormAdminStore(db)

	// 写入一个已经 done 的事件
	evt := &model.OutboxEvent{
		EventType: "bid.accepted",
		Payload:   `{"auctionId":2,"eventId":"outbox-done-test"}`,
		Status:    "done",
		EventUUID: "outbox-done-uuid-" + t.Name(),
	}
	if err := db.Create(evt).Error; err != nil {
		t.Fatalf("创建已 done outbox 事件失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(evt) })

	poller := NewOutboxPoller(adminStore, redisClient)
	poller.streamKey = streamKey
	poller.pollOnce(ctx)

	// 验证 Stream 中没有任何事件
	streamEvents, err := redisClient.XLen(ctx, streamKey).Result()
	if err != nil {
		t.Fatalf("XLen 错误: %v", err)
	}
	if streamEvents > 0 {
		t.Errorf("expected 0 events in stream for already-done events, got %d", streamEvents)
	}
}

// setupOutboxIntegration 创建 MySQL + Redis 连接，返回 db, redisClient, streamKey。
func setupOutboxIntegration(t *testing.T) (*gorm.DB, *goredis.Client, string) {
	t.Helper()

	dsn := getEnvOrDefault("TEST_MYSQL_DSN", "root:rootpassword@tcp(localhost:3308)/paimai?charset=utf8mb4&parseTime=True&loc=Local")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	if err := db.AutoMigrate(&model.OutboxEvent{}); err != nil {
		t.Skipf("AutoMigrate 失败: %v", err)
	}

	addr := getEnvOrDefault("TEST_REDIS_MASTER", "localhost:6381")
	rdb := goredis.NewClient(&goredis.Options{Addr: addr})
	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		t.Skipf("跳过集成测试（Redis 不可用）: %v", err)
	}
	t.Cleanup(func() { rdb.Close() })

	streamKey := "test:outbox:int:" + t.Name()
	t.Cleanup(func() { rdb.Del(context.Background(), streamKey) })

	return db, rdb, streamKey
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
