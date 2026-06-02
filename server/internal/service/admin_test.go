package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
)

type adminStoreStub struct {
	products      map[uint64]*model.Product
	auctions      map[uint64]*model.Auction
	nextProductID uint64
	nextAuctionID uint64
}

// newAdminStoreStub 创建内存版后台数据仓储，供服务层单元测试隔离数据库使用。
func newAdminStoreStub() *adminStoreStub {
	return &adminStoreStub{
		products:      make(map[uint64]*model.Product),
		auctions:      make(map[uint64]*model.Auction),
		nextProductID: 1,
		nextAuctionID: 1,
	}
}

// CreateProduct 在内存仓储中保存商品，并模拟数据库自增 ID。
func (s *adminStoreStub) CreateProduct(_ context.Context, product *model.Product) error {
	product.ID = s.nextProductID
	s.nextProductID++
	cp := *product
	s.products[product.ID] = &cp
	return nil
}

// ListProducts 从内存仓储中查询商品列表，并支持按卖家 ID 过滤。
func (s *adminStoreStub) ListProducts(_ context.Context, sellerID *uint64) ([]model.Product, error) {
	products := make([]model.Product, 0, len(s.products))
	for _, product := range s.products {
		if sellerID == nil || product.SellerID == *sellerID {
			products = append(products, *product)
		}
	}
	return products, nil
}

// GetProduct 从内存仓储中按 ID 查询商品，不存在时模拟 GORM 未找到错误。
func (s *adminStoreStub) GetProduct(_ context.Context, id uint64) (*model.Product, error) {
	product, ok := s.products[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *product
	return &cp, nil
}

// CreateAuction 在内存仓储中保存竞拍，并模拟数据库自增 ID。
func (s *adminStoreStub) CreateAuction(_ context.Context, auction *model.Auction) error {
	auction.ID = s.nextAuctionID
	s.nextAuctionID++
	cp := *auction
	s.auctions[auction.ID] = &cp
	return nil
}

// GetAuction 从内存仓储中按 ID 查询竞拍，不存在时模拟 GORM 未找到错误。
func (s *adminStoreStub) GetAuction(_ context.Context, id uint64) (*model.Auction, error) {
	auction, ok := s.auctions[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *auction
	return &cp, nil
}

// UpdateAuction 在内存仓储中覆盖保存竞拍最新状态。
func (s *adminStoreStub) UpdateAuction(_ context.Context, auction *model.Auction) error {
	cp := *auction
	s.auctions[auction.ID] = &cp
	return nil
}

// ListAuctions 从内存仓储中查询竞拍列表，并支持直播间和状态过滤。
func (s *adminStoreStub) ListAuctions(_ context.Context, filter repository.AuctionFilter) ([]model.Auction, error) {
	auctions := make([]model.Auction, 0, len(s.auctions))
	for _, auction := range s.auctions {
		if filter.RoomID != nil && auction.RoomID != *filter.RoomID {
			continue
		}
		if filter.Status != "" && auction.Status != filter.Status {
			continue
		}
		auctions = append(auctions, *auction)
	}
	return auctions, nil
}

// TestAdminServiceAuctionLifecycle 验证竞拍从创建、发布到启动的核心生命周期。
func TestAdminServiceAuctionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)
	fixedNow := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedNow }

	product, err := svc.CreateProduct(ctx, ProductInput{
		SellerID: 1,
		Name:     "翡翠手镯",
	})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}

	auction, err := svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         product.ID,
		Mode:              AuctionModeSuddenDeath,
		StartPriceCents:   10000,
		BidIncrementCents: 1000,
	})
	if err != nil {
		t.Fatalf("CreateAuction() error = %v", err)
	}
	if auction.Status != "draft" {
		t.Fatalf("expected draft auction, got %s", auction.Status)
	}

	auction, err = svc.PublishAuction(ctx, auction.ID)
	if err != nil {
		t.Fatalf("PublishAuction() error = %v", err)
	}
	if auction.Status != "scheduled" {
		t.Fatalf("expected scheduled auction, got %s", auction.Status)
	}

	auction, err = svc.StartAuction(ctx, auction.ID, StartAuctionInput{DurationSec: 90})
	if err != nil {
		t.Fatalf("StartAuction() error = %v", err)
	}
	if auction.Status != "running" {
		t.Fatalf("expected running auction, got %s", auction.Status)
	}
	if !auction.StartAt.Equal(fixedNow) || !auction.EndAt.Equal(fixedNow.Add(90*time.Second)) {
		t.Fatalf("unexpected start/end time: %s %s", auction.StartAt, auction.EndAt)
	}

	if _, err := svc.UpdateAuction(ctx, auction.ID, AuctionPatchInput{BidIncrementCents: int64Ptr(2000)}); err != ErrAuctionNotEditable {
		t.Fatalf("expected ErrAuctionNotEditable, got %v", err)
	}
}

// TestAdminServiceRejectsInvalidReserveAuction 验证保留价模式缺少保留价时会被拒绝。
func TestAdminServiceRejectsInvalidReserveAuction(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)

	product, err := svc.CreateProduct(ctx, ProductInput{SellerID: 1, Name: "限量球鞋"})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}

	_, err = svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         product.ID,
		Mode:              AuctionModeReserve,
		StartPriceCents:   0,
		BidIncrementCents: 100,
	})
	if err == nil {
		t.Fatal("expected reserve auction without reserve price to fail")
	}
}

