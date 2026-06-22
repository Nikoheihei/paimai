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

	EnsureBiddingRule(ctx context.Context, rule *model.AgentBiddingRule) error
	ListBiddingRules(ctx context.Context, userID uint64) ([]model.AgentBiddingRule, error)
	CreateEpisodeSummary(ctx context.Context, summary *model.AgentEpisodeSummary) error
	ListEpisodeSummaries(ctx context.Context, agentID uint64, limit int) ([]model.AgentEpisodeSummary, error)

	CreateOutboxEvent(ctx context.Context, evt *model.OutboxEvent) error
	CreateMerchantJob(ctx context.Context, job *model.MerchantAgentJob) error
}

// GormStore implements Store with GORM.
type GormStore struct {
	readDB  *gorm.DB
	writeDB *gorm.DB
}

func NewGormStore(db *gorm.DB) *GormStore {
	return NewGormStoreWithRouter(db, db)
}

func NewGormStoreWithRouter(readDB, writeDB *gorm.DB) *GormStore {
	return &GormStore{readDB: readDB, writeDB: writeDB}
}

func (s *GormStore) WithTx(ctx context.Context, fn func(Store) error) error {
	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(NewGormStore(tx))
	})
}

func (s *GormStore) CreateAgent(ctx context.Context, agent *model.AgentProfile) error {
	return s.writeDB.WithContext(ctx).Create(agent).Error
}

func (s *GormStore) GetAgent(ctx context.Context, id uint64) (*model.AgentProfile, error) {
	var agent model.AgentProfile
	if err := s.writeDB.WithContext(ctx).First(&agent, id).Error; err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *GormStore) ListAgentsByOwner(ctx context.Context, ownerID uint64, agentType string) ([]model.AgentProfile, error) {
	var agents []model.AgentProfile
	query := s.readDB.WithContext(ctx).Where("owner_user_id = ?", ownerID).Order("id DESC")
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
	if err := s.writeDB.WithContext(ctx).
		Where("agent_type = ? AND status = ?", "buyer", "active").
		Order("id ASC").
		Find(&agents).Error; err != nil {
		return nil, err
	}
	return agents, nil
}

func (s *GormStore) UpdateAgent(ctx context.Context, agent *model.AgentProfile) error {
	return s.writeDB.WithContext(ctx).Save(agent).Error
}

