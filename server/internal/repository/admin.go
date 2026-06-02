package repository

import (
	"context"

	"gorm.io/gorm"

	"paimai/internal/model"
)

// AuctionFilter 是后台竞拍列表的筛选条件。
//
// repository 层只表达“按哪些字段查询”，不理解具体业务状态是否允许变化；
// 状态是否合法由 service 层和状态机负责，避免数据访问层混入业务判断。
type AuctionFilter struct {
	RoomID *uint64
	Status string
}

// AdminStore 定义后台管理流程需要的数据访问能力。
//
// 服务层只依赖这个接口，而不是直接依赖 GORM。这样可以把数据存储实现隔离起来：
// MySQL/GORM 是当前实现，单元测试可以使用内存 stub，后续替换存储方案也不会牵动业务层。
type AdminStore interface {
	CreateProduct(ctx context.Context, product *model.Product) error
	ListProducts(ctx context.Context, sellerID *uint64) ([]model.Product, error)
	GetProduct(ctx context.Context, id uint64) (*model.Product, error)

	CreateAuction(ctx context.Context, auction *model.Auction) error
	GetAuction(ctx context.Context, id uint64) (*model.Auction, error)
	UpdateAuction(ctx context.Context, auction *model.Auction) error
	ListAuctions(ctx context.Context, filter AuctionFilter) ([]model.Auction, error)
}

// GormAdminStore 是基于 GORM 的 AdminStore 实现。
type GormAdminStore struct {
	db *gorm.DB
}

// NewGormAdminStore 创建 GORM 版本的数据访问对象。
func NewGormAdminStore(db *gorm.DB) *GormAdminStore {
	return &GormAdminStore{db: db}
}

// CreateProduct 将商品记录写入数据库。
func (s *GormAdminStore) CreateProduct(ctx context.Context, product *model.Product) error {
	return s.db.WithContext(ctx).Create(product).Error
}

// ListProducts 按可选卖家 ID 查询商品列表，并按 ID 倒序返回。
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

// GetProduct 根据商品 ID 查询单个商品记录。
func (s *GormAdminStore) GetProduct(ctx context.Context, id uint64) (*model.Product, error) {
	var product model.Product
	if err := s.db.WithContext(ctx).First(&product, id).Error; err != nil {
		return nil, err
	}
	return &product, nil
}

// CreateAuction 将竞拍配置写入数据库。
func (s *GormAdminStore) CreateAuction(ctx context.Context, auction *model.Auction) error {
	return s.db.WithContext(ctx).Create(auction).Error
}

// GetAuction 根据竞拍 ID 查询单个竞拍记录。
func (s *GormAdminStore) GetAuction(ctx context.Context, id uint64) (*model.Auction, error) {
	var auction model.Auction
	if err := s.db.WithContext(ctx).First(&auction, id).Error; err != nil {
		return nil, err
	}
	return &auction, nil
}

// UpdateAuction 保存竞拍记录的最新状态和规则配置。
func (s *GormAdminStore) UpdateAuction(ctx context.Context, auction *model.Auction) error {
	return s.db.WithContext(ctx).Save(auction).Error
}

// ListAuctions 按直播间和状态筛选竞拍列表，并按 ID 倒序返回。
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
