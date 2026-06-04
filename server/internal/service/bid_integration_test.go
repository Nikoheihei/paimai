// go:build integration
// +build integration

package service

import (
	"context"
	"fmt"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
	redisclient "paimai/pkg/redis"
	"paimai/internal/stream"
	ws "paimai/internal/websocket"
)

// TestBidPersistenceIntegration 验证从 PlaceBid 到数据库落库的完整链路。
func TestBidPersistenceIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	redisClients, err := realRedis()
	if err != nil {
		t.Skipf("跳过集成测试（Redis 不可用）: %v", err)
	}
	defer redisClients.Close()

	ctx := context.Background()
	now := time.Now()

	auction := &model.Auction{
		RoomID:            1,
		ProductID:         1,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		CurrentPriceCents: 0,
		BidIncrementCents: 100,
		CapPriceCents:     1000,
		StartAt:           now,
		EndAt:             now.Add(5 * time.Minute),
		Status:            "running",
	}
	if err := db.Create(auction).Error; err != nil {
		t.Fatalf("创建测试竞拍失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(auction) })

	// Redis 热数据
	stateKey := fmt.Sprintf("auction:%d:state", auction.ID)
	redisClients.Master.HSet(ctx, stateKey,
		"status", "running",
		"currentPriceCents", 0,
		"leaderUserId", 0,
		"endAtUnixMilli", auction.EndAt.UnixMilli(),
		"mode", "sudden_death",
		"startPriceCents", 0,
		"bidIncrementCents", 100,
		"capPriceCents", 1000,
		"reservePriceCents", 0,
		"extendThresholdSec", 0,
		"extendDurationSec", 0,
	)

	publicStore := repository.NewGormPublicStore(db)
	streamPublisher := stream.NewPublisher(redisClients.Master)
	svc := NewPublicService(publicStore, nil, redisClients, streamPublisher, nil)

	result, err := svc.PlaceBid(ctx, auction.ID, BidInput{
		UserID:         10,
		AmountCents:    500,
		IdempotencyKey: "it-bid-persist-1",
		ClientTS:       now.UnixMilli(),
	})
	if err != nil {
		t.Fatalf("PlaceBid 失败: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("出价被拒绝")
	}

	var bids []model.Bid
	if err := db.Where("auction_id = ?", auction.ID).Find(&bids).Error; err != nil {
		t.Fatalf("查询出价失败: %v", err)
	}
	if len(bids) == 0 {
		t.Fatal("预期有出价记录，实际为 0")
	}
	if bids[0].AmountCents != 500 {
		t.Fatalf("出价金额 = %d，预期 500", bids[0].AmountCents)
	}

	var updated model.Auction
	if err := db.First(&updated, auction.ID).Error; err != nil {
		t.Fatalf("查询竞拍失败: %v", err)
	}
	if updated.CurrentPriceCents != 500 {
		t.Fatalf("当前价 = %d，预期 500", updated.CurrentPriceCents)
	}
	if *updated.WinnerUserID != 10 {
		t.Fatalf("领先用户 = %d，预期 10", *updated.WinnerUserID)
	}
	// 验证 Redis 热数据已更新
	stateKey = fmt.Sprintf("auction:%d:state", auction.ID)
	redisState, hgetErr := redisClients.Master.HGetAll(ctx, stateKey).Result()
	if hgetErr != nil {
		t.Fatalf("获取 Redis 竞拍状态失败: %v", hgetErr)
	}
	if redisState["currentPriceCents"] != "500" {
		t.Errorf("Redis currentPriceCents = %q，预期 500", redisState["currentPriceCents"])
	}
	if redisState["status"] != "running" {
		t.Errorf("Redis status = %q，预期 running", redisState["status"])
	}
	if redisState["leaderUserId"] == "0" || redisState["leaderUserId"] == "" {
		t.Errorf("Redis leaderUserId 应为非 0（出价用户 10），实际 = %q", redisState["leaderUserId"])
	}

}

// TestWebSocketBroadcastIntegration 验证事件发布后 WebSocket 能收到广播消息。
func TestWebSocketBroadcastIntegration(t *testing.T) {
	redisClients, err := realRedis()
	if err != nil {
		t.Skipf("跳过集成测试（Redis 不可用）: %v", err)
	}
	defer redisClients.Close()

	hub := ws.NewHub()
	go hub.Run()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamConsumer := stream.NewConsumer(redisClients.Master, hub)
	go streamConsumer.Start(ctx)
	time.Sleep(300 * time.Millisecond)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/rooms/1/ws", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WS upgrade failed: %v", err)
			return
		}
		client := ws.NewClient(hub, 1, 99, conn)
		hub.Register(client)
		go client.WritePump()
		go client.ReadPump()
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/rooms/1/ws?userId=99"
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS 连接失败: %v", err)
	}
	defer wsConn.Close()

	publisher := stream.NewPublisher(redisClients.Master)
	payload, _ := json.Marshal(map[string]interface{}{
		"accepted": true,
		"amount":   500,
	})
	event := stream.Event{
		Type:      "bid.accepted",
		RoomID:    1,
		AuctionID: 1,
		Payload:   payload,
	}
	if err := publisher.Publish(ctx, event); err != nil {
		t.Fatalf("发布事件失败: %v", err)
	}

	wsConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, message, err := wsConn.ReadMessage()
	if err != nil {
		t.Fatalf("读取 WS 消息失败: %v", err)
	}

	var received struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(message, &received); err != nil {
		t.Fatalf("解析 WS 消息失败: %v", err)
	}
	if received.Type != "bid.accepted" {
		t.Fatalf("消息类型 = %s，预期 bid.accepted", received.Type)
	}

	// 验证 payload 字段
	var payloadData struct {
		Type     string `json:"type"`
		RoomID   uint64 `json:"roomId"`
		AuctionID uint64 `json:"auctionId"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(received.Data, &payloadData); err != nil {
		t.Fatalf("解析 WS data 失败: %v", err)
	}
	var bidPayload struct {
		Accepted bool `json:"accepted"`
		Amount   int64 `json:"amount"`
	}
	if err := json.Unmarshal(payloadData.Payload, &bidPayload); err != nil {
		t.Fatalf("解析出价 payload 失败: %v", err)
	}
	if !bidPayload.Accepted {
		t.Error("expected accepted=true in payload")
	}
	if bidPayload.Amount != 500 {
		t.Errorf("expected amount=500, got %d", bidPayload.Amount)
	}
}

// TestBidToWSFullPipeline 验证完整的出价→Stream→WS 广播全链路。
func TestBidToWSFullPipeline(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	redisClients, err := realRedis()
	if err != nil {
		t.Skipf("跳过集成测试（Redis 不可用）: %v", err)
	}
	defer redisClients.Close()

	hub := ws.NewHub()
	go hub.Run()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	streamConsumer := stream.NewConsumer(redisClients.Master, hub)
	go streamConsumer.Start(ctx)
	time.Sleep(300 * time.Millisecond)

	now := time.Now()
	auction := &model.Auction{
		RoomID:            1,
		ProductID:         1,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		CurrentPriceCents: 0,
		BidIncrementCents: 100,
		CapPriceCents:     1000,
		StartAt:           now,
		EndAt:             now.Add(5 * time.Minute),
		Status:            "running",
	}
	if err := db.Create(auction).Error; err != nil {
		t.Fatalf("创建测试竞拍失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(auction) })

	stateKey := fmt.Sprintf("auction:%d:state", auction.ID)
	redisClients.Master.HSet(ctx, stateKey,
		"status", "running",
		"currentPriceCents", 0,
		"leaderUserId", 0,
		"endAtUnixMilli", auction.EndAt.UnixMilli(),
		"mode", "sudden_death",
		"startPriceCents", 0,
		"bidIncrementCents", 100,
		"capPriceCents", 1000,
		"reservePriceCents", 0,
		"extendThresholdSec", 0,
		"extendDurationSec", 0,
	)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/rooms/1/ws", func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := ws.NewClient(hub, 1, 99, conn)
		hub.Register(client)
		go client.WritePump()
		go client.ReadPump()
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/rooms/1/ws?userId=99"
	wsConn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("WS 连接失败: %v", err)
	}
	defer wsConn.Close()

	publicStore := repository.NewGormPublicStore(db)
	publisher := stream.NewPublisher(redisClients.Master)
	svc := NewPublicService(publicStore, nil, redisClients, publisher, nil)

	result, err := svc.PlaceBid(ctx, auction.ID, BidInput{
		UserID:         99,
		AmountCents:    500,
		IdempotencyKey: "it-full-pipe-1",
		ClientTS:       now.UnixMilli(),
	})
	if err != nil {
		t.Fatalf("PlaceBid 失败: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("出价被拒绝")
	}

	wsConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, message, err := wsConn.ReadMessage()
	if err != nil {
		t.Fatalf("读取 WS 广播失败（链路不通）: %v", err)
	}

	var received struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(message, &received); err != nil {
		t.Fatalf("解析 WS 消息失败: %v", err)
	}
	if received.Type != "bid.accepted" {
		t.Fatalf("预期 bid.accepted，收到 %s", received.Type)
	}
}

// --- helpers ---

func realDB() (*gorm.DB, error) {
	dsn := getEnvOrDefault("TEST_MYSQL_DSN", "root:rootpassword@tcp(localhost:3308)/paimai?charset=utf8mb4&parseTime=True&loc=Local")
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	// 自动迁移表结构（集成测试需要保证表存在）
	if err := db.AutoMigrate(&model.Auction{}, &model.Bid{}, &model.Order{}, &model.Product{}, &model.User{}, &model.LiveRoom{}); err != nil {
		return nil, err
	}
	return db, nil
}

func realRedis() (*redisclient.Clients, error) {
	master := getEnvOrDefault("TEST_REDIS_MASTER", "localhost:6381")
	slave := getEnvOrDefault("TEST_REDIS_SLAVE", "localhost:6382")
	return redisclient.NewRedisClients(master, slave)
}

func getEnvOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