// ListRunningAuctions 只读查询当前 running 竞拍快照，供 Runner 匹配使用。
func (s *GormStore) ListRunningAuctions(ctx context.Context, limit int) ([]model.Auction, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var auctions []model.Auction
	if err := s.readDB.WithContext(ctx).
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
	err := s.writeDB.WithContext(ctx).
		Where("agent_id = ? AND auction_id = ?", match.AgentID, match.AuctionID).
		First(&existing).Error
	if err == nil {
		match.ID = existing.ID
		return s.writeDB.WithContext(ctx).Model(&model.AgentAuctionMatch{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
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
	return s.writeDB.WithContext(ctx).Create(match).Error
}

func (s *GormStore) UpdateMatchStatus(ctx context.Context, agentID, auctionID uint64, status string) error {
	return s.writeDB.WithContext(ctx).Model(&model.AgentAuctionMatch{}).
		Where("agent_id = ? AND auction_id = ?", agentID, auctionID).
		Update("status", status).Error
}

func (s *GormStore) CreateBidAttempt(ctx context.Context, attempt *model.AgentBidAttempt) error {
	return s.writeDB.WithContext(ctx).Create(attempt).Error
}

func (s *GormStore) GetBidAttemptByIdempotency(ctx context.Context, auctionID uint64, idempotencyKey string) (*model.AgentBidAttempt, error) {
	var attempt model.AgentBidAttempt
	if err := s.writeDB.WithContext(ctx).
		Where("auction_id = ? AND idempotency_key = ?", auctionID, idempotencyKey).
		First(&attempt).Error; err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (s *GormStore) CountBidAttempts(ctx context.Context, agentID, auctionID uint64) (int, error) {
	var count int64
	if err := s.writeDB.WithContext(ctx).
		Model(&model.AgentBidAttempt{}).
		Where("agent_id = ? AND auction_id = ?", agentID, auctionID).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (s *GormStore) LastBidAttempt(ctx context.Context, agentID, auctionID uint64) (*model.AgentBidAttempt, error) {
	var attempt model.AgentBidAttempt
	if err := s.writeDB.WithContext(ctx).
		Where("agent_id = ? AND auction_id = ?", agentID, auctionID).
		Order("id DESC").
		First(&attempt).Error; err != nil {
		return nil, err
	}
	return &attempt, nil
}

func (s *GormStore) FindAcceptedBidAttempt(ctx context.Context, auctionID, buyerID uint64) (*model.AgentBidAttempt, error) {
	var attempt model.AgentBidAttempt
	if err := s.writeDB.WithContext(ctx).
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
	if err := s.writeDB.WithContext(ctx).
		Where("result = ?", "accepted").
		Order("id DESC").
		Limit(limit).
		Find(&attempts).Error; err != nil {
		return nil, err
	}
	return attempts, nil
}

func (s *GormStore) CreatePact(ctx context.Context, pact *model.AgentPact) error {
	return s.writeDB.WithContext(ctx).Create(pact).Error
}

func (s *GormStore) GetPact(ctx context.Context, id uint64) (*model.AgentPact, error) {
	var pact model.AgentPact
	if err := s.writeDB.WithContext(ctx).First(&pact, id).Error; err != nil {
		return nil, err
	}
	return &pact, nil
}

func (s *GormStore) GetPactByOrder(ctx context.Context, orderID uint64) (*model.AgentPact, error) {
	var pact model.AgentPact
	if err := s.writeDB.WithContext(ctx).Where("order_id = ?", orderID).First(&pact).Error; err != nil {
		return nil, err
	}
	return &pact, nil
}

func (s *GormStore) ListPactsByBuyer(ctx context.Context, buyerID uint64) ([]model.AgentPact, error) {
	var pacts []model.AgentPact
	if err := s.readDB.WithContext(ctx).Where("buyer_id = ?", buyerID).Order("id DESC").Find(&pacts).Error; err != nil {
		return nil, err
	}
	return pacts, nil
}

func (s *GormStore) UpdatePact(ctx context.Context, pact *model.AgentPact) error {
	return s.writeDB.WithContext(ctx).Save(pact).Error
}

func (s *GormStore) CreateAuditLog(ctx context.Context, log *model.AgentAuditLog) error {
	return s.writeDB.WithContext(ctx).Create(log).Error
}

func (s *GormStore) ListAuditLogs(ctx context.Context, agentID uint64, limit int) ([]model.AgentAuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var logs []model.AgentAuditLog
	query := s.readDB.WithContext(ctx).Order("id ASC").Limit(limit)
	if agentID > 0 {
		query = query.Where("agent_id = ?", agentID)
	}
	if err := query.Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (s *GormStore) EnsureBiddingRule(ctx context.Context, rule *model.AgentBiddingRule) error {
	var existing model.AgentBiddingRule
	err := s.writeDB.WithContext(ctx).
		Where("user_id = ? AND scope = ? AND rule_type = ?", rule.UserID, rule.Scope, rule.RuleType).
		First(&existing).Error
	if err == nil {
		return nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	return s.writeDB.WithContext(ctx).Create(rule).Error
}

func (s *GormStore) ListBiddingRules(ctx context.Context, userID uint64) ([]model.AgentBiddingRule, error) {
	var rules []model.AgentBiddingRule
	if err := s.readDB.WithContext(ctx).
		Where("user_id = ? AND enabled = ?", userID, true).
		Order("id ASC").
		Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func (s *GormStore) CreateEpisodeSummary(ctx context.Context, summary *model.AgentEpisodeSummary) error {
	return s.writeDB.WithContext(ctx).Create(summary).Error
}

func (s *GormStore) ListEpisodeSummaries(ctx context.Context, agentID uint64, limit int) ([]model.AgentEpisodeSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var summaries []model.AgentEpisodeSummary
	query := s.readDB.WithContext(ctx).Order("id DESC").Limit(limit)
	if agentID > 0 {
		query = query.Where("agent_id = ?", agentID)
	}
	if err := query.Find(&summaries).Error; err != nil {
		return nil, err
	}
	return summaries, nil
}

func (s *GormStore) CreateOutboxEvent(ctx context.Context, evt *model.OutboxEvent) error {
	err := s.writeDB.WithContext(ctx).Create(evt).Error
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "UNIQUE") {
		return nil
	}
	return err
}

func (s *GormStore) CreateMerchantJob(ctx context.Context, job *model.MerchantAgentJob) error {
	return s.writeDB.WithContext(ctx).Create(job).Error
}
