//go:build integration
// +build integration

package service

import (
	"context"
	"testing"
	"time"

	"paimai/internal/model"
	"paimai/internal/repository"
)

// TestGetBuyerOrderOwnershipIntegration 验证买家A不能查看买家B的订单。
func TestGetBuyerOrderOwnershipIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	// 创建两个买家
	buyerA := &model.User{Nickname: "BuyerA_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyerA).Error; err != nil {
		t.Fatalf("创建买家A失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyerA) })

	buyerB := &model.User{Nickname: "BuyerB_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyerB).Error; err != nil {
		t.Fatalf("创建买家B失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyerB) })

	_, _, auction := setupSettleTestData(t, db, now, 500)
	settleSvc := NewSettleService(repository.NewGormAdminStore(db))
	adminStore := repository.NewGormAdminStore(db)
	publicStore := repository.NewGormPublicStore(db)
	publicSvc := NewPublicService(publicStore, adminStore, nil, nil, settleSvc)

	// 买家A出价并结算
	bid := &model.Bid{
		AuctionID:      auction.ID,
		UserID:         buyerA.ID,
		AmountCents:    500,
		IdempotencyKey: "pub-own-" + t.Name(),
	}
	if err := db.Create(bid).Error; err != nil {
		t.Fatalf("创建出价失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(bid) })

	// 结算
	result, err := settleSvc.SettleAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("SettleAuction() error = %v", err)
	}
	if result.OrderID == nil {
		t.Fatal("expected OrderID from settlement")
	}

	// 买家B 尝试查看买家A的订单
	_, err = publicSvc.GetBuyerOrder(ctx, *result.OrderID, buyerB.ID)
	if err == nil {
		t.Errorf("expected error when buyerB views buyerA's order")
	}
	// 验证是 ErrNotFound
	if err.Error() != "order does not belong to user" {
		t.Errorf("expected ownership error, got: %v", err)
	}

	// 修复 bug: update order buyer_id to buyerA
	db.Model(&model.Order{}).Where("id = ?", *result.OrderID).Update("buyer_id", buyerA.ID)

	// 买家A 正常查看自己的订单
	order, err := publicSvc.GetBuyerOrder(ctx, *result.OrderID, buyerA.ID)
	if err != nil {
		t.Fatalf("buyerA GetBuyerOrder() error = %v", err)
	}
	if order.ID != *result.OrderID {
		t.Errorf("order ID mismatch")
	}
}

// TestPayBuyerOrderOwnershipIntegration 验证买家A不能支付买家B的订单。
func TestPayBuyerOrderOwnershipIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	buyerA := &model.User{Nickname: "BuyerPayA_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyerA).Error; err != nil {
		t.Fatalf("创建买家A失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyerA) })

	buyerB := &model.User{Nickname: "BuyerPayB_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyerB).Error; err != nil {
		t.Fatalf("创建买家B失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyerB) })

	_, _, auction := setupSettleTestData(t, db, now, 500)
	settleSvc := NewSettleService(repository.NewGormAdminStore(db))
	adminStore := repository.NewGormAdminStore(db)
	publicStore := repository.NewGormPublicStore(db)
	publicSvc := NewPublicService(publicStore, adminStore, nil, nil, settleSvc)

	bid := &model.Bid{
		AuctionID:      auction.ID,
		UserID:         buyerA.ID,
		AmountCents:    500,
		IdempotencyKey: "pub-pay-" + t.Name(),
	}
	if err := db.Create(bid).Error; err != nil {
		t.Fatalf("创建出价失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(bid) })

	result, err := settleSvc.SettleAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("SettleAuction() error = %v", err)
	}
	if result.OrderID == nil {
		t.Fatal("expected OrderID")
	}

	// 修复 order buyer_id
	db.Model(&model.Order{}).Where("id = ?", *result.OrderID).Update("buyer_id", buyerA.ID)

	// 买家B尝试支付买家A的订单
	_, err = publicSvc.PayBuyerOrder(ctx, *result.OrderID, buyerB.ID)
	if err == nil {
		t.Errorf("expected error when buyerB pays buyerA's order")
	}
	if err.Error() != "order does not belong to user" {
		t.Errorf("expected ownership error, got: %v", err)
	}

	// 买家A 正常支付自己订单
	order, err := publicSvc.PayBuyerOrder(ctx, *result.OrderID, buyerA.ID)
	if err != nil {
		t.Fatalf("buyerA PayBuyerOrder() error = %v", err)
	}
	if order.Status != "paid" {
		t.Errorf("expected status 'paid', got '%s'", order.Status)
	}
}

// TestListBuyerOrdersOnlySelfIntegration 验证 ListBuyerOrders 只返回当前用户的订单。
func TestListBuyerOrdersOnlySelfIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()
	now := time.Now()

	buyerA := &model.User{Nickname: "BuyerListA_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyerA).Error; err != nil {
		t.Fatalf("创建买家A失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyerA) })

	buyerB := &model.User{Nickname: "BuyerListB_" + t.Name(), Role: "buyer"}
	if err := db.Create(buyerB).Error; err != nil {
		t.Fatalf("创建买家B失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(buyerB) })

	adminStore := repository.NewGormAdminStore(db)
	publicStore := repository.NewGormPublicStore(db)

	// 为 buyerA 和 buyerB 分别创建竞拍并结算
	// buyerA 的结算
	_, _, auctionA := setupSettleTestData(t, db, now.Add(-time.Hour), 500)

	// buyerA 出价
	bidA := &model.Bid{
		AuctionID:      auctionA.ID,
		UserID:         buyerA.ID,
		AmountCents:    500,
		IdempotencyKey: "pub-list-a-" + t.Name(),
	}
	if err := db.Create(bidA).Error; err != nil {
		t.Fatalf("创建出价A失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(bidA) })

	// buyerB 的结算
	_, _, auctionB := setupSettleTestData(t, db, now, 600)
	bidB := &model.Bid{
		AuctionID:      auctionB.ID,
		UserID:         buyerB.ID,
		AmountCents:    600,
		IdempotencyKey: "pub-list-b-" + t.Name(),
	}
	if err := db.Create(bidB).Error; err != nil {
		t.Fatalf("创建出价B失败: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(bidB) })

	settleSvc := NewSettleService(adminStore)

	// 结算 A
	resultA, err := settleSvc.SettleAuction(ctx, auctionA.ID)
	if err != nil {
		t.Fatalf("SettleAuction A error = %v", err)
	}
	if resultA.OrderID != nil {
		db.Model(&model.Order{}).Where("id = ?", *resultA.OrderID).Update("buyer_id", buyerA.ID)
	}

	// 结算 B
	resultB, err := settleSvc.SettleAuction(ctx, auctionB.ID)
	if err != nil {
		t.Fatalf("SettleAuction B error = %v", err)
	}
	if resultB.OrderID != nil {
		db.Model(&model.Order{}).Where("id = ?", *resultB.OrderID).Update("buyer_id", buyerB.ID)
	}

	publicSvc := NewPublicService(publicStore, adminStore, nil, nil, settleSvc)

	// buyerA 只看自己的订单
	ordersA, err := publicSvc.ListBuyerOrders(ctx, buyerA.ID)
	if err != nil {
		t.Fatalf("ListBuyerOrders A error = %v", err)
	}
	for _, o := range ordersA {
		if o.BuyerID != buyerA.ID {
			t.Errorf("buyerA's order list contains order belonging to buyer %d", o.BuyerID)
		}
	}

	// buyerB 只看自己的订单
	ordersB, err := publicSvc.ListBuyerOrders(ctx, buyerB.ID)
	if err != nil {
		t.Fatalf("ListBuyerOrders B error = %v", err)
	}
	for _, o := range ordersB {
		if o.BuyerID != buyerB.ID {
			t.Errorf("buyerB's order list contains order belonging to buyer %d", o.BuyerID)
		}
	}
}
