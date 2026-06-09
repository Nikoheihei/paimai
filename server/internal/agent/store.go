package agent

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"paimai/internal/model"
)

// Store owns only agent-layer persistence. Core auction writes stay in existing services.
type Store interface {
	WithTx(ctx context.Context, fn func(Store) error) error

	CreateAgent(ctx context.Context, agent *model.AgentProfile) error
	GetAgent(ctx context.Context, id uint64) (*model.AgentProfile, error)
	ListAgentsByOwner(ctx context.Context, ownerID uint64, agentType string) ([]model.AgentProfile, error)
	ListActiveBuyerAgents(ctx context.Context) ([]model.AgentProfile, error)
	UpdateAgent(ctx context.Context, agent *model.AgentProfile) error

	ListRunningAuctions(ctx context.Context, limit int) ([]model.Auction, error)

	UpsertMatch(ctx context.Context, match *model.AgentAuctionMatch) error
	UpdateMatchStatus(ctx context.Context, agentID, auctionID uint64, status string) error

	CreateBidAttempt(ctx context.Context, attempt *model.AgentBidAttempt) error
	GetBidAttemptByIdempotency(ctx context.Context, auctionID uint64, idempotencyKey string) (*model.AgentBidAttempt, error)
	CountBidAttempts(ctx context.Context, agentID, auctionID uint64) (int, error)
	LastBidAttempt(ctx context.Context, agentID, auctionID uint64) (*model.AgentBidAttempt, error)
	FindAcceptedBidAttempt(ctx context.Context, auctionID, buyerID uint64) (*model.AgentBidAttempt, error)
	ListRecentAcceptedAttempts(ctx context.Context, limit int) ([]model.AgentBidAttempt, error)

	CreatePact(ctx context.Context, pact *model.AgentPact) error
	GetPact(ctx context.Context, id uint64) (*model.AgentPact, error)
	GetPactByOrder(ctx context.Context, orderID uint64) (*model.AgentPact, error)
	ListPactsByBuyer(ctx context.Context, buyerID uint64) ([]model.AgentPact, error)
	UpdatePact(ctx context.Context, pact *model.AgentPact) error

	CreateAuditLog(ctx context.Context, log *model.AgentAuditLog) error
	ListAuditLogs(ctx context.Context, agentID uint64, limit int) ([]model.AgentAuditLog, error)

	CreateOutboxEvent(ctx context.Context, evt *model.OutboxEvent) error
	CreateMerchantJob(ctx context.Context, job *model.MerchantAgentJob) error
}

// GormStore implements Store with GORM.
type GormStore struct {
	db *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return &GormStore{db: db}
}

func (s *GormStore) WithTx(ctx context.Context, fn func(Store) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&GormStore{db: tx})
	})
}

func (s *GormStore) CreateAgent(ctx context.Context, agent *model.AgentProfile) error {
	return s.db.WithContext(ctx).Create(agent).Error
}

func (s *GormStore) GetAgent(ctx context.Context, id uint64) (*model.AgentProfile, error) {
	var agent model.AgentProfile
	if err := s.db.WithContext(ctx).First(&agent, id).Error; err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *GormStore) ListAgentsByOwner(ctx context.Context, ownerID uint64, agentType string) ([]model.AgentProfile, error) {
	var agents []model.AgentProfile
	query := s.db.WithContext(ctx).Where("owner_user_id = ?", ownerID).Order("id DESC")
	if agentType != "" {
		query = query.Where("agent_type = ?", agentType)
	}
	if err := query.Find(&agents).Error; err != nil {
		return nil, err
	}
	return agents, nil
}

func (s *GormStore) ListActiveBuyerAgents(ctx context.Context) ([]model.AgentProfile, error) {
	var agents []model.AgentProfile
	if err := s.db.WithContext(ctx).
		Where("agent_type = ? AND status = ?", "buyer", "active").
		Order("id ASC").
		Find(&agents).Error; err != nil {
		return nil, err
	}
	return agents, nil
}

func (s *GormStore) UpdateAgent(ctx context.Context, agent *model.AgentProfile) error {
	return s.db.WithContext(ctx).Save(agent).Error
}

// ListRunningAuctions 只读查询当前 running 竞拍快照，供 Runner 匹配使用。
func (s *GormStore) ListRunningAuctions(ctx context.Context, limit int) ([]model.Auction, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var auctions []model.Auction
	if err := s.db.WithContext(ctx).
		Where("status = ?", "running").
		Order("id ASC").
		Limit(limit).
		Find(&auctions).Error; err != nil {
		return nil, err
	}
	return auctions, nil
}

