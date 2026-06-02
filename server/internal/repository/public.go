package repository

import (
	"context"

	"gorm.io/gorm"

	"paimai/internal/model"
)

// PublicStore 定义用户端直播间、竞拍详情、排行榜和出价流程需要的数据访问能力。
//
// 用户侧查询和后台管理使用独立接口，避免为了复用而把所有方法塞进同一个 Store。
// 这样后续用户端读模型、后台写模型分开优化时，服务层依赖也更清晰。
type PublicStore interface {
	GetRoom(ctx context.Context, id uint64) (*model.LiveRoom, error)
	GetAuction(ctx context.Context, id uint64) (*model.Auction, error)
	ListRoomAuctions(ctx context.Context, roomID uint64, status string) ([]model.Auction, error)
	CreateBid(ctx context.Context, bid *model.Bid) error
	UpdateAuctionBidState(ctx context.Context, auction *model.Auction) error
	ListAuctionBids(ctx context.Context, auctionID uint64, limit int) ([]model.Bid, error)
}

// GormPublicStore 是基于 GORM 的用户侧数据访问实现。
type GormPublicStore struct {
	db *gorm.DB
}

// NewGormPublicStore 创建 GORM 版本的用户侧数据访问对象。
func NewGormPublicStore(db *gorm.DB) *GormPublicStore {
	return &GormPublicStore{db: db}
}

// GetRoom 根据直播间 ID 查询直播间信息。
func (s *GormPublicStore) GetRoom(ctx context.Context, id uint64) (*model.LiveRoom, error) {
	var room model.LiveRoom
	if err := s.db.WithContext(ctx).First(&room, id).Error; err != nil {
		return nil, err
	}
	return &room, nil
}

// GetAuction 根据竞拍 ID 查询竞拍详情。
func (s *GormPublicStore) GetAuction(ctx context.Context, id uint64) (*model.Auction, error) {
	var auction model.Auction
	if err := s.db.WithContext(ctx).First(&auction, id).Error; err != nil {
		return nil, err
	}
	return &auction, nil
}

// ListRoomAuctions 查询指定直播间下的竞拍列表，可按状态过滤。
func (s *GormPublicStore) ListRoomAuctions(ctx context.Context, roomID uint64, status string) ([]model.Auction, error) {
	var auctions []model.Auction
	query := s.db.WithContext(ctx).Where("room_id = ?", roomID).Order("id DESC")
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if err := query.Find(&auctions).Error; err != nil {
		return nil, err
	}
	return auctions, nil
}

// CreateBid 将一次有效出价写入数据库。
func (s *GormPublicStore) CreateBid(ctx context.Context, bid *model.Bid) error {
	return s.db.WithContext(ctx).Create(bid).Error
}

// UpdateAuctionBidState 更新竞拍当前价、领先用户、结束时间和状态。
func (s *GormPublicStore) UpdateAuctionBidState(ctx context.Context, auction *model.Auction) error {
	return s.db.WithContext(ctx).
		Model(&model.Auction{}).
		Where("id = ?", auction.ID).
		Updates(map[string]interface{}{
			"current_price_cents": auction.CurrentPriceCents,
			"winner_user_id":      auction.WinnerUserID,
			"end_at":              auction.EndAt,
			"status":              auction.Status,
			"version":             gorm.Expr("version + 1"),
		}).Error
}

// ListAuctionBids 按金额倒序查询竞拍出价记录，用于数据库兜底排行榜。
func (s *GormPublicStore) ListAuctionBids(ctx context.Context, auctionID uint64, limit int) ([]model.Bid, error) {
	var bids []model.Bid
	query := s.db.WithContext(ctx).
		Where("auction_id = ? AND accepted = ?", auctionID, true).
		Order("amount_cents DESC, server_ts ASC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&bids).Error; err != nil {
		return nil, err
	}
	return bids, nil
}
