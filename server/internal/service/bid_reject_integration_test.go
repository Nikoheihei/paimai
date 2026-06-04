//go:build integration
// +build integration

package service

import (
	"context"
	"testing"
	"time"

	"paimai/internal/model"
	"paimai/internal/repository"
	"paimai/internal/stream"
)

// TestBidRejectedByAuctionNotRunningIntegration 验证 non-running 竞拍返回错误。
func TestBidRejectedByAuctionNotRunningIntegration(t *testing.T) {
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

	seller := &model.User{Nickname: "NR_Seller_" + t.Name(), Role: "seller"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("创建商家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(seller) })

	buyer := &model.User{Nickname: "NR_Buyer_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyer).Error; err != nil {
		t.Fatalf("创建买家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyer) })

	product := &model.Product{SellerID: seller.ID, Name: "NR_Test_" + t.Name()}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(product) })

	room := &model.LiveRoom{SellerID: seller.ID, Title: "NR_Room_" + t.Name(), Status: "live"}
	if err := db.Create(room).Error; err != nil {
		t.Fatalf("创建直播间失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(room) })

	// 创建 draft 状态竞拍
	auction := &model.Auction{
		RoomID:            room.ID,
		ProductID:         product.ID,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		CurrentPriceCents: 0,
		BidIncrementCents: 100,
		CapPriceCents:     10000,
		StartAt:           time.Now(),
		EndAt:             time.Now().Add(30 * time.Minute),
		Status:            "draft",
		Version:           1,
	}
	if err := db.Create(auction).Error; err != nil {
		t.Fatalf("创建竞拍失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(auction) })

	publicStore := repository.NewGormPublicStore(db)
	publisher := stream.NewPublisher(redisClients.Master)
	svc := NewPublicService(publicStore, nil, redisClients, publisher, nil)

	_, err = svc.PlaceBid(ctx, auction.ID, BidInput{
		UserID:         buyer.ID,
		AmountCents:    500,
		IdempotencyKey: "not-running-" + t.Name(),
	})
	if err == nil {
		t.Fatal("expected error for draft auction")
	}
	if err.Error() != "bid engine unavailable" {
		t.Errorf("expected 'bid engine unavailable', got: %v", err)
	}
}

// TestBidRejectedByReservePriceIntegration 验证出价低于保留价被拒绝。
func TestBidRejectedByReservePriceIntegration(t *testing.T) {
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

	seller := &model.User{Nickname: "RP_Seller_" + t.Name(), Role: "seller"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("创建商家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(seller) })

	buyer := &model.User{Nickname: "RP_Buyer_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyer).Error; err != nil {
		t.Fatalf("创建买家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyer) })

	product := &model.Product{SellerID: seller.ID, Name: "RP_Test_" + t.Name()}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(product) })

	room := &model.LiveRoom{SellerID: seller.ID, Title: "RP_Room_" + t.Name(), Status: "live"}
	if err := db.Create(room).Error; err != nil {
		t.Fatalf("创建直播间失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(room) })

	reservePrice := int64(1000)
	auction := &model.Auction{
		RoomID:            room.ID,
		ProductID:         product.ID,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		CurrentPriceCents: 0,
		BidIncrementCents: 100,
		CapPriceCents:     10000,
		ReservePriceCents: &reservePrice,
		StartAt:           time.Now().Add(-5 * time.Minute),
		EndAt:             time.Now().Add(30 * time.Minute),
		Status:            "running",
		Version:           1,
	}
	if err := db.Create(auction).Error; err != nil {
		t.Fatalf("创建竞拍失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(auction) })

	publicStore := repository.NewGormPublicStore(db)
	publisher := stream.NewPublisher(redisClients.Master)
	svc := NewPublicService(publicStore, nil, redisClients, publisher, nil)

	result, err := svc.PlaceBid(ctx, auction.ID, BidInput{
		UserID:         buyer.ID,
		AmountCents:    500,
		IdempotencyKey: "reserve-" + t.Name(),
	})
	if err != nil {
		t.Fatalf("PlaceBid() error = %v", err)
	}
	if result.Accepted {
		t.Error("expected bid rejected (below reserve price)")
	}
	if result.ReserveMet {
		t.Error("expected ReserveMet=false")
	}

	var bidCount int64
	db.Model(&model.Bid{}).Where("idempotency_key = ?", "reserve-"+t.Name()).Count(&bidCount)
	if bidCount > 0 {
		t.Errorf("expected 0 bid records for rejected bid, got %d", bidCount)
	}
}

// TestBidIdempotencyIntegration 验证同一 IdempotencyKey 出价两次，DB 只有一条出价记录。
func TestBidIdempotencyIntegration(t *testing.T) {
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

	seller := &model.User{Nickname: "ID_Seller_" + t.Name(), Role: "seller"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("创建商家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(seller) })

	product := &model.Product{SellerID: seller.ID, Name: "ID_Test_" + t.Name()}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(product) })

	room := &model.LiveRoom{SellerID: seller.ID, Title: "ID_Room_" + t.Name(), Status: "live"}
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
		CapPriceCents:     10000,
		StartAt:           time.Now().Add(-5 * time.Minute),
		EndAt:             time.Now().Add(30 * time.Minute),
		Status:            "running",
		Version:           1,
	}
	if err := db.Create(auction).Error; err != nil {
		t.Fatalf("创建竞拍失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(auction) })

	publicStore := repository.NewGormPublicStore(db)
	publisher := stream.NewPublisher(redisClients.Master)
	svc := NewPublicService(publicStore, nil, redisClients, publisher, nil)

	key := "idem-" + t.Name()
	_, err = svc.PlaceBid(ctx, auction.ID, BidInput{
		UserID:         seller.ID,
		AmountCents:    500,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("第一次 PlaceBid() error = %v", err)
	}

	var count1 int64
	db.Model(&model.Bid{}).Where("idempotency_key = ?", key).Count(&count1)

	result, err := svc.PlaceBid(ctx, auction.ID, BidInput{
		UserID:         seller.ID,
		AmountCents:    500,
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("第二次 PlaceBid() error = %v", err)
	}
	if !result.IdempotentReplay {
		t.Error("expected IdempotentReplay=true on second bid")
	}

	var count2 int64
	db.Model(&model.Bid{}).Where("idempotency_key = ?", key).Count(&count2)
	if count2 != count1 {
		t.Errorf("expected %d bid records after replay, got %d", count1, count2)
	}
}
