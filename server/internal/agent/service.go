package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/service"
)

const (
	AgentTypeBuyer            = "buyer"
	AgentTypeMerchantOps      = "merchant_ops"
	AgentTypePlatformObserver = "platform_observer"

	AgentStatusDraft           = "draft"
	AgentStatusActive          = "active"
	AgentStatusPaused          = "paused"
	AgentStatusStoppedAfterWin = "stopped_after_win"
	AgentStatusExpired         = "expired"

	PactStatusCreated  = "created"
	PactStatusApproved = "approved"
	PactStatusRejected = "rejected"
	PactStatusExpired  = "expired"
)

var (
	ErrAgentForbidden = errors.New("agent forbidden")
	ErrAuditRequired  = errors.New("agent audit required")
)

// CoreReader is the read-only slice of the existing auction system used by the agent layer.
type CoreReader interface {
	GetAuction(ctx context.Context, id uint64) (*model.Auction, error)
	GetProduct(ctx context.Context, id uint64) (*model.Product, error)
	GetUser(ctx context.Context, id uint64) (*model.User, error)
	GetOrder(ctx context.Context, id uint64) (*model.Order, error)
	GetOrderByAuction(ctx context.Context, auctionID uint64) (*model.Order, error)
	ListAuctionBids(ctx context.Context, auctionID uint64, limit int) ([]model.Bid, error)
}

// Bidder is intentionally the existing buyer bid API shape.
type Bidder interface {
	PlaceBid(ctx context.Context, auctionID uint64, input service.BidInput) (*service.BidResult, error)
}

// Service coordinates the agent layer without mutating core auction state directly.
type Service struct {
	store        Store
	core         CoreReader
	bidder       Bidder
	sessionStore SessionStateStore
	now          func() time.Time
}

func NewService(store Store, core CoreReader, bidder Bidder) *Service {
	return &Service{
		store:        store,
		core:         core,
		bidder:       bidder,
		sessionStore: NewMemorySessionStore(),
		now:          time.Now,
	}
}

func (s *Service) SetSessionStore(store SessionStateStore) {
	if store != nil {
		s.sessionStore = store
	}
}

type CreateBuyerAgentInput struct {
	Prompt          string              `json:"prompt"`
	BuyerID         uint64              `json:"buyerId"`
	RoomID          uint64              `json:"roomId"`
	AuctionID       uint64              `json:"auctionId"`
	MaxBudgetCents  int64               `json:"maxBudgetCents"`
	Strategy        string              `json:"strategy"`
	MaxBidTimes     int                 `json:"maxBidTimes"`
	MinIntervalMs   int64               `json:"minIntervalMs"`
	RequireHumanPay *bool               `json:"requireHumanPay"`
	ProductKeywords []string            `json:"productKeywords"`
	Custom          CustomStrategySkill `json:"custom"`
	// OverBudgetCents 仅对 cap_only 策略生效：允许在预算之上额外超出的金额（分）。
	OverBudgetCents int64 `json:"overBudgetCents"`
	// CustomText 自定义策略自然语言文本。
	CustomText string `json:"customText"`
	// --- 新3维度字段 ---
	// Trigger 出价触发方式：lead=主动出价 / follow=跟价模式
	Trigger string `json:"trigger"`
	// Pace 出价节奏：min_step=最小步长 / reserve=保留价优先
	Pace string `json:"pace"`
	// StopRatio 停止比例：0=仅预算硬约束 / 0-1=到达预算X%时停止
	StopRatio float64 `json:"stopRatio"`

	ExpiresAt *time.Time `json:"expiresAt"`
}

