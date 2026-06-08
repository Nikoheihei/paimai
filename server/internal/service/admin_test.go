package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
)

type adminStoreStub struct {
	products      map[uint64]*model.Product
	auctions      map[uint64]*model.Auction
	orders        map[uint64]*model.Order
	rooms         map[uint64]*model.LiveRoom
	users         map[uint64]*model.User
	outboxEvents  []model.OutboxEvent
	nextProductID uint64
	nextAuctionID uint64
	nextOrderID   uint64
	nextRoomID    uint64
}

// newAdminStoreStub 创建内存版后台数据仓储，供服务层单元测试隔离数据库使用。
func newAdminStoreStub() *adminStoreStub {
	return &adminStoreStub{
		products:      make(map[uint64]*model.Product),
		auctions:      make(map[uint64]*model.Auction),
		orders:        make(map[uint64]*model.Order),
		rooms:         make(map[uint64]*model.LiveRoom),
		users:         make(map[uint64]*model.User),
		nextProductID: 1,
		nextAuctionID: 1,
		nextOrderID:   1,
		nextRoomID:    1,
	}
}

// CreateProduct 在内存仓储中保存商品，并模拟数据库自增 ID。
func (s *adminStoreStub) CreateProduct(_ context.Context, product *model.Product) error {
	product.ID = s.nextProductID
	s.nextProductID++
	if product.Status == "" {
		product.Status = ProductStatusAvailable
	}
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
	existing, ok := s.auctions[auction.ID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if existing.Version != auction.Version {
		return errors.New("auction version conflict")
	}
	auction.Version++
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

// CreateOrder 在内存仓储中保存订单，并模拟自增 ID。
func (s *adminStoreStub) CreateOrder(_ context.Context, order *model.Order) error {
	order.ID = s.nextOrderID
	s.nextOrderID++
	cp := *order
	s.orders[order.ID] = &cp
	return nil
}

// GetOrder 从内存仓储中按 ID 查询订单。
func (s *adminStoreStub) GetOrder(_ context.Context, id uint64) (*model.Order, error) {
	order, ok := s.orders[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *order
	return &cp, nil
}

// GetOrderByAuction 从内存仓储中按竞拍 ID 查询关联订单。
func (s *adminStoreStub) GetOrderByAuction(_ context.Context, auctionID uint64) (*model.Order, error) {
	for _, order := range s.orders {
		if order.AuctionID == auctionID {
			cp := *order
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// UpdateOrder 在内存仓储中覆盖保存订单最新状态。
func (s *adminStoreStub) UpdateOrder(_ context.Context, order *model.Order) error {
	cp := *order
	s.orders[order.ID] = &cp
	return nil
}

// ListOrders 从内存仓储中返回所有订单列表。
func (s *adminStoreStub) ListOrders(_ context.Context) ([]model.Order, error) {
	orders := make([]model.Order, 0, len(s.orders))
	for _, order := range s.orders {
		orders = append(orders, *order)
	}
	return orders, nil
}

// ListRunningExpiredAuctions 从内存仓储中查询所有 running 但已过期的竞拍。
func (s *adminStoreStub) DeleteProduct(_ context.Context, id uint64) error {
	delete(s.products, id)
	return nil
}

func (s *adminStoreStub) CreateRoom(_ context.Context, room *model.LiveRoom) error {
	room.ID = s.nextRoomID
	s.nextRoomID++
	cp := *room
	s.rooms[room.ID] = &cp
	return nil
}

func (s *adminStoreStub) GetRoom(_ context.Context, id uint64) (*model.LiveRoom, error) {
	room, ok := s.rooms[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *room
	return &cp, nil
}

func (s *adminStoreStub) UpdateRoom(_ context.Context, room *model.LiveRoom) error {
	cp := *room
	s.rooms[room.ID] = &cp
	return nil
}

func (s *adminStoreStub) DeleteRoom(_ context.Context, id uint64) error {
	delete(s.rooms, id)
	return nil
}

func (s *adminStoreStub) ListRoomsBySeller(_ context.Context, sellerID uint64) ([]model.LiveRoom, error) {
	rooms := make([]model.LiveRoom, 0)
	for _, room := range s.rooms {
		if room.SellerID == sellerID {
			rooms = append(rooms, *room)
		}
	}
	return rooms, nil
}

func (s *adminStoreStub) GetUser(_ context.Context, id uint64) (*model.User, error) {
	user, ok := s.users[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *user
	return &cp, nil
}

func (s *adminStoreStub) ListOrdersBySeller(_ context.Context, sellerID uint64) ([]model.Order, error) {
	orders := make([]model.Order, 0)
	for _, order := range s.orders {
		if order.SellerID == sellerID {
			orders = append(orders, *order)
		}
	}
	return orders, nil
}

func (s *adminStoreStub) ListOrdersByBuyer(_ context.Context, buyerID uint64) ([]model.Order, error) {
	orders := make([]model.Order, 0)
	for _, order := range s.orders {
		if order.BuyerID == buyerID {
			orders = append(orders, *order)
		}
	}
	return orders, nil
}

func (s *adminStoreStub) UpdateOrderStatus(_ context.Context, id uint64, status string, paidAt *time.Time, addressID *uint64, addressSnapshot string) error {
	order, ok := s.orders[id]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if order.Status != "pending_payment" {
		return fmt.Errorf("order %d is not pending_payment, cannot update to %s", id, status)
	}
	order.Status = status
	if paidAt != nil {
		order.PaidAt = paidAt
	}
	if addressID != nil {
		order.AddressID = addressID
	}
	if addressSnapshot != "" {
		order.AddressSnapshot = addressSnapshot
	}
	return nil
}

func (s *adminStoreStub) ListExpiredPendingOrders(_ context.Context, before time.Time, limit int) ([]model.Order, error) {
	orders := make([]model.Order, 0)
	for _, order := range s.orders {
		if order.Status == "pending_payment" && order.CreatedAt.Before(before) {
			orders = append(orders, *order)
			if limit > 0 && len(orders) >= limit {
				break
			}
		}
	}
	return orders, nil
}

func (s *adminStoreStub) ListRunningExpiredAuctions(_ context.Context) ([]model.Auction, error) {
	auctions := make([]model.Auction, 0)
	for _, auction := range s.auctions {
		if auction.Status == "running" && auction.EndAt.Before(time.Now()) {
			auctions = append(auctions, *auction)
		}
	}
	return auctions, nil
}

// TestAdminServiceAuctionLifecycle 验证竞拍从创建、发布到启动的核心生命周期。
func (s *adminStoreStub) WithTx(_ context.Context, fn func(repository.AdminStore) error) error {
	return fn(s)
}

func (s *adminStoreStub) CreateBid(_ context.Context, bid *model.Bid) error {
	return nil
}

func (s *adminStoreStub) UpdateAuctionBidState(_ context.Context, auction *model.Auction) error {
	existing, ok := s.auctions[auction.ID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	if existing.Version != auction.Version {
		return errors.New("optimistic lock: version mismatch")
	}
	auction.Version++
	cp := *auction
	s.auctions[auction.ID] = &cp
	return nil
}

func (s *adminStoreStub) CreateOutboxEvent(_ context.Context, evt *model.OutboxEvent) error {
	cp := *evt
	for _, existing := range s.outboxEvents {
		if cp.EventUUID != "" && existing.EventUUID == cp.EventUUID {
			return nil
		}
	}
	if cp.ID == 0 {
		cp.ID = uint64(len(s.outboxEvents) + 1)
	}
	s.outboxEvents = append(s.outboxEvents, cp)
	return nil
}

func (s *adminStoreStub) PickPendingOutboxEvents(_ context.Context, limit int) ([]model.OutboxEvent, error) {
	return nil, nil
}

func (s *adminStoreStub) MarkOutboxEventDone(_ context.Context, id uint64) error {
	return nil
}

func (s *adminStoreStub) MarkOutboxEventFailed(_ context.Context, id uint64) error {
	return nil
}

func (s *adminStoreStub) UpdateProduct(_ context.Context, product *model.Product) error {
	existing, ok := s.products[product.ID]
	if !ok {
		return gorm.ErrRecordNotFound
	}
	existing.Name = product.Name
	existing.ImageURL = product.ImageURL
	existing.Description = product.Description
	existing.Status = product.Status
	return nil
}

func (s *adminStoreStub) UpdateProductStatus(_ context.Context, id uint64, status string) error {
	product, ok := s.products[id]
	if !ok {
		return nil
	}
	product.Status = status
	return nil
}

func (s *adminStoreStub) ListAuctionBids(_ context.Context, auctionID uint64, limit int) ([]model.Bid, error) {
	return nil, nil
}

func TestAdminServiceAuctionLifecycle(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)
	fixedNow := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedNow }

	product, err := svc.CreateProduct(ctx, 1, ProductInput{
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

func TestAdminServiceStartsDueScheduledAuctions(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)
	fixedNow := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixedNow }

	product, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "定时上架商品"})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	futureProduct, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "未来上架商品"})
	if err != nil {
		t.Fatalf("CreateProduct(future) error = %v", err)
	}
	dueStart := fixedNow.Add(-30 * time.Second)
	dueEnd := dueStart.Add(120 * time.Second)
	due, err := svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         product.ID,
		StartPriceCents:   1000,
		BidIncrementCents: 100,
		StartAt:           &dueStart,
		EndAt:             &dueEnd,
	})
	if err != nil {
		t.Fatalf("CreateAuction(due) error = %v", err)
	}
	if _, err := svc.PublishAuction(ctx, due.ID); err != nil {
		t.Fatalf("PublishAuction(due) error = %v", err)
	}

	futureStart := fixedNow.Add(1 * time.Minute)
	futureEnd := futureStart.Add(90 * time.Second)
	future, err := svc.CreateAuction(ctx, AuctionInput{
		RoomID:            10,
		ProductID:         futureProduct.ID,
		StartPriceCents:   1000,
		BidIncrementCents: 100,
		StartAt:           &futureStart,
		EndAt:             &futureEnd,
	})
	if err != nil {
		t.Fatalf("CreateAuction(future) error = %v", err)
	}
	if _, err := svc.PublishAuction(ctx, future.ID); err != nil {
		t.Fatalf("PublishAuction(future) error = %v", err)
	}

	count, err := svc.StartDueScheduledAuctions(ctx)
	if err != nil {
		t.Fatalf("StartDueScheduledAuctions() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 started auction, got %d", count)
	}

	started, err := svc.getAuction(ctx, due.ID)
	if err != nil {
		t.Fatalf("get due auction error = %v", err)
	}
	if started.Status != "running" {
		t.Fatalf("expected due auction running, got %s", started.Status)
	}
	if !started.StartAt.Equal(fixedNow) || !started.EndAt.Equal(fixedNow.Add(120*time.Second)) {
		t.Fatalf("unexpected due start/end: %s %s", started.StartAt, started.EndAt)
	}

	stillScheduled, err := svc.getAuction(ctx, future.ID)
	if err != nil {
		t.Fatalf("get future auction error = %v", err)
	}
	if stillScheduled.Status != "scheduled" {
		t.Fatalf("expected future auction scheduled, got %s", stillScheduled.Status)
	}
}

// TestAdminServiceRejectsInvalidReserveAuction 验证保留价模式缺少保留价时会被拒绝。
func TestAdminServiceRejectsInvalidReserveAuction(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewAdminService(store, nil)

	product, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "限量球鞋"})
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

	product, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "老茶饼"})
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

	product, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "孤品珠宝"})
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

	product, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "二手名表"})
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

	product, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "测试商品"})
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

	product, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "测试商品"})
	if err != nil {
		t.Fatalf("CreateProduct() error = %v", err)
	}
	secondProduct, err := svc.CreateProduct(ctx, 1, ProductInput{SellerID: 1, Name: "测试商品二"})
	if err != nil {
		t.Fatalf("CreateProduct(second) error = %v", err)
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
		ProductID:         secondProduct.ID,
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

	_, err := svc.CreateProduct(context.Background(), 1, ProductInput{
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