func (s *GormStore) UpsertMatch(ctx context.Context, match *model.AgentAuctionMatch) error {
	var existing model.AgentAuctionMatch
	err := s.db.WithContext(ctx).
		Where("agent_id = ? AND auction_id = ?", match.AgentID, match.AuctionID).
		First(&existing).Error
	if err == nil {
		match.ID = existing.ID
		return s.db.WithContext(ctx).Model(&model.AgentAuctionMatch{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
			"product_id":            match.ProductID,
			"match_score":           match.MatchScore,
			"match_reason_json":     match.MatchReasonJSON,
			"product_snapshot_json": match.ProductSnapshotJSON,
			"status":                match.Status,
			"trace_id":              match.TraceID,
		}).Error
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	return s.db.WithContext(ctx).Create(match).Error
}

func (s *GormStore) UpdateMatchStatus(ctx context.Context, agentID, auctionID uint64, status string) error {
	return s.db.WithContext(ctx).Model(&model.AgentAuctionMatch{}).
		Where("agent_id = ? AND auction_id = ?", agentID, auctionID).
		Update("status", status).Error
}

func (s *GormStore) CreateBidAttempt(ctx context.Context, attempt *model.AgentBidAttempt) error {
	return s.db.WithContext(ctx).Create(attempt).Error
}

func (s *GormStore) GetBidAttemptByIdempotency(ctx context.Context, auctionID uint64, idempotencyKey string) (*model.AgentBidAttempt, error) {
	var attempt model.AgentBidAttempt
	if err := s.db.WithContext(ctx).
		Where("auction_id = ? AND idempotency_key = ?", auctionID, idempotencyKey).
		First(&attempt).Error; err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (s *GormStore) CountBidAttempts(ctx context.Context, agentID, auctionID uint64) (int, error) {
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&model.AgentBidAttempt{}).
		Where("agent_id = ? AND auction_id = ?", agentID, auctionID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *GormStore) LastBidAttempt(ctx context.Context, agentID, auctionID uint64) (*model.AgentBidAttempt, error) {
	var attempt model.AgentBidAttempt
	if err := s.db.WithContext(ctx).
		Where("agent_id = ? AND auction_id = ?", agentID, auctionID).
		Order("id DESC").
		First(&attempt).Error; err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (s *GormStore) FindAcceptedBidAttempt(ctx context.Context, auctionID, buyerID uint64) (*model.AgentBidAttempt, error) {
	var attempt model.AgentBidAttempt
	if err := s.db.WithContext(ctx).
		Where("auction_id = ? AND buyer_id = ? AND result = ?", auctionID, buyerID, "accepted").
		Order("id DESC").
		First(&attempt).Error; err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (s *GormStore) ListRecentAcceptedAttempts(ctx context.Context, limit int) ([]model.AgentBidAttempt, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	var attempts []model.AgentBidAttempt
	if err := s.db.WithContext(ctx).
		Where("result = ?", "accepted").
		Order("id DESC").
		Limit(limit).
		Find(&attempts).Error; err != nil {
		return nil, err
	}
	return attempts, nil
}

func (s *GormStore) CreatePact(ctx context.Context, pact *model.AgentPact) error {
	return s.db.WithContext(ctx).Create(pact).Error
}

func (s *GormStore) GetPact(ctx context.Context, id uint64) (*model.AgentPact, error) {
	var pact model.AgentPact
	if err := s.db.WithContext(ctx).First(&pact, id).Error; err != nil {
		return nil, err
	}
	return &pact, nil
}

func (s *GormStore) GetPactByOrder(ctx context.Context, orderID uint64) (*model.AgentPact, error) {
	var pact model.AgentPact
	if err := s.db.WithContext(ctx).Where("order_id = ?", orderID).First(&pact).Error; err != nil {
		return nil, err
	}
	return &pact, nil
}

func (s *GormStore) ListPactsByBuyer(ctx context.Context, buyerID uint64) ([]model.AgentPact, error) {
	var pacts []model.AgentPact
	if err := s.db.WithContext(ctx).Where("buyer_id = ?", buyerID).Order("id DESC").Find(&pacts).Error; err != nil {
		return nil, err
	}
	return pacts, nil
}

func (s *GormStore) UpdatePact(ctx context.Context, pact *model.AgentPact) error {
	return s.db.WithContext(ctx).Save(pact).Error
}

func (s *GormStore) CreateAuditLog(ctx context.Context, log *model.AgentAuditLog) error {
	return s.db.WithContext(ctx).Create(log).Error
}

func (s *GormStore) ListAuditLogs(ctx context.Context, agentID uint64, limit int) ([]model.AgentAuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var logs []model.AgentAuditLog
	query := s.db.WithContext(ctx).Order("id ASC").Limit(limit)
	if agentID > 0 {
		query = query.Where("agent_id = ?", agentID)
	}
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *GormStore) CreateOutboxEvent(ctx context.Context, evt *model.OutboxEvent) error {
	err := s.db.WithContext(ctx).Create(evt).Error
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "UNIQUE") {
		return nil
	}
	return err
}

func (s *GormStore) CreateMerchantJob(ctx context.Context, job *model.MerchantAgentJob) error {
	return s.db.WithContext(ctx).Create(job).Error
}