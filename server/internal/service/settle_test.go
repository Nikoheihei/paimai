package service

import (
	"context"
	"errors"
	"testing"
	"time"


	"paimai/internal/model"
	"paimai/internal/statemachine"
)

// TestSettleAuctionNoBids 验证无人出价时结算结果为流拍。
func TestSettleAuctionNoBids(t *testing.T) {
	svc, store := newSettleTestHarness()
	auctionID := seedRunningAuction(store, nil, nil)

	result, err := svc.SettleAuction(context.Background(), auctionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Settled {
		t.Fatal("expected settled=true")
	}
	if result.Status != string(statemachine.StateFailed) {
		t.Fatalf("expected status=failed, got %s", result.Status)
	}
	if result.OrderID != nil {
		t.Fatal("expected no order for no-bid auction")
	}
}

// TestSettleAuctionWithWinnerNoReserve 验证有赢家且无保留价时成交并生成订单。
func TestSettleAuctionWithWinnerNoReserve(t *testing.T) {
	svc, store := newSettleTestHarness()
	winner := uint64(42)
	auctionID := seedRunningAuction(store, &winner, nil)

	result, err := svc.SettleAuction(context.Background(), auctionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Settled {
		t.Fatal("expected settled=true")
	}
	if result.Status != string(statemachine.StateSold) {
		t.Fatalf("expected status=sold, got %s", result.Status)
	}
	if result.OrderID == nil {
		t.Fatal("expected order ID for sold auction")
	}
	if result.FinalPriceCents != 1000 {
		t.Fatalf("expected finalPrice=1000, got %d", result.FinalPriceCents)
	}

	// 验证订单已持久化
	order, err := store.GetOrderByAuction(context.Background(), auctionID)
	if err != nil {
		t.Fatalf("order not found: %v", err)
	}
	if order.BuyerID != winner {
		t.Fatalf("expected buyer %d, got %d", winner, order.BuyerID)
	}
	if order.Status != "pending_payment" {
		t.Fatalf("expected pending_payment, got %s", order.Status)
	}
}

// TestSettleAuctionReserveNotMet 验证出价未达保留价时流拍。
func TestSettleAuctionReserveNotMet(t *testing.T) {
	svc, store := newSettleTestHarness()
	winner := uint64(42)
	reserve := int64(5000)
	auction := &model.Auction{
		RoomID:            1,
		ProductID:         1,
		Mode:              "sudden_death",
		Status:            string(statemachine.StateRunning),
		CurrentPriceCents: 1000,
		WinnerUserID:      &winner,
		ReservePriceCents: &reserve,
		StartAt:           time.Now().Add(-10 * time.Minute),
		EndAt:             time.Now().Add(-1 * time.Minute),
	}
	_ = store.CreateAuction(context.Background(), auction)

	result, err := svc.SettleAuction(context.Background(), auction.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Settled {
		t.Fatal("expected settled=true")
	}
	if result.Status != string(statemachine.StateFailed) {
		t.Fatalf("expected status=failed for reserve not met, got %s", result.Status)
	}
	if result.OrderID != nil {
		t.Fatal("expected no order when reserve not met")
	}
}

// TestSettleAuctionIdempotent 验证已成交的竞拍重复结算幂等返回。
func TestSettleAuctionIdempotent(t *testing.T) {
	svc, store := newSettleTestHarness()
	winner := uint64(42)
	auctionID := seedRunningAuction(store, &winner, nil)

	// 第一次结算
	result1, err := svc.SettleAuction(context.Background(), auctionID)
	if err != nil {
		t.Fatalf("first settle failed: %v", err)
	}
	if !result1.Settled {
		t.Fatal("first settle should be true")
	}

	// 第二次结算（幂等）
	result2, err := svc.SettleAuction(context.Background(), auctionID)
	if err != nil {
		t.Fatalf("second settle failed: %v", err)
	}
	if result2.Settled {
		t.Fatal("second settle should be false (idempotent)")
	}
	if result2.Status != string(statemachine.StateSold) {
		t.Fatalf("expected status=sold, got %s", result2.Status)
	}
	if result2.OrderID == nil || *result2.OrderID != *result1.OrderID {
		t.Fatal("idempotent settle should return same order")
	}
}

// TestSettleAlreadyFailedIdempotent 验证已流拍的竞拍幂等返回。
func TestSettleAlreadyFailedIdempotent(t *testing.T) {
	svc, store := newSettleTestHarness()
	auctionID := seedRunningAuction(store, nil, nil)
	// 先结算为流拍
	_, _ = svc.SettleAuction(context.Background(), auctionID)

	// 再次结算
	result, err := svc.SettleAuction(context.Background(), auctionID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Settled {
		t.Fatal("already failed auction should not settle again")
	}
	if result.Status != string(statemachine.StateFailed) {
		t.Fatalf("expected status=failed, got %s", result.Status)
	}
}

// TestSettleAuctionNotFound 验证不存在的竞拍返回 ErrNotFound。
func TestSettleAuctionNotFound(t *testing.T) {
	svc, _ := newSettleTestHarness()
	_, err := svc.SettleAuction(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestPayOrder 验证订单从 pending_payment 支付为 paid。
func TestPayOrder(t *testing.T) {
	svc, store := newSettleTestHarness()
	order := seedOrder(store, "pending_payment")

	paid, err := svc.PayOrder(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("pay failed: %v", err)
	}
	if paid.Status != "paid" {
		t.Fatalf("expected status=paid, got %s", paid.Status)
	}
	if paid.PaidAt == nil {
		t.Fatal("expected PaidAt to be set")
	}
}

// TestPayOrderAlreadyPaid 验证已支付的订单幂等返回成功。
func TestPayOrderAlreadyPaid(t *testing.T) {
	svc, store := newSettleTestHarness()
	now := time.Now()
	order := seedOrder(store, "paid")
	order.PaidAt = &now
	_ = store.UpdateOrder(context.Background(), order)

	paid, err := svc.PayOrder(context.Background(), order.ID)
	if err != nil {
		t.Fatalf("pay idempotent failed: %v", err)
	}
	if paid.Status != "paid" {
		t.Fatalf("expected status=paid, got %s", paid.Status)
	}
}

// TestPayOrderClosed 验证已关闭的订单不允许支付。
func TestPayOrderClosed(t *testing.T) {
	svc, store := newSettleTestHarness()
	order := seedOrder(store, "closed")

	_, err := svc.PayOrder(context.Background(), order.ID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

// TestPayOrderNotFound 验证不存在的订单返回 ErrNotFound。
func TestPayOrderNotFound(t *testing.T) {
	svc, _ := newSettleTestHarness()
	_, err := svc.PayOrder(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestSettleExpiredAuctions 验证启动时结算所有过期 running 竞拍。
func TestSettleExpiredAuctions(t *testing.T) {
	svc, store := newSettleTestHarness()
	now := time.Now()

	// 确保 Room 存在
	_ = store.CreateRoom(context.Background(), &model.LiveRoom{
		ID: 1, SellerID: 100, Title: "测试直播间", Status: "live",
	})

	// 创建一个过期的 running 竞拍
	winner := uint64(42)
	expiredAuction := &model.Auction{
		RoomID:            1,
		ProductID:         1,
		Mode:              "sudden_death",
		Status:            string(statemachine.StateRunning),
		CurrentPriceCents: 2000,
		WinnerUserID:      &winner,
		StartAt:           now.Add(-20 * time.Minute),
		EndAt:             now.Add(-5 * time.Minute),
	}
	_ = store.CreateAuction(context.Background(), expiredAuction)

	// 创建一个未过期的 running 竞拍（不应该被结算）
	freshAuction := &model.Auction{
		RoomID:            1,
		ProductID:         2,
		Mode:              "sudden_death",
		Status:            string(statemachine.StateRunning),
		CurrentPriceCents: 500,
		StartAt:           now,
		EndAt:             now.Add(10 * time.Minute),
	}
	_ = store.CreateAuction(context.Background(), freshAuction)

	count, err := svc.SettleExpiredAuctions(context.Background())
	if err != nil {
		t.Fatalf("SettleExpiredAuctions failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 settled auction, got %d", count)
	}

	// 验证过期竞拍已结算
	auction, _ := store.GetAuction(context.Background(), expiredAuction.ID)
	if auction.Status != string(statemachine.StateSold) {
		t.Fatalf("expected expired auction to be sold, got %s", auction.Status)
	}

	// 验证未过期竞拍还是 running
	fresh, _ := store.GetAuction(context.Background(), freshAuction.ID)
	if fresh.Status != string(statemachine.StateRunning) {
		t.Fatalf("expected fresh auction to still be running, got %s", fresh.Status)
	}
}

// --- 测试辅助函数 ---

// newSettleTestHarness 创建结算服务及其底层内存存储，用于结算相关的单元测试。
func newSettleTestHarness() (*SettleService, *adminStoreStub) {
	store := newAdminStoreStub()
	svc := NewSettleService(store)
	svc.now = func() time.Time {
		return time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	}
	return svc, store
}

// seedRunningAuction 在内存仓储中插入一个 running 状态的竞拍。
func seedRunningAuction(store *adminStoreStub, winnerUserID *uint64, reservePrice *int64) uint64 {
	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)
	// 确保 Room 1 存在（SettleAuction 需要查 Room 获取 SellerID）
	if _, err := store.GetRoom(context.Background(), 1); err != nil {
		_ = store.CreateRoom(context.Background(), &model.LiveRoom{
			ID: 1, SellerID: 100, Title: "测试直播间", Status: "live",
		})
	}
	auction := &model.Auction{
		RoomID:            1,
		ProductID:         1,
		Mode:              "sudden_death",
		Status:            string(statemachine.StateRunning),
		StartPriceCents:    0,
		CurrentPriceCents: 1000,
		BidIncrementCents: 100,
		CapPriceCents:     10000,
		ReservePriceCents: reservePrice,
		WinnerUserID:      winnerUserID,
		StartAt:           now.Add(-10 * time.Minute),
		EndAt:             now.Add(-1 * time.Minute),
	}
	_ = store.CreateAuction(context.Background(), auction)
	return auction.ID
}

// seedOrder 在内存仓储中插入一笔订单，用于支付相关测试。
func seedOrder(store *adminStoreStub, status string) *model.Order {
	order := &model.Order{
		AuctionID:       1,
		ProductID:       1,
		BuyerID:         42,
		SellerID:        1,
		FinalPriceCents: 5000,
		Status:          status,
	}
	_ = store.CreateOrder(context.Background(), order)
	return order
}
