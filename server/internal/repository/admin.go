package repository

import (
	"fmt"
	"context"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
)

// AuctionFilter 是后台竞拍列表的筛选条件。
type AuctionFilter struct {
	RoomID *uint64
	Status string
}

// AdminStore 定义后台管理流程需要的数据访问能力。
type AdminStore interface {
	CreateProduct(ctx context.Context, product *model.Product) error
	ListProducts(ctx context.Context, sellerID *uint64) ([]model.Product, error)
	GetProduct(ctx context.Context, id uint64) (*model.Product, error)
	DeleteProduct(ctx context.Context, id uint64) error

	CreateAuction(ctx context.Context, auction *model.Auction) error
	GetAuction(ctx context.Context, id uint64) (*model.Auction, error)
	UpdateAuction(ctx context.Context, auction *model.Auction) error
	ListAuctions(ctx context.Context, filter AuctionFilter) ([]model.Auction, error)

	CreateRoom(ctx context.Context, room *model.LiveRoom) error
	GetRoom(ctx context.Context, id uint64) (*model.LiveRoom, error)
	UpdateRoom(ctx context.Context, room *model.LiveRoom) error
	ListRoomsBySeller(ctx context.Context, sellerID uint64) ([]model.LiveRoom, error)

	CreateOrder(ctx context.Context, order *model.Order) error
	GetOrder(ctx context.Context, id uint64) (*model.Order, error)
	GetOrderByAuction(ctx context.Context, auctionID uint64) (*model.Order, error)
	UpdateOrder(ctx context.Context, order *model.Order) error
	ListOrders(ctx context.Context) ([]model.Order, error)
	ListOrdersBySeller(ctx context.Context, sellerID uint64) ([]model.Order, error)
	ListRunningExpiredAuctions(ctx context.Context) ([]model.Auction, error)

	// Outbox 事件队列
	WithTx(ctx context.Context, fn func(AdminStore) error) error
	CreateOutboxEvent(ctx context.Context, evt *model.OutboxEvent) error

	// 出价事务（用于 PlaceBid 的 MySQL 事务内）
	CreateBid(ctx context.Context, bid *model.Bid) error
	UpdateAuctionBidState(ctx context.Context, auction *model.Auction) error
	PickPendingOutboxEvents(ctx context.Context, limit int) ([]model.OutboxEvent, error)
	MarkOutboxEventDone(ctx context.Context, id uint64) error
	MarkOutboxEventFailed(ctx context.Context, id uint64) error
}

// GormAdminStore 是基于 GORM 的 AdminStore 实现。
type GormAdminStore struct {
	db *gorm.DB
}

// NewGormAdminStore 创建 GORM 版本的数据访问对象。
// txGormAdminStore 是事务内使用的 AdminStore 实现，共享同一个 *gorm.DB（事务对象）。
type txGormAdminStore struct {
	db *gorm.DB
}

func NewGormAdminStore(db *gorm.DB) *GormAdminStore {
	return &GormAdminStore{db: db}
}

// WithTx 在事务中执行 fn。
func (s *GormAdminStore) WithTx(ctx context.Context, fn func(AdminStore) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := &txGormAdminStore{db: tx}
		return fn(txStore)
	})
}

func (s *txGormAdminStore) CreateBid(ctx context.Context, bid *model.Bid) error {
	return s.db.WithContext(ctx).Create(bid).Error
}

