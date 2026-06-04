//go:build integration
// +build integration

package service

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
)

// TestSettleAuctionCreatesOrderIntegration 验证 running 竞拍有出价时结算生成 pending_payment 订单。
func TestSettleAuctionCreatesOrderIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	seller, buyer, auction := setupSettleTestData(t, db, now, 500)

	settleSvc := NewSettleService(repository.NewGormAdminStore(db))

	result, err := settleSvc.SettleAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("SettleAuction() error = %v", err)
	}
	if !result.Settled {
		t.Fatal("expected Settled=true")
	}
	if result.Status != "sold" {
		t.Errorf("expected status 'sold', got '%s'", result.Status)
	}
	if result.OrderID == nil {
		t.Fatal("expected OrderID to be set")
	}
	if result.FinalPriceCents != 500 {
		t.Errorf("expected FinalPriceCents=500, got %d", result.FinalPriceCents)
	}

	order, err := settleSvc.adminStore.GetOrder(ctx, *result.OrderID)
	if err != nil {
		t.Fatalf("GetOrder() error = %v", err)
	}
	if order.AuctionID != auction.ID {
		t.Errorf("order AuctionID mismatch: %d vs %d", order.AuctionID, auction.ID)
	}
	if order.BuyerID != buyer.ID {
		t.Errorf("order BuyerID mismatch: %d vs %d", order.BuyerID, buyer.ID)
	}
	if order.SellerID != seller.ID {
		t.Errorf("order SellerID mismatch: %d vs %d", order.SellerID, seller.ID)
	}
	if order.FinalPriceCents != 500 {
		t.Errorf("order FinalPriceCents expected 500, got %d", order.FinalPriceCents)
	}
	if order.Status != "pending_payment" {
		t.Errorf("expected order status 'pending_payment', got '%s'", order.Status)
	}

	// 验证竞拍状态
	updatedAuction, err := settleSvc.adminStore.GetAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("GetAuction() error = %v", err)
	}
	if updatedAuction.Status != "sold" {
		t.Errorf("expected auction status 'sold', got '%s'", updatedAuction.Status)
	}
}

// TestSettleAuctionNoBidIntegration 验证无出价竞拍结算为 failed，无订单。
func TestSettleAuctionNoBidIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	_, _, auction := setupSettleTestData(t, db, now, 0)

	settleSvc := NewSettleService(repository.NewGormAdminStore(db))

	result, err := settleSvc.SettleAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("SettleAuction() error = %v", err)
	}
	if !result.Settled {
		t.Fatal("expected Settled=true")
	}
	if result.Status != "failed" {
		t.Errorf("expected status 'failed', got '%s'", result.Status)
	}
	if result.OrderID != nil {
		t.Error("expected OrderID to be nil for no-bid auction")
	}

	updatedAuction, err := settleSvc.adminStore.GetAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("GetAuction() error = %v", err)
	}
	if updatedAuction.Status != "failed" {
		t.Errorf("expected auction status 'failed', got '%s'", updatedAuction.Status)
	}
}

// TestSettleAuctionIdempotentIntegration 验证已结算竞拍再次结算返回幂等结果。
func TestSettleAuctionIdempotentIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	_, _, auction := setupSettleTestData(t, db, now, 500)

	settleSvc := NewSettleService(repository.NewGormAdminStore(db))

	result1, err := settleSvc.SettleAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("第一次 SettleAuction() error = %v", err)
	}
	if !result1.Settled {
		t.Fatal("expected Settled=true on first call")
	}

	result2, err := settleSvc.SettleAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("第二次 SettleAuction() error = %v", err)
	}
	if result2.Settled {
		t.Error("expected Settled=false on idempotent call")
	}
	if result2.OrderID == nil || *result2.OrderID != *result1.OrderID {
		t.Error("expected same OrderID on idempotent call")
	}
}

// TestPayOrderIntegration 验证支付后订单状态变更。
func TestPayOrderIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	_, _, auction := setupSettleTestData(t, db, now, 500)

	settleSvc := NewSettleService(repository.NewGormAdminStore(db))

	result, err := settleSvc.SettleAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("SettleAuction() error = %v", err)
	}

	order, err := settleSvc.PayOrder(ctx, *result.OrderID)
	if err != nil {
		t.Fatalf("PayOrder() error = %v", err)
	}
	if order.Status != "paid" {
		t.Errorf("expected order status 'paid', got '%s'", order.Status)
	}

	// 幂等
	order2, err := settleSvc.PayOrder(ctx, *result.OrderID)
	if err != nil {
		t.Fatalf("PayOrder() idempotent error = %v", err)
	}
	if order2.Status != "paid" {
		t.Errorf("expected 'paid' on idempotent call, got '%s'", order2.Status)
	}
}

