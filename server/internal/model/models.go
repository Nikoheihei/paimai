package model

import (
	"time"
)

// User 表示系统用户（买家、卖家或主播）。
type User struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	Nickname  string    `gorm:"size:255;not null" json:"nickname"`
	AvatarURL string    `gorm:"size:512" json:"avatarUrl"`
	Role      string    `gorm:"type:enum('buyer','seller','anchor');default:'buyer';not null" json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

// LiveRoom 表示由卖家创建的在线直播间。
type LiveRoom struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	SellerID  uint64    `gorm:"not null;index" json:"sellerId"`
	Title     string    `gorm:"size:255;not null" json:"title"`
	CoverURL  string    `gorm:"size:512" json:"coverUrl"`
	Status    string    `gorm:"type:enum('offline','live','closed');default:'offline';not null" json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}

// Product 表示待拍卖的商品。
type Product struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	SellerID    uint64    `gorm:"not null;index" json:"sellerId"`
	Name        string    `gorm:"size:255;not null" json:"name"`
	ImageURL    string    `gorm:"size:512" json:"imageUrl"`
	Description string    `gorm:"type:text" json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

// Auction 表示拍卖场次/商品拍卖配置及当前状态。
type Auction struct {
	ID                 uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	RoomID             uint64    `gorm:"not null;index:idx_room_status" json:"roomId"`
	ProductID          uint64    `gorm:"not null;index" json:"productId"`
	Mode               string    `gorm:"type:varchar(50);default:'sudden_death';not null" json:"mode"` // sudden_death (绝杀模式) 或 extension (延时模式)
	StartPriceCents    int64     `gorm:"not null;default:0" json:"startPriceCents"`
	CurrentPriceCents  int64     `gorm:"not null;default:0" json:"currentPriceCents"`
	BidIncrementCents  int64     `gorm:"not null;default:0" json:"bidIncrementCents"`
	CapPriceCents      int64     `gorm:"not null;default:0" json:"capPriceCents"`
	ReservePriceCents  *int64    `gorm:"default:null" json:"reservePriceCents"` // 保留价，可为空（表示无保留价）
	StartAt            time.Time `json:"startAt"`
	EndAt              time.Time `gorm:"index:idx_status_end_at" json:"endAt"`
	ExtendThresholdSec int       `gorm:"not null;default:0" json:"extendThresholdSec"`
	ExtendDurationSec  int       `gorm:"not null;default:0" json:"extendDurationSec"`
	Status             string    `gorm:"type:enum('draft','scheduled','running','sold','failed','cancelled');default:'draft';not null;index:idx_room_status;index:idx_status_end_at" json:"status"`
	WinnerUserID       *uint64   `gorm:"index;default:null" json:"winnerUserId"`
	Version            int32     `gorm:"not null;default:1" json:"version"` // 乐观锁版本号
	CancelReason       string    `gorm:"size:255" json:"cancelReason"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// Bid 表示买家提交的单次出价记录。
type Bid struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	AuctionID      uint64    `gorm:"not null;index;uniqueIndex:idx_auction_idem" json:"auctionId"`
	UserID         uint64    `gorm:"not null;index" json:"userId"`
	AmountCents    int64     `gorm:"not null" json:"amountCents"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:idx_auction_idem" json:"idempotencyKey"`
	ClientTS       int64     `gorm:"not null" json:"clientTs"`
	ServerTS       int64     `gorm:"not null" json:"serverTs"`
	Accepted       bool      `gorm:"not null;default:true" json:"accepted"`
	RejectReason   string    `gorm:"size:255" json:"rejectReason"`
	CreatedAt      time.Time `json:"createdAt"`
}

// UserAuth 用户认证信息，与 User 表一对一关联。
// 密码哈希永远不在 JSON 中暴露（gorm:"-" + json:"-"）。
type UserAuth struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint64    `gorm:"uniqueIndex;not null" json:"userId"`
	Username     string    `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Order 表示拍卖成功后生成的最终购买订单。
type Order struct {
	ID              uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	AuctionID       uint64     `gorm:"not null;uniqueIndex" json:"auctionId"`
	ProductID       uint64     `gorm:"not null" json:"productId"`
	BuyerID         uint64     `gorm:"not null;index" json:"buyerId"`
	SellerID        uint64     `gorm:"not null" json:"sellerId"`
	FinalPriceCents int64      `gorm:"not null" json:"finalPriceCents"`
	Status          string     `gorm:"type:enum('pending_payment','paid','closed');default:'pending_payment';not null" json:"status"`
	CreatedAt       time.Time  `json:"createdAt"`
	PaidAt          *time.Time `gorm:"default:null" json:"paidAt"`
}

// OutboxEvent 表示待异步处理的事件记录，与业务数据在同一个 MySQL 事务中写入。
type OutboxEvent struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	EventType string    `gorm:"size:64;not null;index" json:"eventType"`
	Payload   string    `gorm:"type:text;not null" json:"payload"`
	Status    string    `gorm:"type:enum('pending','done','failed');default:'pending';not null;index" json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}
