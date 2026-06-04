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

// TestAuctionStateMachineFullIntegration 验证完整状态机链路：draft → publish → start → settle_sold。
func TestAuctionStateMachineFullIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	seller, product, room := setupStateMachineTestData(t, db, now)
	adminStore := repository.NewGormAdminStore(db)
	adminSvc := NewAdminService(adminStore, nil)

	// Step 1: 创建竞拍（draft）
	auctionInput := AuctionInput{
		RoomID:            room.ID,
		ProductID:         product.ID,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		BidIncrementCents: 100,
		CapPriceCents:     10000,
	}
	auction, err := adminSvc.CreateAuction(ctx, auctionInput)
	if err != nil {
		t.Fatalf("CreateAuction() error = %v", err)
	}
	if auction.Status != "draft" {
		t.Errorf("expected status 'draft', got '%s'", auction.Status)
	}

	// Step 2: 发布（draft → scheduled）
	auction, err = adminSvc.PublishAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("PublishAuction() error = %v", err)
	}
	if auction.Status != "scheduled" {
		t.Errorf("expected status 'scheduled', got '%s'", auction.Status)
	}

	// Step 3: 开始（scheduled → running）
	input := StartAuctionInput{}
	auction, err = adminSvc.StartAuction(ctx, auction.ID, input)
	if err != nil {
		t.Fatalf("StartAuction() error = %v", err)
	}
	if auction.Status != "running" {
		t.Errorf("expected status 'running', got '%s'", auction.Status)
	}

	// Step 4: 直接写入出价记录（使用真实 PlaceBid 需要在真实 Redis 下跑）
	// 本测试侧重状态机链路，出价逻辑由 bid_integration_test.go 覆盖
	if err := db.Create(&model.Bid{
		AuctionID: auction.ID,
		UserID:    seller.ID,
		AmountCents: 500,
		IdempotencyKey: "admin-int-bid-" + t.Name(),
	}).Error; err != nil {
		t.Fatalf("创建出价失败: %v", err)
	}

	// 更新竞拍当前价格
	db.Model(auction).Update("current_price_cents", 500)

	// Step 5: 结算
	settleSvc := NewSettleService(adminStore)
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

	// Step 6: 验证终态不可变
	_, err = adminSvc.PublishAuction(ctx, auction.ID)
	if err == nil {
		t.Error("expected error publishing sold auction")
	}

	// 验证 DB 最终状态
	finalAuction, err := adminStore.GetAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("GetAuction() error = %v", err)
	}
	if finalAuction.Status != "sold" {
		t.Errorf("expected DB status 'sold', got '%s'", finalAuction.Status)
	}
}

// TestAuctionCancelIntegration 验证 running 竞拍取消。
func TestAuctionCancelIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	_, product, room := setupStateMachineTestData(t, db, now)
	adminStore := repository.NewGormAdminStore(db)
	adminSvc := NewAdminService(adminStore, nil)

	auctionInput := AuctionInput{
		RoomID:            room.ID,
		ProductID:         product.ID,
		Mode:              "sudden_death",
		StartPriceCents:   0,
		BidIncrementCents: 100,
		CapPriceCents:     10000,
	}
	auction, err := adminSvc.CreateAuction(ctx, auctionInput)
	if err != nil {
		t.Fatalf("CreateAuction() error = %v", err)
	}
	if _, err := adminSvc.PublishAuction(ctx, auction.ID); err != nil {
		t.Fatalf("PublishAuction() error = %v", err)
	}
	if _, err := adminSvc.StartAuction(ctx, auction.ID, StartAuctionInput{}); err != nil {
		t.Fatalf("StartAuction() error = %v", err)
	}

	// 取消
	_, err = adminSvc.CancelAuction(ctx, auction.ID, CancelAuctionInput{
		Reason: "集成测试取消",
	})
	if err != nil {
		t.Fatalf("CancelAuction() error = %v", err)
	}

	finalAuction, err := adminStore.GetAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("GetAuction() error = %v", err)
	}
	if finalAuction.Status != "cancelled" {
		t.Errorf("expected status 'cancelled', got '%s'", finalAuction.Status)
	}
	if finalAuction.CancelReason != "集成测试取消" {
		t.Errorf("expected cancel reason '集成测试取消', got '%s'", finalAuction.CancelReason)
	}
}