// TestSettleCancelledAuctionIntegration 验证已取消竞拍结算直接跳过。
func TestSettleCancelledAuctionIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	_, _, auction := setupSettleTestData(t, db, now, 500)

	settleSvc := NewSettleService(repository.NewGormAdminStore(db))

	auction.Status = "cancelled"
	if err := db.Save(auction).Error; err != nil {
		t.Fatalf("取消竞拍失败: %v", err)
	}

	result, err := settleSvc.SettleAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("SettleAuction() error = %v", err)
	}
	if result.Settled {
		t.Error("expected Settled=false for cancelled auction")
	}
	if result.Status != "cancelled" {
		t.Errorf("expected status 'cancelled', got '%s'", result.Status)
	}
}

// TestListSellerOrdersMultiTenantIntegration 验证多商家订单隔离。
func TestListSellerOrdersMultiTenantIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	seller1, buyer1, auction1 := setupSettleTestData(t, db, now, 300)
	seller2, buyer2, auction2 := setupSettleTestData(t, db, now, 500)

	settleSvc := NewSettleService(repository.NewGormAdminStore(db))

	if _, err := settleSvc.SettleAuction(ctx, auction1.ID); err != nil {
		t.Fatalf("SettleAuction 1 error = %v", err)
	}
	if _, err := settleSvc.SettleAuction(ctx, auction2.ID); err != nil {
		t.Fatalf("SettleAuction 2 error = %v", err)
	}

	orders1, err := settleSvc.adminStore.ListOrdersBySeller(ctx, seller1.ID)
	if err != nil {
		t.Fatalf("ListOrdersBySeller 1 error = %v", err)
	}
	if len(orders1) != 1 {
		t.Fatalf("seller1 expected 1 order, got %d", len(orders1))
	}
	if orders1[0].BuyerID != buyer1.ID {
		t.Errorf("seller1 order BuyerID mismatch")
	}

	orders2, err := settleSvc.adminStore.ListOrdersBySeller(ctx, seller2.ID)
	if err != nil {
		t.Fatalf("ListOrdersBySeller 2 error = %v", err)
	}
	if len(orders2) != 1 {
		t.Fatalf("seller2 expected 1 order, got %d", len(orders2))
	}
	if orders2[0].BuyerID != buyer2.ID {
		t.Errorf("seller2 order BuyerID mismatch")
	}
}

// setupSettleTestData 创建结算测试所需的商家、买家、商品、直播间、竞拍和出价。
// 返回 (seller, buyer, auction)。
func setupSettleTestData(t *testing.T, db *gorm.DB, now time.Time, bidAmount int64) (*model.User, *model.User, *model.Auction) {
	t.Helper()

	ts := now.UnixNano()

	seller := &model.User{Nickname: "Seller_" + t.Name(), Role: "seller"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("创建商家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(seller) })

	buyer := &model.User{Nickname: "Buyer_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyer).Error; err != nil {
		t.Fatalf("创建买家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyer) })

	product := &model.Product{SellerID: seller.ID, Name: "集成测试商品_" + t.Name()}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(product) })

	room := &model.LiveRoom{SellerID: seller.ID, Title: "集成测试直播间_" + t.Name(), Status: "live"}
	if err := db.Create(room).Error; err != nil {
		t.Fatalf("创建直播间失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(room) })

	auction := &model.Auction{
		RoomID:            room.ID,
		ProductID:         product.ID,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		CurrentPriceCents: bidAmount,
		BidIncrementCents: 100,
		CapPriceCents:     ts % 100000,
		StartAt:           now.Add(-10 * time.Minute),
		EndAt:             now.Add(-1 * time.Minute),
		Status:            "running",
		Version:           1,
	}

	if bidAmount > 0 {
		auction.WinnerUserID = &buyer.ID
	}
	if err := db.Create(auction).Error; err != nil {
		t.Fatalf("创建竞拍失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(auction) })

	if bidAmount > 0 {
		bid := &model.Bid{
			AuctionID:         auction.ID,
			UserID:            buyer.ID,
			AmountCents:       bidAmount,
			IdempotencyKey:    "settle-int-" + t.Name(),
		}
		if err := db.Create(bid).Error; err != nil {
			t.Fatalf("创建出价记录失败: %v", err)
		}
		t.Cleanup(func() { db.Unscoped().Delete(bid) })
	}

	return seller, buyer, auction
}
