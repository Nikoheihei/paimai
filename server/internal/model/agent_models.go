package model

import "time"

// AgentProfile stores buyer-owned and merchant-ops agents outside the core auction path.
type AgentProfile struct {
	ID             uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	OwnerUserID    uint64     `gorm:"not null;index" json:"ownerUserId"`
	AgentType      string     `gorm:"size:32;not null;index" json:"agentType"`
	Status         string     `gorm:"size:32;not null;index" json:"status"`
	Prompt         string     `gorm:"type:text" json:"prompt"`
	StrategyJSON   string     `gorm:"type:text" json:"strategyJson"`
	MaxBudgetCents int64      `gorm:"not null;default:0" json:"maxBudgetCents"`
	ExpiresAt      *time.Time `gorm:"default:null" json:"expiresAt"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// AgentAuctionMatch records an agent's decision to watch or stop a specific auction.
type AgentAuctionMatch struct {
	ID                  uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID             uint64    `gorm:"not null;uniqueIndex:idx_agent_auction_match" json:"agentId"`
	AuctionID           uint64    `gorm:"not null;uniqueIndex:idx_agent_auction_match;index" json:"auctionId"`
	ProductID           uint64    `gorm:"not null;index" json:"productId"`
	MatchScore          int       `gorm:"not null;default:0" json:"matchScore"`
	MatchReasonJSON     string    `gorm:"type:text" json:"matchReasonJson"`
	ProductSnapshotJSON string    `gorm:"type:text" json:"productSnapshotJson"`
	Status              string    `gorm:"size:32;not null;index" json:"status"`
	TraceID             string    `gorm:"size:64;not null;index" json:"traceId"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// AgentBidAttempt links an agent decision to the existing buyer bid path.
type AgentBidAttempt struct {
	ID             uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID        uint64    `gorm:"not null;index" json:"agentId"`
	BuyerID        uint64    `gorm:"not null;index" json:"buyerId"`
	AuctionID      uint64    `gorm:"not null;index;uniqueIndex:idx_agent_bid_idem" json:"auctionId"`
	AmountCents    int64     `gorm:"not null" json:"amountCents"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:idx_agent_bid_idem" json:"idempotencyKey"`
	TraceID        string    `gorm:"size:64;not null;index" json:"traceId"`
	Result         string    `gorm:"size:32;not null;index" json:"result"`
	RejectCode     string    `gorm:"size:64" json:"rejectCode"`
	BidID          *uint64   `gorm:"default:null" json:"bidId"`
	CreatedAt      time.Time `json:"createdAt"`
}

// AgentPact is the mandatory human approval object after an agent-assisted win.
type AgentPact struct {
	ID                  uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID             uint64     `gorm:"not null;index" json:"agentId"`
	BuyerID             uint64     `gorm:"not null;index" json:"buyerId"`
	AuctionID           uint64     `gorm:"not null;index" json:"auctionId"`
	OrderID             uint64     `gorm:"not null;uniqueIndex" json:"orderId"`
	ProductSnapshotJSON string     `gorm:"type:text;not null" json:"productSnapshotJson"`
	FinalPriceCents     int64      `gorm:"not null" json:"finalPriceCents"`
	WinningBidID        *uint64    `gorm:"default:null" json:"winningBidId"`
	BidHistoryHash      string     `gorm:"size:128;not null" json:"bidHistoryHash"`
	MaxBudgetCents      int64      `gorm:"not null" json:"maxBudgetCents"`
	AddressRequired     bool       `gorm:"not null;default:true" json:"addressRequired"`
	AddressID           *uint64    `gorm:"default:null" json:"addressId"`
	AddressSnapshot     string     `gorm:"type:text" json:"addressSnapshot"`
	PaymentDeadlineAt   time.Time  `json:"paymentDeadlineAt"`
	Status              string     `gorm:"size:32;not null;index" json:"status"`
	ApprovedByUserID    *uint64    `gorm:"default:null" json:"approvedByUserId"`
	ApprovedAt          *time.Time `gorm:"default:null" json:"approvedAt"`
	RejectedAt          *time.Time `gorm:"default:null" json:"rejectedAt"`
	TraceID             string     `gorm:"size:64;not null;index" json:"traceId"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

// AgentAuditLog is append-only from the application layer.
type AgentAuditLog struct {
	ID          uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	TraceID     string    `gorm:"size:64;not null;index" json:"traceId"`
	AgentID     *uint64   `gorm:"default:null;index" json:"agentId"`
	UserID      uint64    `gorm:"not null;index" json:"userId"`
	ActionType  string    `gorm:"size:64;not null;index" json:"actionType"`
	TimestampMS int64     `gorm:"not null;index" json:"timestampMs"`
	PayloadJSON string    `gorm:"type:text;not null" json:"payloadJson"`
	Operator    string    `gorm:"size:32;not null;index" json:"operator"`
	CreatedAt   time.Time `json:"createdAt"`
}


// MerchantAgentJob records merchant operations performed by merchant ops agents.
type MerchantAgentJob struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement" json:"id"`
	AgentID    uint64    `gorm:"not null;index" json:"agentId"`
	SellerID   uint64    `gorm:"not null;index" json:"sellerId"`
	JobType    string    `gorm:"size:64;not null;index" json:"jobType"`
	Status     string    `gorm:"size:32;not null;index" json:"status"`
	InputJSON  string    `gorm:"type:text" json:"inputJson"`
	ResultJSON string    `gorm:"type:text" json:"resultJson"`
	TraceID    string    `gorm:"size:64;not null;index" json:"traceId"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}