type CreateMerchantAgentInput struct {
	Prompt    string     `json:"prompt"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type AgentBidInput struct {
	AuctionID      uint64 `json:"auctionId"`
	AmountCents    int64  `json:"amountCents"`
	IdempotencyKey string `json:"idempotencyKey"`
	TraceID        string `json:"traceId"`
	ClientTS       int64  `json:"clientTs"`
}

type AgentBidResponse struct {
	Attempt          *model.AgentBidAttempt `json:"attempt"`
	BidResult        *service.BidResult     `json:"bidResult,omitempty"`
	Pact             *model.AgentPact       `json:"pact,omitempty"`
	IdempotentReplay bool                   `json:"idempotentReplay"`
}

type PactApprovalInput struct {
	AddressID       *uint64 `json:"addressId"`
	AddressSnapshot string  `json:"addressSnapshot"`
}

type MerchantReportInput struct {
	AgentID uint64 `json:"agentId"`
	Scope   string `json:"scope"`
}

func (s *Service) CreateBuyerAgent(ctx context.Context, buyerID uint64, input CreateBuyerAgentInput) (*model.AgentProfile, error) {
	if buyerID == 0 {
		return nil, service.ErrUnauthorized
	}
	if err := s.ensureDefaultBiddingRules(ctx, buyerID); err != nil {
		return nil, err
	}
	traceID := newTraceID()
	// 意图解析只负责把自然语言翻译成结构化 skill；运行时只执行 skill 模板。
	parsed := ParseIntent(ctx, input.Prompt, input.MaxBudgetCents, input.ProductKeywords)
	strategy, err := buildStrategySkill(buyerID, input, parsed)
	if err != nil {
		return nil, err
	}
	if strategy.MaxBudgetCents <= 0 {
		return nil, fmt.Errorf("%w: 无法确定预算，请在 prompt 中写明最高价或填写预算", service.ErrInvalidInput)
	}
	strategyJSON, _ := json.Marshal(strategy)
	profile := &model.AgentProfile{
		OwnerUserID:    buyerID,
		AgentType:      AgentTypeBuyer,
		Status:         AgentStatusDraft,
		Prompt:         strategy.Prompt,
		StrategyJSON:   string(strategyJSON),
		MaxBudgetCents: strategy.MaxBudgetCents,
		ExpiresAt:      input.ExpiresAt,
	}
	if err := s.store.CreateAgent(ctx, profile); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, traceID, &profile.ID, buyerID, "agent.created", "user_manual", map[string]interface{}{
		"agentId": profile.ID,
		"type":    AgentTypeBuyer,
	}); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, traceID, &profile.ID, buyerID, "agent.intent.parsed", "agent_system", strategy); err != nil {
		return nil, err
	}
	_ = s.emitEvent(ctx, "agent.created", fmt.Sprintf("agent-created-%d", profile.ID), map[string]interface{}{
		"agentId": profile.ID,
		"userId":  buyerID,
	})
	return profile, nil
}

func (s *Service) CreateMerchantOpsAgent(ctx context.Context, sellerID uint64, input CreateMerchantAgentInput) (*model.AgentProfile, error) {
	if sellerID == 0 {
		return nil, service.ErrUnauthorized
	}
	traceID := newTraceID()
	profile := &model.AgentProfile{
		OwnerUserID:    sellerID,
		AgentType:      AgentTypeMerchantOps,
		Status:         AgentStatusDraft,
		Prompt:         strings.TrimSpace(input.Prompt),
		StrategyJSON:   "{}",
		MaxBudgetCents: 0,
		ExpiresAt:      input.ExpiresAt,
	}
	if err := s.store.CreateAgent(ctx, profile); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, traceID, &profile.ID, sellerID, "agent.created", "user_manual", map[string]interface{}{
		"agentId": profile.ID,
		"type":    AgentTypeMerchantOps,
	}); err != nil {
		return nil, err
	}
	_ = s.emitEvent(ctx, "agent.created", fmt.Sprintf("agent-created-%d", profile.ID), map[string]interface{}{
		"agentId": profile.ID,
		"userId":  sellerID,
	})
	return profile, nil
}

func (s *Service) ListAgents(ctx context.Context, ownerID uint64, agentType string) ([]model.AgentProfile, error) {
	if ownerID == 0 {
		return nil, service.ErrUnauthorized
	}
	return s.store.ListAgentsByOwner(ctx, ownerID, agentType)
}

func (s *Service) ActivateAgent(ctx context.Context, ownerID, agentID uint64) (*model.AgentProfile, error) {
	return s.setAgentStatus(ctx, ownerID, agentID, AgentStatusActive, "agent.activated")
}

func (s *Service) PauseAgent(ctx context.Context, ownerID, agentID uint64) (*model.AgentProfile, error) {
	return s.setAgentStatus(ctx, ownerID, agentID, AgentStatusPaused, "agent.paused")
}

func (s *Service) SubmitBuyerBid(ctx context.Context, buyerID, agentID uint64, input AgentBidInput) (*AgentBidResponse, error) {
	if s.bidder == nil {
		return nil, fmt.Errorf("%w: bidder unavailable", service.ErrInvalidInput)
	}
	traceID := strings.TrimSpace(input.TraceID)
	if traceID == "" {
		traceID = newTraceID()
	}
	idem := strings.TrimSpace(input.IdempotencyKey)
	if idem == "" {
		idem = fmt.Sprintf("agent:%d:auction:%d:trace:%s", agentID, input.AuctionID, traceID)
	}
	if existing, err := s.store.GetBidAttemptByIdempotency(ctx, input.AuctionID, idem); err == nil {
		return &AgentBidResponse{Attempt: existing, IdempotentReplay: true}, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	agent, err := s.requireBuyerAgent(ctx, buyerID, agentID)
	if err != nil {
		return nil, err
	}
	if _, err := s.core.GetUser(ctx, buyerID); err != nil {
		return nil, fmt.Errorf("%w: buyer identity invalid", service.ErrUnauthorized)
	}
	auction, err := s.core.GetAuction(ctx, input.AuctionID)
	if err != nil {
		return nil, err
	}
	if auction.Status != "running" {
		return nil, fmt.Errorf("%w: auction is not running", service.ErrInvalidTransition)
	}
	product, err := s.core.GetProduct(ctx, auction.ProductID)
	if err != nil {
		return nil, err
	}
	skill, err := decodeStrategySkill(agent)
	if err != nil {
		return nil, err
	}
	if err := ensureAuctionMatchesSkill(skill, auction); err != nil {
		return nil, err
	}
	if err := s.ensureProductMatches(agent, product); err != nil {
		return nil, err
	}
	policy, err := s.loadBiddingPolicy(ctx, buyerID)
	if err != nil {
		return nil, err
	}
	bids, err := s.core.ListAuctionBids(ctx, auction.ID, 1000)
	if err != nil {
		return nil, err
	}
	decision := DecideBid(agent, auction, product, bids)
	if !decision.ShouldBid {
		s.saveSessionState(ctx, buildSessionState(agent, auction, skill, policy, PlanStateWatching, []string{"agent_decision:wait"}, nil), sessionTTL(auction, s.now))
		return nil, fmt.Errorf("%w: strategy refused bid: %s", service.ErrInvalidInput, decision.Reason)
	}
	if input.AmountCents != decision.AmountCents {
		return nil, fmt.Errorf("%w: bid amount must match strategy template decision", service.ErrInvalidInput)
	}

	bidAmount := input.AmountCents
	if auction.CapPriceCents > 0 && bidAmount > auction.CapPriceCents {
		bidAmount = auction.CapPriceCents
	}
	if bidAmount <= 0 || bidAmount < auction.CurrentPriceCents+auction.BidIncrementCents {
		return nil, fmt.Errorf("%w: bid does not satisfy auction increment", service.ErrInvalidInput)
	}
	if bidAmount > agent.MaxBudgetCents {
		return nil, fmt.Errorf("%w: bid exceeds agent max budget", service.ErrInvalidInput)
	}
	if err := s.checkBidGuard(ctx, bidGuardInput{
		Agent:     agent,
		Auction:   auction,
		Product:   product,
		Skill:     skill,
		BidAmount: bidAmount,
		Policy:    policy,
	}); err != nil {
		planState := PlanStateStoppedByGuard
		riskFlags := []string{"guard_blocked"}
		if errors.Is(err, ErrAgentNeedsUserConfirmation) {
			planState = PlanStateNeedsUserConfirm
			riskFlags = []string{"approval_threshold_reached"}
		}
		s.saveSessionState(ctx, buildSessionState(agent, auction, skill, policy, planState, []string{
			fmt.Sprintf("agent_decision:blocked:%d", bidAmount),
		}, riskFlags), sessionTTL(auction, s.now))
		_ = s.audit(ctx, traceID, &agent.ID, buyerID, "agent.bid.guard_blocked", "agent_system", map[string]interface{}{
			"auctionId": auction.ID,
			"amount":    bidAmount,
			"reason":    err.Error(),
			"planState": planState,
		})
		return nil, err
	}
	s.saveSessionState(ctx, buildSessionState(agent, auction, skill, policy, PlanStateEligibleToBid, []string{
		fmt.Sprintf("agent_decision:bid:%d", bidAmount),
	}, nil), sessionTTL(auction, s.now))

	match := buildMatch(agent, auction, product, traceID)
	if err := s.store.UpsertMatch(ctx, match); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, traceID, &agent.ID, buyerID, "agent.auction.matched", "agent_system", map[string]interface{}{
		"auctionId": auction.ID,
		"productId": product.ID,
		"amount":    bidAmount,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAuditRequired, err)
	}
	if err := s.audit(ctx, traceID, &agent.ID, buyerID, "agent.bid.submitted", "agent_system", map[string]interface{}{
		"auctionId":      auction.ID,
		"amountCents":    bidAmount,
		"idempotencyKey": idem,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAuditRequired, err)
	}

	result, bidErr := s.bidder.PlaceBid(ctx, auction.ID, service.BidInput{
		UserID:         buyerID,
		AmountCents:    bidAmount,
		IdempotencyKey: idem,
		ClientTS:       input.ClientTS,
	})
	attempt := &model.AgentBidAttempt{
		AgentID:        agent.ID,
		BuyerID:        buyerID,
		AuctionID:      auction.ID,
		AmountCents:    bidAmount,
		IdempotencyKey: idem,
		TraceID:        traceID,
		Result:         "accepted",
	}
	if bidErr != nil {
		attempt.Result = "rejected"
		if reject, ok := bidErr.(*service.BidRejectError); ok {
			attempt.RejectCode = reject.Code
		} else {
			attempt.RejectCode = "BID_ERROR"
		}
	}
	if result != nil && !result.Accepted {
		attempt.Result = "rejected"
		attempt.RejectCode = result.Code
	}
	if err := s.store.CreateBidAttempt(ctx, attempt); err != nil {
		return nil, err
	}
	if bidErr != nil {
		return &AgentBidResponse{Attempt: attempt, BidResult: result}, bidErr
	}

	var pact *model.AgentPact
	if result != nil && result.Sold {
		_ = s.store.UpdateMatchStatus(ctx, agent.ID, auction.ID, "won")
		agent.Status = AgentStatusStoppedAfterWin
		_ = s.store.UpdateAgent(ctx, agent)
		_ = s.audit(ctx, traceID, &agent.ID, buyerID, "agent.bid.won", "agent_system", map[string]interface{}{
			"auctionId": auction.ID,
			"amount":    result.CurrentPriceCents,
		})
		if created, err := s.CreatePactFromWin(ctx, agent.ID, auction.ID, traceID); err == nil {
			pact = created
		} else {
			go s.retryPactFromWin(agent.ID, auction.ID, traceID)
		}
	}
	return &AgentBidResponse{Attempt: attempt, BidResult: result, Pact: pact}, nil
}

func (s *Service) CreatePactFromWin(ctx context.Context, agentID, auctionID uint64, traceID string) (*model.AgentPact, error) {
	if traceID == "" {
		traceID = newTraceID()
	}
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent.AgentType != AgentTypeBuyer {
		return nil, fmt.Errorf("%w: only buyer agents create pacts", ErrAgentForbidden)
	}
	auction, order, product, err := s.validatePactCore(ctx, agent, auctionID, 0)
	if err != nil {
		return nil, err
	}
	if _, err := s.store.FindAcceptedBidAttempt(ctx, auctionID, order.BuyerID); err != nil {
		return nil, fmt.Errorf("%w: no accepted agent bid attempt for winning buyer", service.ErrInvalidInput)
	}
	if existing, err := s.store.GetPactByOrder(ctx, order.ID); err == nil {
		return existing, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	productSnapshot, _ := json.Marshal(product)
	pact := &model.AgentPact{
		AgentID:             agent.ID,
		BuyerID:             order.BuyerID,
		AuctionID:           auction.ID,
		OrderID:             order.ID,
		ProductSnapshotJSON: string(productSnapshot),
		FinalPriceCents:     order.FinalPriceCents,
		BidHistoryHash:      "",
		MaxBudgetCents:      agent.MaxBudgetCents,
		AddressRequired:     true,
		PaymentDeadlineAt:   order.CreatedAt.Add(service.PaymentWindow),
		Status:              PactStatusCreated,
		TraceID:             traceID,
	}
	if err := s.store.CreatePact(ctx, pact); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, traceID, &agent.ID, order.BuyerID, "pact.created", "agent_system", map[string]interface{}{
		"pactId":    pact.ID,
		"auctionId": auction.ID,
		"orderId":   order.ID,
	}); err != nil {
		return nil, err
	}
	_ = s.emitEvent(ctx, "pact.created", fmt.Sprintf("pact-created-%d", pact.ID), map[string]interface{}{
		"pactId":    pact.ID,
		"auctionId": auction.ID,
		"orderId":   order.ID,
		"buyerId":   order.BuyerID,
	})
	return pact, nil
}

func (s *Service) ListPacts(ctx context.Context, buyerID uint64) ([]model.AgentPact, error) {
	if buyerID == 0 {
		return nil, service.ErrUnauthorized
	}
	return s.store.ListPactsByBuyer(ctx, buyerID)
}

func (s *Service) GetPact(ctx context.Context, buyerID, pactID uint64) (*model.AgentPact, error) {
	pact, err := s.store.GetPact(ctx, pactID)
	if err != nil {
		return nil, err
	}
	if pact.BuyerID != buyerID {
		return nil, service.ErrUnauthorized
	}
	return pact, nil
}

func (s *Service) ApprovePact(ctx context.Context, buyerID, pactID uint64, input PactApprovalInput) (*model.AgentPact, error) {
	pact, err := s.GetPact(ctx, buyerID, pactID)
	if err != nil {
		return nil, err
	}
	if pact.Status == PactStatusApproved {
		return pact, nil
	}
	if pact.Status != PactStatusCreated {
		return nil, fmt.Errorf("%w: Pact 当前状态不允许批准: %s", service.ErrInvalidTransition, pact.Status)
	}
	if input.AddressID == nil || strings.TrimSpace(input.AddressSnapshot) == "" {
		return nil, fmt.Errorf("%w: address is required before Pact approval", service.ErrInvalidInput)
	}
	agent, err := s.store.GetAgent(ctx, pact.AgentID)
	if err != nil {
		return nil, err
	}
	if _, _, _, err := s.validatePactCore(ctx, agent, pact.AuctionID, pact.OrderID); err != nil {
		return nil, err
	}
	now := s.now()
	pact.Status = PactStatusApproved
	pact.AddressID = input.AddressID
	pact.AddressSnapshot = strings.TrimSpace(input.AddressSnapshot)
	pact.ApprovedByUserID = &buyerID
	pact.ApprovedAt = &now
	if err := s.store.UpdatePact(ctx, pact); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, pact.TraceID, &pact.AgentID, buyerID, "pact.approved", "user_manual", map[string]interface{}{
		"pactId":    pact.ID,
		"orderId":   pact.OrderID,
		"auctionId": pact.AuctionID,
	}); err != nil {
		return nil, err
	}
	_ = s.emitEvent(ctx, "pact.approved", fmt.Sprintf("pact-approved-%d", pact.ID), map[string]interface{}{
		"pactId":  pact.ID,
		"orderId": pact.OrderID,
		"buyerId": buyerID,
	})
	s.createEpisodeSummaryFromPact(ctx, pact, "approved")
	return pact, nil
}

func (s *Service) RejectPact(ctx context.Context, buyerID, pactID uint64) (*model.AgentPact, error) {
	pact, err := s.GetPact(ctx, buyerID, pactID)
	if err != nil {
		return nil, err
	}
	if pact.Status == PactStatusRejected {
		return pact, nil
	}
	if pact.Status != PactStatusCreated {
		return nil, fmt.Errorf("%w: Pact 当前状态不允许拒绝: %s", service.ErrInvalidTransition, pact.Status)
	}
	now := s.now()
	pact.Status = PactStatusRejected
	pact.RejectedAt = &now
	if err := s.store.UpdatePact(ctx, pact); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, pact.TraceID, &pact.AgentID, buyerID, "pact.rejected", "user_manual", map[string]interface{}{
		"pactId":    pact.ID,
		"orderId":   pact.OrderID,
		"auctionId": pact.AuctionID,
	}); err != nil {
		return nil, err
	}
	_ = s.emitEvent(ctx, "pact.rejected", fmt.Sprintf("pact-rejected-%d", pact.ID), map[string]interface{}{
		"pactId":  pact.ID,
		"orderId": pact.OrderID,
		"buyerId": buyerID,
	})
	s.createEpisodeSummaryFromPact(ctx, pact, "rejected")
	return pact, nil
}

// CheckBuyerPaymentAllowed is a thin gate before the existing buyer pay API.
func (s *Service) CheckBuyerPaymentAllowed(ctx context.Context, orderID, buyerID uint64) error {
	order, err := s.core.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order.BuyerID != buyerID {
		return service.ErrUnauthorized
	}
	pact, err := s.store.GetPactByOrder(ctx, orderID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if pact.BuyerID != buyerID {
		return service.ErrUnauthorized
	}
	if pact.Status != PactStatusApproved {
		return fmt.Errorf("%w: agent-assisted order requires approved Pact before payment", service.ErrInvalidTransition)
	}
	return s.audit(ctx, pact.TraceID, &pact.AgentID, buyerID, "pact.payment_gate.passed", "platform_system", map[string]interface{}{
		"pactId":  pact.ID,
		"orderId": orderID,
	})
}

func (s *Service) AuditPaymentResult(ctx context.Context, orderID, buyerID uint64, paid bool, payErr error) {
	pact, err := s.store.GetPactByOrder(ctx, orderID)
	if err != nil || pact.BuyerID != buyerID {
		return
	}
	action := "order.payment_failed"
	payload := map[string]interface{}{"orderId": orderID}
	if paid {
		action = "order.paid"
	} else if payErr != nil {
		payload["error"] = payErr.Error()
	}
	_ = s.audit(ctx, pact.TraceID, &pact.AgentID, buyerID, action, "platform_system", payload)
}

func (s *Service) ListAuditLogs(ctx context.Context, ownerID, agentID uint64, limit int) ([]model.AgentAuditLog, error) {
	if agentID > 0 {
		agent, err := s.store.GetAgent(ctx, agentID)
		if err != nil {
			return nil, err
		}
		if agent.OwnerUserID != ownerID {
			return nil, service.ErrUnauthorized
		}
	}
	return s.store.ListAuditLogs(ctx, agentID, limit)
}

func (s *Service) RecordProductReleased(ctx context.Context, orderID, operatorUserID uint64) error {
	order, err := s.core.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order.Status != "closed" {
		return fmt.Errorf("%w: order is not closed", service.ErrInvalidTransition)
	}
	product, err := s.core.GetProduct(ctx, order.ProductID)
	if err != nil {
		return err
	}
	if product.Status != service.ProductStatusAvailable {
		return fmt.Errorf("%w: product is not available", service.ErrInvalidTransition)
	}
	traceID := newTraceID()
	if err := s.audit(ctx, traceID, nil, operatorUserID, "product.released", "platform_system", map[string]interface{}{
		"orderId":   order.ID,
		"productId": product.ID,
		"reason":    "payment_timeout",
	}); err != nil {
		return err
	}
	return s.emitEvent(ctx, "product.released", fmt.Sprintf("product-released-%d-order-%d", product.ID, order.ID), map[string]interface{}{
		"orderId":   order.ID,
		"productId": product.ID,
		"reason":    "payment_timeout",
	})
}

func (s *Service) CreateMerchantReportJob(ctx context.Context, sellerID uint64, input MerchantReportInput) (*model.MerchantAgentJob, error) {
	agent, err := s.store.GetAgent(ctx, input.AgentID)
	if err != nil {
		return nil, err
	}
	if agent.OwnerUserID != sellerID {
		return nil, service.ErrUnauthorized
	}
	if agent.AgentType != AgentTypeMerchantOps {
		return nil, fmt.Errorf("%w: only merchant ops agents can create merchant jobs", ErrAgentForbidden)
	}
	traceID := newTraceID()
	inputJSON, _ := json.Marshal(input)
	job := &model.MerchantAgentJob{
		AgentID:    agent.ID,
		SellerID:   sellerID,
		JobType:    "report",
		Status:     "succeeded",
		InputJSON:  string(inputJSON),
		ResultJSON: `{"message":"report job recorded; read-only merchant operation"}`,
		TraceID:    traceID,
	}
	if err := s.store.CreateMerchantJob(ctx, job); err != nil {
		return nil, err
	}
	if err := s.audit(ctx, traceID, &agent.ID, sellerID, "merchant.report.generated", "merchant_system", map[string]interface{}{
		"jobId": job.ID,
		"scope": input.Scope,
	}); err != nil {
		return nil, err
	}
	return job, nil
}

func (s *Service) RecordMerchantProductRelisted(ctx context.Context, sellerID, productID, auctionID uint64) error {
	traceID := newTraceID()
	if err := s.audit(ctx, traceID, nil, sellerID, "merchant.product.relisted", "merchant_system", map[string]interface{}{
		"productId": productID,
		"auctionId": auctionID,
	}); err != nil {
		return err
	}
	return s.emitEvent(ctx, "merchant.product.relisted", fmt.Sprintf("merchant-product-relisted-%d-auction-%d", productID, auctionID), map[string]interface{}{
		"productId": productID,
		"auctionId": auctionID,
		"sellerId":  sellerID,
	})
}

func (s *Service) RecordMerchantProductOffline(ctx context.Context, sellerID, productID uint64) error {
	traceID := newTraceID()
	if err := s.audit(ctx, traceID, nil, sellerID, "merchant.product.offline", "merchant_system", map[string]interface{}{
		"productId": productID,
	}); err != nil {
		return err
	}
	return s.emitEvent(ctx, "merchant.product.offline", fmt.Sprintf("merchant-product-offline-%d", productID), map[string]interface{}{
		"productId": productID,
		"sellerId":  sellerID,
	})
}

func (s *Service) retryPactFromWin(agentID, auctionID uint64, traceID string) {
	for i := 0; i < 10; i++ {
		time.Sleep(300 * time.Millisecond)
		if _, err := s.CreatePactFromWin(context.Background(), agentID, auctionID, traceID); err == nil {
			return
		}
	}
}

func (s *Service) setAgentStatus(ctx context.Context, ownerID, agentID uint64, status, action string) (*model.AgentProfile, error) {
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent.OwnerUserID != ownerID {
		return nil, service.ErrUnauthorized
	}
	agent.Status = status
	if err := s.store.UpdateAgent(ctx, agent); err != nil {
		return nil, err
	}
	traceID := newTraceID()
	if err := s.audit(ctx, traceID, &agent.ID, ownerID, action, "user_manual", map[string]interface{}{
		"agentId": agent.ID,
		"status":  status,
	}); err != nil {
		return nil, err
	}
	_ = s.emitEvent(ctx, action, fmt.Sprintf("%s-%d-%d", action, agent.ID, s.now().UnixNano()), map[string]interface{}{
		"agentId": agent.ID,
		"userId":  ownerID,
		"status":  status,
	})
	return agent, nil
}

func (s *Service) requireBuyerAgent(ctx context.Context, buyerID, agentID uint64) (*model.AgentProfile, error) {
	if buyerID == 0 {
		return nil, service.ErrUnauthorized
	}
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if agent.OwnerUserID != buyerID {
		return nil, service.ErrUnauthorized
	}
	if agent.AgentType != AgentTypeBuyer {
		return nil, fmt.Errorf("%w: only buyer agents can bid", ErrAgentForbidden)
	}
	if agent.Status != AgentStatusActive {
		return nil, fmt.Errorf("%w: agent is not active", service.ErrInvalidTransition)
	}
	if agent.ExpiresAt != nil && s.now().After(*agent.ExpiresAt) {
		agent.Status = AgentStatusExpired
		_ = s.store.UpdateAgent(ctx, agent)
		return nil, fmt.Errorf("%w: agent expired", service.ErrInvalidTransition)
	}
	return agent, nil
}

func (s *Service) validatePactCore(ctx context.Context, agent *model.AgentProfile, auctionID, expectedOrderID uint64) (*model.Auction, *model.Order, *model.Product, error) {
	auction, err := s.core.GetAuction(ctx, auctionID)
	if err != nil {
		return nil, nil, nil, err
	}
	if auction.Status != "sold" {
		return nil, nil, nil, fmt.Errorf("%w: auction must be sold before Pact", service.ErrInvalidTransition)
	}
	order, err := s.core.GetOrderByAuction(ctx, auctionID)
	if err != nil {
		return nil, nil, nil, err
	}
	if expectedOrderID > 0 && order.ID != expectedOrderID {
		return nil, nil, nil, service.ErrUnauthorized
	}
	if order.Status != "pending_payment" {
		return nil, nil, nil, fmt.Errorf("%w: order must be pending_payment before Pact approval", service.ErrInvalidTransition)
	}
	if order.BuyerID != agent.OwnerUserID {
		return nil, nil, nil, service.ErrUnauthorized
	}
	product, err := s.core.GetProduct(ctx, order.ProductID)
	if err != nil {
		return nil, nil, nil, err
	}
	if product.Status != service.ProductStatusLocked {
		return nil, nil, nil, fmt.Errorf("%w: product must be locked before Pact approval", service.ErrInvalidTransition)
	}
	if order.FinalPriceCents < auction.StartPriceCents {
		return nil, nil, nil, fmt.Errorf("%w: final price below start price", service.ErrInvalidInput)
	}
	if order.FinalPriceCents > agent.MaxBudgetCents {
		return nil, nil, nil, fmt.Errorf("%w: final price exceeds agent budget", service.ErrInvalidInput)
	}
	return auction, order, product, nil
}

func (s *Service) ensureProductMatches(agent *model.AgentProfile, product *model.Product) error {
	strategy, err := decodeStrategySkill(agent)
	if err != nil {
		return err
	}
	keywords := normalizeKeywords(strategy.ProductKeywords)
	if len(keywords) == 0 {
		return nil
	}
	text := strings.ToLower(product.Name + " " + product.Description)
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return nil
		}
	}
	return fmt.Errorf("%w: product does not match agent intent", service.ErrInvalidInput)
}

func ensureAuctionMatchesSkill(skill StrategySkill, auction *model.Auction) error {
	if skill.RoomID > 0 && auction.RoomID != skill.RoomID {
		return fmt.Errorf("%w: auction outside agent room scope", service.ErrInvalidInput)
	}
	if skill.AuctionID > 0 && auction.ID != skill.AuctionID {
		return fmt.Errorf("%w: auction outside agent auction scope", service.ErrInvalidInput)
	}
	return nil
}

func (s *Service) enforceStrategyBidGuards(ctx context.Context, agent *model.AgentProfile, auction *model.Auction, bidAmount int64, skill StrategySkill) error {
	if skill.BuyerID != agent.OwnerUserID {
		return service.ErrUnauthorized
	}
	if !skill.RequireHumanPay {
		return fmt.Errorf("%w: requireHumanPay must be true", service.ErrInvalidInput)
	}
	if bidAmount > skill.MaxBudgetCents {
		return fmt.Errorf("%w: bid exceeds strategy maxBudgetCents", service.ErrInvalidInput)
	}
	if err := ensureAuctionMatchesSkill(skill, auction); err != nil {
		return err
	}
	if skill.MaxBidTimes > 0 {
		count, err := s.store.CountBidAttempts(ctx, agent.ID, auction.ID)
		if err != nil {
			return err
		}
		if count >= skill.MaxBidTimes {
			return fmt.Errorf("%w: maxBidTimes reached", service.ErrInvalidInput)
		}
	}
	if skill.MinIntervalMs > 0 {
		last, err := s.store.LastBidAttempt(ctx, agent.ID, auction.ID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err == nil && !last.CreatedAt.IsZero() {
			elapsed := s.now().Sub(last.CreatedAt)
			if elapsed < time.Duration(skill.MinIntervalMs)*time.Millisecond {
				return fmt.Errorf("%w: minIntervalMs not reached", service.ErrInvalidInput)
			}
		}
	}
	return nil
}

func (s *Service) audit(ctx context.Context, traceID string, agentID *uint64, userID uint64, action, operator string, payload interface{}) error {
	if traceID == "" {
		traceID = newTraceID()
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.store.CreateAuditLog(ctx, &model.AgentAuditLog{
		TraceID:     traceID,
		AgentID:     agentID,
		UserID:      userID,
		ActionType:  action,
		TimestampMS: s.now().UnixMilli(),
		PayloadJSON: string(payloadJSON),
		Operator:    operator,
	})
}

func (s *Service) emitEvent(ctx context.Context, eventType, eventID string, payload map[string]interface{}) error {
	payload["type"] = eventType
	payload["eventId"] = eventID
	body, _ := json.Marshal(payload)
	return s.store.CreateOutboxEvent(ctx, &model.OutboxEvent{
		EventType: eventType,
		Payload:   string(body),
		Status:    "pending",
		EventUUID: eventID,
	})
}

func buildMatch(agent *model.AgentProfile, auction *model.Auction, product *model.Product, traceID string) *model.AgentAuctionMatch {
	productSnapshot, _ := json.Marshal(product)
	reason, _ := json.Marshal(map[string]interface{}{
		"reason": "product matched explicit or broad buyer intent",
		"prompt": agent.Prompt,
	})
	return &model.AgentAuctionMatch{
		AgentID:             agent.ID,
		AuctionID:           auction.ID,
		ProductID:           product.ID,
		MatchScore:          100,
		MatchReasonJSON:     string(reason),
		ProductSnapshotJSON: string(productSnapshot),
		Status:              "matched",
		TraceID:             traceID,
	}
}

func normalizeKeywords(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, value)
	}
	return result
}

func newTraceID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("trace-%d", time.Now().UnixNano())
	}
	return "trace-" + hex.EncodeToString(b[:])
}