func (s *txGormAdminStore) UpdateAuctionBidState(ctx context.Context, auction *model.Auction) error {
	result := s.db.WithContext(ctx).
		Model(&model.Auction{}).
		Where("id = ? AND version = ?", auction.ID, auction.Version).
		Updates(map[string]interface{}{
			"current_price_cents": auction.CurrentPriceCents,
			"winner_user_id":      auction.WinnerUserID,
			"end_at":              auction.EndAt,
			"status":              auction.Status,
			"version":             gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("auction version conflict: id=%d version=%d", auction.ID, auction.Version)
	}
	return nil
}

func (s *txGormAdminStore) CreateOutboxEvent(ctx context.Context, evt *model.OutboxEvent) error {
	return s.db.WithContext(ctx).Create(evt).Error
}

func (s *txGormAdminStore) PickPendingOutboxEvents(ctx context.Context, limit int) ([]model.OutboxEvent, error) {
	var events []model.OutboxEvent
	if err := s.db.WithContext(ctx).Where("status = ?", "pending").Order("id ASC").Limit(limit).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (s *txGormAdminStore) MarkOutboxEventDone(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Model(&model.OutboxEvent{}).Where("id = ?", id).Update("status", "done").Error
}

func (s *txGormAdminStore) MarkOutboxEventFailed(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Model(&model.OutboxEvent{}).Where("id = ?", id).Update("status", "failed").Error
}

// txGormAdminStore 中需要实现的 AdminStore 其余方法（事务传播）
func (s *txGormAdminStore) CreateProduct(ctx context.Context, product *model.Product) error { return s.db.WithContext(ctx).Create(product).Error }
func (s *txGormAdminStore) ListProducts(ctx context.Context, sellerID *uint64) ([]model.Product, error) { var p []model.Product; q := s.db.WithContext(ctx); if sellerID != nil { q = q.Where("seller_id = ?", *sellerID) }; err := q.Order("id DESC").Find(&p).Error; return p, err }
func (s *txGormAdminStore) GetProduct(ctx context.Context, id uint64) (*model.Product, error) { var p model.Product; err := s.db.WithContext(ctx).First(&p, id).Error; return &p, err }
func (s *txGormAdminStore) DeleteProduct(ctx context.Context, id uint64) error { return s.db.WithContext(ctx).Delete(&model.Product{}, id).Error }
func (s *txGormAdminStore) CreateAuction(ctx context.Context, auction *model.Auction) error { return s.db.WithContext(ctx).Create(auction).Error }
func (s *txGormAdminStore) GetAuction(ctx context.Context, id uint64) (*model.Auction, error) { var a model.Auction; err := s.db.WithContext(ctx).First(&a, id).Error; return &a, err }
func (s *txGormAdminStore) UpdateAuction(ctx context.Context, auction *model.Auction) error { return s.db.WithContext(ctx).Save(auction).Error }
func (s *txGormAdminStore) ListAuctions(ctx context.Context, filter AuctionFilter) ([]model.Auction, error) { var a []model.Auction; q := s.db.WithContext(ctx); if filter.RoomID != nil { q = q.Where("room_id = ?", *filter.RoomID) }; if filter.Status != "" { q = q.Where("status = ?", filter.Status) }; err := q.Order("id DESC").Find(&a).Error; return a, err }
func (s *txGormAdminStore) CreateRoom(ctx context.Context, room *model.LiveRoom) error { return s.db.WithContext(ctx).Create(room).Error }
func (s *txGormAdminStore) GetRoom(ctx context.Context, id uint64) (*model.LiveRoom, error) { var r model.LiveRoom; err := s.db.WithContext(ctx).First(&r, id).Error; return &r, err }
func (s *txGormAdminStore) UpdateRoom(ctx context.Context, room *model.LiveRoom) error { return s.db.WithContext(ctx).Save(room).Error }
func (s *txGormAdminStore) ListRoomsBySeller(ctx context.Context, sellerID uint64) ([]model.LiveRoom, error) { var r []model.LiveRoom; err := s.db.WithContext(ctx).Where("seller_id = ?", sellerID).Order("id DESC").Find(&r).Error; return r, err }
func (s *txGormAdminStore) CreateOrder(ctx context.Context, order *model.Order) error { return s.db.WithContext(ctx).Create(order).Error }
func (s *txGormAdminStore) GetOrder(ctx context.Context, id uint64) (*model.Order, error) { var o model.Order; err := s.db.WithContext(ctx).First(&o, id).Error; return &o, err }
func (s *txGormAdminStore) GetOrderByAuction(ctx context.Context, auctionID uint64) (*model.Order, error) { var o model.Order; err := s.db.WithContext(ctx).Where("auction_id = ?", auctionID).First(&o).Error; return &o, err }
func (s *txGormAdminStore) UpdateOrder(ctx context.Context, order *model.Order) error { return s.db.WithContext(ctx).Save(order).Error }
func (s *txGormAdminStore) ListOrders(ctx context.Context) ([]model.Order, error) { var o []model.Order; err := s.db.WithContext(ctx).Order("id DESC").Find(&o).Error; return o, err }
func (s *txGormAdminStore) ListOrdersBySeller(ctx context.Context, sellerID uint64) ([]model.Order, error) { var o []model.Order; err := s.db.WithContext(ctx).Where("seller_id = ?", sellerID).Order("id DESC").Find(&o).Error; return o, err }
func (s *txGormAdminStore) ListRunningExpiredAuctions(ctx context.Context) ([]model.Auction, error) { var a []model.Auction; err := s.db.WithContext(ctx).Where("status = ? AND end_at <= ?", "running", time.Now()).Order("id ASC").Find(&a).Error; return a, err }
func (s *txGormAdminStore) WithTx(ctx context.Context, fn func(AdminStore) error) error { panic("nested transaction not supported") }


func (s *GormAdminStore) CreateProduct(ctx context.Context, product *model.Product) error {
	return s.db.WithContext(ctx).Create(product).Error
}

func (s *GormAdminStore) ListProducts(ctx context.Context, sellerID *uint64) ([]model.Product, error) {
	var products []model.Product
	query := s.db.WithContext(ctx).Order("id DESC")
	if sellerID != nil {
		query = query.Where("seller_id = ?", *sellerID)
	}
	if err := query.Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

func (s *GormAdminStore) GetProduct(ctx context.Context, id uint64) (*model.Product, error) {
	var product model.Product
	if err := s.db.WithContext(ctx).First(&product, id).Error; err != nil {
		return nil, err
	}
	return &product, nil
}

func (s *GormAdminStore) DeleteProduct(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Delete(&model.Product{}, id).Error
}

func (s *GormAdminStore) CreateAuction(ctx context.Context, auction *model.Auction) error {
	return s.db.WithContext(ctx).Create(auction).Error
}

func (s *GormAdminStore) GetAuction(ctx context.Context, id uint64) (*model.Auction, error) {
	var auction model.Auction
	if err := s.db.WithContext(ctx).First(&auction, id).Error; err != nil {
		return nil, err
	}
	return &auction, nil
}

func (s *GormAdminStore) UpdateAuction(ctx context.Context, auction *model.Auction) error {
	return s.db.WithContext(ctx).Save(auction).Error
}

func (s *GormAdminStore) ListAuctions(ctx context.Context, filter AuctionFilter) ([]model.Auction, error) {
	var auctions []model.Auction
	query := s.db.WithContext(ctx).Order("id DESC")
	if filter.RoomID != nil {
		query = query.Where("room_id = ?", *filter.RoomID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	if err := query.Find(&auctions).Error; err != nil {
		return nil, err
	}
	return auctions, nil
}

func (s *GormAdminStore) CreateRoom(ctx context.Context, room *model.LiveRoom) error {
	return s.db.WithContext(ctx).Create(room).Error
}

func (s *GormAdminStore) GetRoom(ctx context.Context, id uint64) (*model.LiveRoom, error) {
	var room model.LiveRoom
	if err := s.db.WithContext(ctx).First(&room, id).Error; err != nil {
		return nil, err
	}
	return &room, nil
}

func (s *GormAdminStore) UpdateRoom(ctx context.Context, room *model.LiveRoom) error {
	return s.db.WithContext(ctx).Save(room).Error
}

func (s *GormAdminStore) ListRoomsBySeller(ctx context.Context, sellerID uint64) ([]model.LiveRoom, error) {
	var rooms []model.LiveRoom
	if err := s.db.WithContext(ctx).Where("seller_id = ?", sellerID).Order("id DESC").Find(&rooms).Error; err != nil {
		return nil, err
	}
	return rooms, nil
}

func (s *GormAdminStore) CreateOrder(ctx context.Context, order *model.Order) error {
	return s.db.WithContext(ctx).Create(order).Error
}

func (s *GormAdminStore) GetOrder(ctx context.Context, id uint64) (*model.Order, error) {
	var order model.Order
	if err := s.db.WithContext(ctx).First(&order, id).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func (s *GormAdminStore) GetOrderByAuction(ctx context.Context, auctionID uint64) (*model.Order, error) {
	var order model.Order
	if err := s.db.WithContext(ctx).Where("auction_id = ?", auctionID).First(&order).Error; err != nil {
		return nil, err
	}
	return &order, nil
}

func (s *GormAdminStore) UpdateOrder(ctx context.Context, order *model.Order) error {
	return s.db.WithContext(ctx).Save(order).Error
}

func (s *GormAdminStore) ListOrders(ctx context.Context) ([]model.Order, error) {
	var orders []model.Order
	if err := s.db.WithContext(ctx).Order("id DESC").Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

func (s *GormAdminStore) ListOrdersBySeller(ctx context.Context, sellerID uint64) ([]model.Order, error) {
	var orders []model.Order
	if err := s.db.WithContext(ctx).Where("seller_id = ?", sellerID).Order("id DESC").Find(&orders).Error; err != nil {
		return nil, err
	}
	return orders, nil
}

func (s *GormAdminStore) ListRunningExpiredAuctions(ctx context.Context) ([]model.Auction, error) {
	var auctions []model.Auction
	if err := s.db.WithContext(ctx).
		Where("status = ? AND end_at <= ?", "running", time.Now()).
		Order("id ASC").
		Find(&auctions).Error; err != nil {
		return nil, err
	}
	return auctions, nil
}

func (s *GormAdminStore) CreateBid(ctx context.Context, bid *model.Bid) error {
	return s.db.WithContext(ctx).Create(bid).Error
}

func (s *GormAdminStore) UpdateAuctionBidState(ctx context.Context, auction *model.Auction) error {
	result := s.db.WithContext(ctx).
		Model(&model.Auction{}).
		Where("id = ? AND version = ?", auction.ID, auction.Version).
		Updates(map[string]interface{}{
			"current_price_cents": auction.CurrentPriceCents,
			"winner_user_id":      auction.WinnerUserID,
			"end_at":              auction.EndAt,
			"status":              auction.Status,
			"version":             gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("auction version conflict: id=%d version=%d", auction.ID, auction.Version)
	}
	return nil
}

func (s *GormAdminStore) CreateOutboxEvent(ctx context.Context, evt *model.OutboxEvent) error {
	return s.db.WithContext(ctx).Create(evt).Error
}

func (s *GormAdminStore) PickPendingOutboxEvents(ctx context.Context, limit int) ([]model.OutboxEvent, error) {
	var events []model.OutboxEvent
	if err := s.db.WithContext(ctx).
		Where("status = ?", "pending").
		Order("id ASC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

func (s *GormAdminStore) MarkOutboxEventDone(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Model(&model.OutboxEvent{}).Where("id = ?", id).Update("status", "done").Error
}

func (s *GormAdminStore) MarkOutboxEventFailed(ctx context.Context, id uint64) error {
	return s.db.WithContext(ctx).Model(&model.OutboxEvent{}).Where("id = ?", id).Update("status", "failed").Error
}