// TestAdminServiceUpdatesDraftAuctionRules 验证草稿竞拍可以修改规则并同步当前价。
func TestAdminServiceUpdatesDraftAuctionRules(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)

	product, err := svc.CreateProduct(ctx, ProductInput{SellerID: 1, Name: "老茶饼"})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	auction, err := svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         product.ID,
		StartPriceCents:   1000,
		BidIncrementCents: 100,
	})
	if err != nil {
		t.Fatalf("CreateAuction() error = %v", err)
	}

	reservePrice := int64(5000)
	mode := AuctionModeReserve
	auction, err = svc.UpdateAuction(ctx, auction.ID, AuctionPatchInput{
		Mode:              &mode,
		StartPriceCents:   int64Ptr(2000),
		BidIncrementCents: int64Ptr(200),
		ReservePriceCents: &reservePrice,
	})
	if err != nil {
		t.Fatalf("UpdateAuction() error = %v", err)
	}
	if auction.Mode != AuctionModeReserve {
		t.Fatalf("expected reserve mode, got %s", auction.Mode)
	}
	if auction.CurrentPriceCents != 2000 {
		t.Fatalf("expected current price to follow start price, got %d", auction.CurrentPriceCents)
	}
	if auction.ReservePriceCents == nil || *auction.ReservePriceCents != reservePrice {
		t.Fatalf("unexpected reserve price: %v", auction.ReservePriceCents)
	}
}

// TestAdminServiceCancelLifecycle 验证竞拍取消后进入终态且不能再次发布。
func TestAdminServiceCancelLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)

	product, err := svc.CreateProduct(ctx, ProductInput{SellerID: 1, Name: "孤品珠宝"})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	auction, err := svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         product.ID,
		StartPriceCents:   10000,
		BidIncrementCents: 500,
	})
	if err != nil {
		t.Fatalf("CreateAuction() error = %v", err)
	}

	auction, err = svc.CancelAuction(ctx, auction.ID, CancelAuctionInput{Reason: "商品瑕疵待确认"})
	if err != nil {
		t.Fatalf("CancelAuction() error = %v", err)
	}
	if auction.Status != "cancelled" {
		t.Fatalf("expected cancelled auction, got %s", auction.Status)
	}
	if auction.CancelReason != "商品瑕疵待确认" {
		t.Fatalf("unexpected cancel reason: %s", auction.CancelReason)
	}

	_, err = svc.PublishAuction(ctx, auction.ID)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition after cancellation, got %v", err)
	}
}

// TestAdminServiceRejectsInvalidExtensionAuction 验证延时模式缺少延时参数时会被拒绝。
func TestAdminServiceRejectsInvalidExtensionAuction(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)

	product, err := svc.CreateProduct(ctx, ProductInput{SellerID: 1, Name: "二手名表"})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}

	_, err = svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         product.ID,
		Mode:              AuctionModeExtension,
		StartPriceCents:   10000,
		BidIncrementCents: 1000,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for missing extension settings, got %v", err)
	}
}

// TestAdminServiceRejectsUnsupportedAuctionMode 验证未知竞拍模式会被服务层拒绝。
func TestAdminServiceRejectsUnsupportedAuctionMode(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)

	product, err := svc.CreateProduct(ctx, ProductInput{SellerID: 1, Name: "测试商品"})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}

	_, err = svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         product.ID,
		Mode:              "lottery",
		StartPriceCents:   100,
		BidIncrementCents: 10,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for unsupported mode, got %v", err)
	}
}

// TestAdminServiceListFilters 验证竞拍列表按直播间和状态过滤的行为。
func TestAdminServiceListFilters(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)

	product, err := svc.CreateProduct(ctx, ProductInput{SellerID: 1, Name: "测试商品"})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	first, err := svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         product.ID,
		StartPriceCents:   100,
		BidIncrementCents: 10,
	})
	if err != nil {
		t.Fatalf("CreateAuction(first) error = %v", err)
	}
	second, err := svc.CreateAuction(ctx, AuctionInput{
		RoomID:            20,
		ProductID:         product.ID,
		StartPriceCents:   200,
		BidIncrementCents: 20,
	})
	if err != nil {
		t.Fatalf("CreateAuction(second) error = %v", err)
	}
	if _, err := svc.PublishAuction(ctx, second.ID); err != nil {
		t.Fatalf("PublishAuction(second) error = %v", err)
	}

	roomID := uint64(10)
	roomAuctions, err := svc.ListAuctions(ctx, repository.AuctionFilter{RoomID: &roomID})
	if err != nil {
		t.Fatalf("ListAuctions(room) error = %v", err)
	}
	if len(roomAuctions) != 1 || roomAuctions[0].ID != first.ID {
		t.Fatalf("unexpected room filtered auctions: %+v", roomAuctions)
	}

	scheduled, err := svc.ListAuctions(ctx, repository.AuctionFilter{Status: "scheduled"})
	if err != nil {
		t.Fatalf("ListAuctions(status) error = %v", err)
	}
	if len(scheduled) != 1 || scheduled[0].ID != second.ID {
		t.Fatalf("unexpected status filtered auctions: %+v", scheduled)
	}
}

// TestAdminServiceRejectsInvalidProductInput 验证商品名称为空白时会被拒绝。
func TestAdminServiceRejectsInvalidProductInput(t *testing.T) {
	svc := NewAdminService(newAdminStoreStub(), nil)

	_, err := svc.CreateProduct(context.Background(), ProductInput{
		SellerID: 1,
		Name:     "   ",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for blank product name, got %v", err)
	}
}

// int64Ptr 将 int64 值转成指针，方便测试构造可选字段。
func int64Ptr(v int64) *int64 {
	return &v
}
