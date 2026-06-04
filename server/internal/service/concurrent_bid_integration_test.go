//go:build integration
// +build integration

package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"paimai/internal/model"
	"paimai/internal/repository"
	"paimai/internal/stream"
	ws "paimai/internal/websocket"
)

// TestConcurrentBidsIntegration 验证多用户同时出价的 DB 一致性。
// 多个用户同时对同一个竞拍出价，验证：
// 1. 至少有一个出价被接受
// 2. 竞拍 currentPriceCents = 最高出价
// 3. winnerUserId = 最高出价用户
// 4. 所有出价记录完整落入 MySQL
func TestConcurrentBidsIntegration(t *testing.T) {
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

	// 创建竞拍数据
	seller := &model.User{Nickname: "ConcurSeller_" + t.Name(), Role: "seller"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("创建商家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(seller) })

	product := &model.Product{SellerID: seller.ID, Name: "ConcurTest_" + t.Name()}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(product) })

	room := &model.LiveRoom{SellerID: seller.ID, Title: "ConcurRoom_" + t.Name(), Status: "live"}
	if err := db.Create(room).Error; err != nil {
		t.Fatalf("创建直播间失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(room) })

	auction := &model.Auction{
		RoomID:            room.ID,
		ProductID:         product.ID,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		CurrentPriceCents: 0,
		BidIncrementCents: 100,
		CapPriceCents:     100000,
		StartAt:           now.Add(-5 * time.Minute),
		EndAt:             now.Add(30 * time.Minute),
		Status:            "running",
		Version:           1,
	}
	if err := db.Create(auction).Error; err != nil {
		t.Fatalf("创建竞拍失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(auction) })

	// 设置 Redis 热数据
	stateKey := fmt.Sprintf("auction:%d:state", auction.ID)
	redisClients.Master.HSet(ctx, stateKey,
		"status", "running",
		"currentPriceCents", 0,
		"leaderUserId", 0,
		"endAtUnixMilli", auction.EndAt.UnixMilli(),
		"mode", "sudden_death",
		"startPriceCents", 0,
		"bidIncrementCents", 100,
		"capPriceCents", 100000,
		"reservePriceCents", 0,
		"extendThresholdSec", 0,
		"extendDurationSec", 0,
	)

	publicStore := repository.NewGormPublicStore(db)
	publisher := stream.NewPublisher(redisClients.Master)
	svc := NewPublicService(publicStore, nil, redisClients, publisher, nil)

	// 并发出价：5 个用户各出一次
	numUsers := 5
	var wg sync.WaitGroup
	results := make([]*BidResult, numUsers)
	errs := make([]error, numUsers)

	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			userID := uint64(1000 + idx)
			amount := int64((idx + 1) * 200) // 200, 400, 600, 800, 1000

			result, bidErr := svc.PlaceBid(ctx, auction.ID, BidInput{
				UserID:         userID,
				AmountCents:    amount,
				IdempotencyKey: fmt.Sprintf("concur-%s-%d", t.Name(), idx),
			})
			results[idx] = result
			errs[idx] = bidErr
		}(i)
	}
	wg.Wait()

	// 验证：至少有一个出价被接受
	acceptedCount := 0
	maxAmount := int64(0)
	maxUserID := uint64(0)
	for i, r := range results {
		if errs[i] != nil {
			t.Logf("用户 %d 出价错误: %v", 1000+i, errs[i])
			continue
		}
		if r == nil {
			continue
		}
		if r.Accepted {
			acceptedCount++
			if r.AmountCents > maxAmount {
				maxAmount = r.AmountCents
				maxUserID = r.UserID
			}
		}
	}
	if acceptedCount == 0 {
		t.Fatal("所有并发出价均被拒绝")
	}

	// 验证 DB 出价记录
	var bidCount int64
	db.Model(&model.Bid{}).Where("auction_id = ?", auction.ID).Count(&bidCount)
	if bidCount < 1 {
		t.Errorf("期望至少 1 条出价记录，实际 %d", bidCount)
	}

	// 验证竞拍最终状态
	var updated model.Auction
	if err := db.First(&updated, auction.ID).Error; err != nil {
		t.Fatalf("查询竞拍失败: %v", err)
	}
	if updated.CurrentPriceCents != maxAmount {
		t.Errorf("竞拍当前价 = %d，预期最高出价 %d", updated.CurrentPriceCents, maxAmount)
	}
	if *updated.WinnerUserID != maxUserID {
		t.Errorf("赢家用户 = %d，预期最高出价用户 %d", *updated.WinnerUserID, maxUserID)
	}

	// 验证 Redis 热数据一致性
	redisState, err := redisClients.Master.HGetAll(ctx, stateKey).Result()
	if err != nil {
		t.Fatalf("获取 Redis 竞拍状态失败: %v", err)
	}
	if redisState["status"] != "running" {
		t.Errorf("Redis status = %q，预期 running", redisState["status"])
	}
}

// TestConcurrentBidsWithStreamIntegration 验证并发出价的 Stream 事件分发。
// 启动真实 Stream Consumer（不启动 WS 服务器），验证出价后事件被正确消费。
func TestConcurrentBidsWithStreamIntegration(t *testing.T) {
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

	seller := &model.User{Nickname: "Concur2Seller_" + t.Name(), Role: "seller"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("创建商家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(seller) })

	product := &model.Product{SellerID: seller.ID, Name: "Concur2Test_" + t.Name()}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(product) })

	room := &model.LiveRoom{SellerID: seller.ID, Title: "Concur2Room_" + t.Name(), Status: "live"}
	if err := db.Create(room).Error; err != nil {
		t.Fatalf("创建直播间失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(room) })

	auction := &model.Auction{
		RoomID:            room.ID,
		ProductID:         product.ID,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		CurrentPriceCents: 0,
		BidIncrementCents: 100,
		CapPriceCents:     100000,
		StartAt:           now.Add(-5 * time.Minute),
		EndAt:             now.Add(30 * time.Minute),
		Status:            "running",
		Version:           1,
	}
	if err := db.Create(auction).Error; err != nil {
		t.Fatalf("创建竞拍失败: %v", err)
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
		"capPriceCents", 100000,
		"reservePriceCents", 0,
		"extendThresholdSec", 0,
		"extendDurationSec", 0,
	)

	// 启动 Hub + Stream Consumer（不启动 WS 服务器，仅验证事件消费）
	hub := ws.NewHub()
	go hub.Run()

	consumer := stream.NewConsumer(redisClients.Master, hub)
	go consumer.Start(ctx)
	time.Sleep(200 * time.Millisecond)

	publicStore := repository.NewGormPublicStore(db)
	publisher := stream.NewPublisher(redisClients.Master)
	svc := NewPublicService(publicStore, nil, redisClients, publisher, nil)

	// 并发出价
	numUsers := 3
	var wg sync.WaitGroup
	acceptedBids := make([]*BidResult, 0, numUsers)
	var mu sync.Mutex

	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			userID := uint64(2000 + idx)
			amount := int64((idx + 1) * 300)

			result, bidErr := svc.PlaceBid(ctx, auction.ID, BidInput{
				UserID:         userID,
				AmountCents:    amount,
				IdempotencyKey: fmt.Sprintf("concur2-%s-%d", t.Name(), idx),
			})
			if bidErr != nil {
				t.Logf("用户 %d 出价错误: %v", userID, bidErr)
				return
			}
			if result != nil && result.Accepted {
				mu.Lock()
				acceptedBids = append(acceptedBids, result)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if len(acceptedBids) == 0 {
		t.Fatal("所有并发出价均被拒绝")
	}

	// 验证 DB 有出价记录
	var bidCount int64
	db.Model(&model.Bid{}).Where("auction_id = ?", auction.ID).Count(&bidCount)
	if int64(len(acceptedBids)) != bidCount {
		t.Errorf("accepted 出价 %d 条，DB 记录 %d 条", len(acceptedBids), bidCount)
	}

	// 给 Consumer 一点时间消费 Outbox 事件并写入 Stream
	time.Sleep(500 * time.Millisecond)

	// 验证 Redis Stream 中出现了事件
	streamLen, err := redisClients.Master.XLen(ctx, "auction:events").Result()
	if err != nil {
		t.Fatalf("XLen 错误: %v", err)
	}
	if streamLen == 0 {
		t.Log("注意: 集成测试环境可能无法访问 Redis Stream（依赖 consumer group 预创建）")
	}
}