// TestAuctionInvalidTransitionsIntegration 验证非法状态迁移返回错误。
func TestAuctionInvalidTransitionsIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	_, product, room := setupStateMachineTestData(t, db, now)
	adminStore := repository.NewGormAdminStore(db)
	adminSvc := NewAdminService(adminStore, nil)

	// 创建 draft 竞拍
	input := AuctionInput{RoomID: room.ID, ProductID: product.ID, Mode: "sudden_death", StartPriceCents: 0, BidIncrementCents: 100, CapPriceCents: 10000}
	auction, err := adminSvc.CreateAuction(ctx, input)
	if err != nil {
		t.Fatalf("CreateAuction() error = %v", err)
	}

	// draft → start（非法）
	_, err = adminSvc.StartAuction(ctx, auction.ID, StartAuctionInput{})
	if err == nil {
		t.Error("expected error: draft cannot start directly")
	}

	// 正常发布
	if _, err := adminSvc.PublishAuction(ctx, auction.ID); err != nil {
		t.Fatalf("PublishAuction() error = %v", err)
	}

	// scheduled → publish（非法）
	_, err = adminSvc.PublishAuction(ctx, auction.ID)
	if err == nil {
		t.Error("expected error: scheduled cannot publish again")
	}

	// 正常开始
	if _, err := adminSvc.StartAuction(ctx, auction.ID, StartAuctionInput{}); err != nil {
		t.Fatalf("StartAuction() error = %v", err)
	}

	// running → publish（非法）
	_, err = adminSvc.PublishAuction(ctx, auction.ID)
	if err == nil {
		t.Error("expected error: running cannot publish")
	}

	// running → start（非法）
	_, err = adminSvc.StartAuction(ctx, auction.ID, StartAuctionInput{})
	if err == nil {
		t.Error("expected error: running cannot start again")
	}
}

// TestCreateProductAndListIntegration 验证创建商品后列表能查到。
func TestCreateProductAndListIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()

	seller := &model.User{Nickname: "ProductSeller_" + t.Name(), Role: "seller"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("创建商家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(seller) })

	adminStore := repository.NewGormAdminStore(db)
	adminSvc := NewAdminService(adminStore, nil)

	product, err := adminSvc.CreateProduct(ctx, seller.ID, ProductInput{Name: "集成测试商品_" + t.Name()})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	if product.ID == 0 {
		t.Fatal("expected product ID > 0")
	}

	products, err := adminSvc.ListProducts(ctx, &seller.ID)
	if err != nil {
		t.Fatalf("ListProducts() error = %v", err)
	}
	found := false
	for _, p := range products {
		if p.ID == product.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("created product not found in list")
	}
}

// setupStateMachineTestData 创建状态机测试所需的商家、商品、直播间。
func setupStateMachineTestData(t *testing.T, db *gorm.DB, now time.Time) (*model.User, *model.Product, *model.LiveRoom) {
	t.Helper()

	seller := &model.User{Nickname: "AdminTestSeller_" + t.Name(), Role: "seller"}
	if err := db.Create(seller).Error; err != nil {
		t.Fatalf("创建商家失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(seller) })

	product := &model.Product{SellerID: seller.ID, Name: "状态机测试商品_" + t.Name()}
	if err := db.Create(product).Error; err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(product) })

	room := &model.LiveRoom{SellerID: seller.ID, Title: "状态机测试直播间_" + t.Name(), Status: "live"}
	if err := db.Create(room).Error; err != nil {
		t.Fatalf("创建直播间失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(room) })

	return seller, product, room
}
