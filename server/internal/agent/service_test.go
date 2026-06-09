package agent

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/service"
)

func TestSubmitBuyerBidRejectsInactiveAgent(t *testing.T) {
	svc, store, _, bidder := newAgentTestHarness()
	agent := seedAgent(store, 1, AgentTypeBuyer, AgentStatusDraft, 1000)

	_, err := svc.SubmitBuyerBid(context.Background(), 1, agent.ID, AgentBidInput{
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-inactive",
	})
	if !errors.Is(err, service.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
	if bidder.calls != 0 {
		t.Fatalf("inactive agent must not call existing bid path, calls=%d", bidder.calls)
	}
}

func TestSubmitBuyerBidRejectsOtherUser(t *testing.T) {
	svc, store, _, bidder := newAgentTestHarness()
	agent := seedAgent(store, 1, AgentTypeBuyer, AgentStatusActive, 1000)

	_, err := svc.SubmitBuyerBid(context.Background(), 2, agent.ID, AgentBidInput{
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-other",
	})
	if !errors.Is(err, service.ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
	if bidder.calls != 0 {
		t.Fatalf("cross-user agent must not call existing bid path, calls=%d", bidder.calls)
	}
}

func TestSubmitBuyerBidRejectsMerchantAgent(t *testing.T) {
	svc, store, _, bidder := newAgentTestHarness()
	agent := seedAgent(store, 1, AgentTypeMerchantOps, AgentStatusActive, 0)

	_, err := svc.SubmitBuyerBid(context.Background(), 1, agent.ID, AgentBidInput{
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-merchant",
	})
	if !errors.Is(err, ErrAgentForbidden) {
		t.Fatalf("expected ErrAgentForbidden, got %v", err)
	}
	if bidder.calls != 0 {
		t.Fatalf("merchant agent must never call bid path, calls=%d", bidder.calls)
	}
}

func TestSubmitBuyerBidUsesExistingBidderAndAudits(t *testing.T) {
	svc, store, _, bidder := newAgentTestHarness()
	agent := seedAgent(store, 1, AgentTypeBuyer, AgentStatusActive, 1000)

	resp, err := svc.SubmitBuyerBid(context.Background(), 1, agent.ID, AgentBidInput{
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-ok",
		TraceID:        "trace-ok",
	})
	if err != nil {
		t.Fatalf("SubmitBuyerBid() error = %v", err)
	}
	if bidder.calls != 1 {
		t.Fatalf("expected existing bid path once, got %d", bidder.calls)
	}
	if bidder.lastInput.UserID != 1 || bidder.lastInput.AmountCents != 200 || bidder.lastAuctionID != 1 {
		t.Fatalf("unexpected bidder input: auction=%d input=%+v", bidder.lastAuctionID, bidder.lastInput)
	}
	if resp.Attempt == nil || resp.Attempt.Result != "accepted" {
		t.Fatalf("expected accepted attempt, got %+v", resp.Attempt)
	}
	if !store.hasAudit("agent.bid.submitted") {
		t.Fatal("expected agent.bid.submitted audit")
	}
}

func TestCreateBuyerAgentBuildsStrategySkill(t *testing.T) {
	t.Setenv("AGENT_LLM_API_KEY", "")
	svc, _, _, _ := newAgentTestHarness()
	requirePay := true
	agent, err := svc.CreateBuyerAgent(context.Background(), 1, CreateBuyerAgentInput{
		Prompt:          "帮我拍 jade pendant，最高10元，保守策略，最多3次，间隔5秒",
		MaxBudgetCents:  1000,
		Strategy:        StrategyConservative,
		MaxBidTimes:     3,
		MinIntervalMs:   5000,
		RequireHumanPay: &requirePay,
		ProductKeywords: []string{"jade"},
	})
	if err != nil {
		t.Fatalf("CreateBuyerAgent() error = %v", err)
	}
	skill, err := decodeStrategySkill(agent)
	if err != nil {
		t.Fatalf("decodeStrategySkill() error = %v", err)
	}
	if skill.BuyerID != 1 || skill.Strategy != StrategyConservative || skill.MaxBidTimes != 3 || skill.MinIntervalMs != 5000 || !skill.RequireHumanPay {
		t.Fatalf("unexpected strategy skill: %+v", skill)
	}
}

func TestCreateBuyerAgentRejectsAutoPay(t *testing.T) {
	t.Setenv("AGENT_LLM_API_KEY", "")
	svc, _, _, _ := newAgentTestHarness()
	_, err := svc.CreateBuyerAgent(context.Background(), 1, CreateBuyerAgentInput{
		Prompt:         "帮我拍 jade，最高10元，拍中后自动支付",
		MaxBudgetCents: 1000,
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for auto pay, got %v", err)
	}
}

func TestSubmitBuyerBidRejectsAmountOutsideStrategyDecision(t *testing.T) {
	svc, store, _, bidder := newAgentTestHarness()
	agent := seedAgent(store, 1, AgentTypeBuyer, AgentStatusActive, 1000)

	_, err := svc.SubmitBuyerBid(context.Background(), 1, agent.ID, AgentBidInput{
		AuctionID:      1,
		AmountCents:    300,
		IdempotencyKey: "idem-wrong-amount",
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if bidder.calls != 0 {
		t.Fatalf("strategy mismatch must not call bidder, calls=%d", bidder.calls)
	}
}

func TestSubmitBuyerBidRejectsMaxBidTimes(t *testing.T) {
	svc, store, _, bidder := newAgentTestHarness()
	agent := seedAgent(store, 1, AgentTypeBuyer, AgentStatusActive, 1000)
	agent.StrategyJSON = `{"buyerId":1,"productKeywords":["jade"],"maxBudgetCents":1000,"strategy":"conservative","maxBidTimes":1,"minIntervalMs":0,"requireHumanPay":true}`
	_ = store.UpdateAgent(context.Background(), agent)
	_ = store.CreateBidAttempt(context.Background(), &model.AgentBidAttempt{
		AgentID:        agent.ID,
		BuyerID:        1,
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-old",
		Result:         "accepted",
		CreatedAt:      time.Now().Add(-time.Hour),
	})

	_, err := svc.SubmitBuyerBid(context.Background(), 1, agent.ID, AgentBidInput{
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-max",
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if bidder.calls != 0 {
		t.Fatalf("maxBidTimes guard must not call bidder, calls=%d", bidder.calls)
	}
}

func TestSubmitBuyerBidRejectsMinInterval(t *testing.T) {
	svc, store, _, bidder := newAgentTestHarness()
	agent := seedAgent(store, 1, AgentTypeBuyer, AgentStatusActive, 1000)
	agent.StrategyJSON = `{"buyerId":1,"productKeywords":["jade"],"maxBudgetCents":1000,"strategy":"conservative","maxBidTimes":5,"minIntervalMs":60000,"requireHumanPay":true}`
	_ = store.UpdateAgent(context.Background(), agent)
	_ = store.CreateBidAttempt(context.Background(), &model.AgentBidAttempt{
		AgentID:        agent.ID,
		BuyerID:        1,
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-recent",
		Result:         "accepted",
		CreatedAt:      time.Now(),
	})

	_, err := svc.SubmitBuyerBid(context.Background(), 1, agent.ID, AgentBidInput{
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-interval",
	})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if bidder.calls != 0 {
		t.Fatalf("minInterval guard must not call bidder, calls=%d", bidder.calls)
	}
}

func TestSubmitBuyerBidIdempotencySkipsBidder(t *testing.T) {
	svc, store, _, bidder := newAgentTestHarness()
	_ = seedAgent(store, 1, AgentTypeBuyer, AgentStatusActive, 1000)
	_ = store.CreateBidAttempt(context.Background(), &model.AgentBidAttempt{
		AgentID:        1,
		BuyerID:        1,
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-replay",
		TraceID:        "trace-replay",
		Result:         "accepted",
	})

	resp, err := svc.SubmitBuyerBid(context.Background(), 1, 1, AgentBidInput{
		AuctionID:      1,
		AmountCents:    200,
		IdempotencyKey: "idem-replay",
	})
	if err != nil {
		t.Fatalf("SubmitBuyerBid replay error = %v", err)
	}
	if !resp.IdempotentReplay {
		t.Fatal("expected idempotent replay")
	}
	if bidder.calls != 0 {
		t.Fatalf("idempotent replay must not call bidder, calls=%d", bidder.calls)
	}
}

func TestCreatePactAndApproveRequiresAddress(t *testing.T) {
	svc, store, core, _ := newAgentTestHarness()
	core.auctions[1].Status = "sold"
	agent := seedAgent(store, 1, AgentTypeBuyer, AgentStatusActive, 1000)
	_ = store.CreateBidAttempt(context.Background(), &model.AgentBidAttempt{
		AgentID:        agent.ID,
		BuyerID:        1,
		AuctionID:      1,
		AmountCents:    500,
		IdempotencyKey: "idem-win",
		TraceID:        "trace-win",
		Result:         "accepted",
	})

	pact, err := svc.CreatePactFromWin(context.Background(), agent.ID, 1, "trace-win")
	if err != nil {
		t.Fatalf("CreatePactFromWin() error = %v", err)
	}
	if pact.Status != PactStatusCreated {
		t.Fatalf("expected created pact, got %s", pact.Status)
	}
	if !store.hasAudit("pact.created") {
		t.Fatal("expected pact.created audit")
	}

	_, err = svc.ApprovePact(context.Background(), 1, pact.ID, PactApprovalInput{})
	if !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("expected address validation error, got %v", err)
	}

	addrID := uint64(9)
	approved, err := svc.ApprovePact(context.Background(), 1, pact.ID, PactApprovalInput{
		AddressID:       &addrID,
		AddressSnapshot: "Alice, 13800000000, Shanghai",
	})
	if err != nil {
		t.Fatalf("ApprovePact() error = %v", err)
	}
	if approved.Status != PactStatusApproved {
		t.Fatalf("expected approved pact, got %s", approved.Status)
	}
	if !store.hasAudit("pact.approved") {
		t.Fatal("expected pact.approved audit")
	}
}

func TestPaymentGateRequiresApprovedPact(t *testing.T) {
	svc, store, _, _ := newAgentTestHarness()
	pact := &model.AgentPact{
		AgentID:           1,
		BuyerID:           1,
		AuctionID:         1,
		OrderID:           1,
		FinalPriceCents:   500,
		MaxBudgetCents:    1000,
		BidHistoryHash:    "hash",
		PaymentDeadlineAt: time.Now().Add(time.Minute),
		Status:            PactStatusCreated,
		TraceID:           "trace-pay",
	}
	_ = store.CreatePact(context.Background(), pact)

	err := svc.CheckBuyerPaymentAllowed(context.Background(), 1, 1)
	if !errors.Is(err, service.ErrInvalidTransition) {
		t.Fatalf("expected Pact approval gate error, got %v", err)
	}

	pact.Status = PactStatusApproved
	_ = store.UpdatePact(context.Background(), pact)
	if err := svc.CheckBuyerPaymentAllowed(context.Background(), 1, 1); err != nil {
		t.Fatalf("approved Pact should pass payment gate: %v", err)
	}

	if err := svc.CheckBuyerPaymentAllowed(context.Background(), 2, 1); err != nil {
		t.Fatalf("manual order without Pact should pass: %v", err)
	}
}

func newAgentTestHarness() (*Service, *memoryStore, *fakeCore, *fakeBidder) {
	store := newMemoryStore()
	core := newFakeCore()
	bidder := &fakeBidder{}
	svc := NewService(store, core, bidder)
	svc.now = func() time.Time {
		return time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	}
	return svc, store, core, bidder
}

func seedAgent(store *memoryStore, ownerID uint64, agentType, status string, maxBudget int64) *model.AgentProfile {
	agent := &model.AgentProfile{
		OwnerUserID:    ownerID,
		AgentType:      agentType,
		Status:         status,
		StrategyJSON:   `{"prompt":"jade","productKeywords":["jade"],"maxBudgetCents":1000}`,
		MaxBudgetCents: maxBudget,
	}
	_ = store.CreateAgent(context.Background(), agent)
	return agent
}

type fakeBidder struct {
	calls         int
	lastAuctionID uint64
	lastInput     service.BidInput
}

func (b *fakeBidder) PlaceBid(_ context.Context, auctionID uint64, input service.BidInput) (*service.BidResult, error) {
	b.calls++
	b.lastAuctionID = auctionID
	b.lastInput = input
	return &service.BidResult{
		Accepted:          true,
		AuctionID:         auctionID,
		UserID:            input.UserID,
		AmountCents:       input.AmountCents,
		CurrentPriceCents: input.AmountCents,
		Status:            "running",
	}, nil
}

type fakeCore struct {
	users    map[uint64]*model.User
	auctions map[uint64]*model.Auction
	products map[uint64]*model.Product
	orders   map[uint64]*model.Order
	bids     []model.Bid
}

func newFakeCore() *fakeCore {
	winner := uint64(1)
	return &fakeCore{
		users: map[uint64]*model.User{
			1: {ID: 1, Role: "buyer", Nickname: "Alice"},
			2: {ID: 2, Role: "buyer", Nickname: "Bob"},
		},
		auctions: map[uint64]*model.Auction{
			1: {
				ID:                1,
				RoomID:            1,
				ProductID:         1,
				Status:            "running",
				StartPriceCents:   100,
				CurrentPriceCents: 100,
				BidIncrementCents: 100,
				CapPriceCents:     1000,
				WinnerUserID:      &winner,
			},
		},
		products: map[uint64]*model.Product{
			1: {ID: 1, SellerID: 10, Name: "jade pendant", Description: "green jade", Status: service.ProductStatusLocked},
		},
		orders: map[uint64]*model.Order{
			1: {ID: 1, AuctionID: 1, ProductID: 1, BuyerID: 1, SellerID: 10, FinalPriceCents: 500, Status: "pending_payment", CreatedAt: time.Date(2026, 6, 8, 11, 59, 0, 0, time.UTC)},
			2: {ID: 2, AuctionID: 2, ProductID: 1, BuyerID: 1, SellerID: 10, FinalPriceCents: 300, Status: "pending_payment", CreatedAt: time.Date(2026, 6, 8, 11, 59, 0, 0, time.UTC)},
		},
		bids: []model.Bid{
			{ID: 1, AuctionID: 1, UserID: 2, AmountCents: 100, Accepted: true, ServerTS: 1},
		},
	}
}

func (c *fakeCore) GetAuction(_ context.Context, id uint64) (*model.Auction, error) {
	auction, ok := c.auctions[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *auction
	return &cp, nil
}

func (c *fakeCore) GetProduct(_ context.Context, id uint64) (*model.Product, error) {
	product, ok := c.products[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *product
	return &cp, nil
}

func (c *fakeCore) GetUser(_ context.Context, id uint64) (*model.User, error) {
	user, ok := c.users[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *user
	return &cp, nil
}

func (c *fakeCore) GetOrder(_ context.Context, id uint64) (*model.Order, error) {
	order, ok := c.orders[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *order
	return &cp, nil
}

func (c *fakeCore) GetOrderByAuction(_ context.Context, auctionID uint64) (*model.Order, error) {
	for _, order := range c.orders {
		if order.AuctionID == auctionID {
			cp := *order
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (c *fakeCore) ListAuctionBids(_ context.Context, auctionID uint64, _ int) ([]model.Bid, error) {
	var result []model.Bid
	for _, bid := range c.bids {
		if bid.AuctionID == auctionID {
			result = append(result, bid)
		}
	}
	return result, nil
}

type memoryStore struct {
	agents   map[uint64]*model.AgentProfile
	matches  map[string]*model.AgentAuctionMatch
	attempts []*model.AgentBidAttempt
	pacts    map[uint64]*model.AgentPact
	audits   []*model.AgentAuditLog
	outbox   []*model.OutboxEvent
	jobs     []*model.MerchantAgentJob
	nextID   uint64
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		agents:  make(map[uint64]*model.AgentProfile),
		matches: make(map[string]*model.AgentAuctionMatch),
		pacts:   make(map[uint64]*model.AgentPact),
		nextID:  1,
	}
}

func (s *memoryStore) WithTx(_ context.Context, fn func(Store) error) error { return fn(s) }

func (s *memoryStore) CreateAgent(_ context.Context, agent *model.AgentProfile) error {
	if agent.ID == 0 {
		agent.ID = s.nextID
		s.nextID++
	}
	cp := *agent
	s.agents[agent.ID] = &cp
	return nil
}

func (s *memoryStore) GetAgent(_ context.Context, id uint64) (*model.AgentProfile, error) {
	agent, ok := s.agents[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *agent
	return &cp, nil
}

func (s *memoryStore) ListAgentsByOwner(_ context.Context, ownerID uint64, agentType string) ([]model.AgentProfile, error) {
	var agents []model.AgentProfile
	for _, agent := range s.agents {
		if agent.OwnerUserID == ownerID && (agentType == "" || agent.AgentType == agentType) {
			agents = append(agents, *agent)
		}
	}
	return agents, nil
}

func (s *memoryStore) ListActiveBuyerAgents(_ context.Context) ([]model.AgentProfile, error) {
	var agents []model.AgentProfile
	for _, agent := range s.agents {
		if agent.AgentType == "buyer" && agent.Status == "active" {
			agents = append(agents, *agent)
		}
	}
	return agents, nil
}

func (s *memoryStore) ListRunningAuctions(_ context.Context, _ int) ([]model.Auction, error) {
	return nil, nil
}

func (s *memoryStore) ListRecentAcceptedAttempts(_ context.Context, limit int) ([]model.AgentBidAttempt, error) {
	var out []model.AgentBidAttempt
	for i := len(s.attempts) - 1; i >= 0; i-- {
		if s.attempts[i].Result == "accepted" {
			out = append(out, *s.attempts[i])
		}
	}
	return out, nil
}

func (s *memoryStore) UpdateAgent(_ context.Context, agent *model.AgentProfile) error {
	cp := *agent
	s.agents[agent.ID] = &cp
	return nil
}

func (s *memoryStore) UpsertMatch(_ context.Context, match *model.AgentAuctionMatch) error {
	key := matchKey(match.AgentID, match.AuctionID)
	cp := *match
	s.matches[key] = &cp
	return nil
}

func (s *memoryStore) UpdateMatchStatus(_ context.Context, agentID, auctionID uint64, status string) error {
	if match, ok := s.matches[matchKey(agentID, auctionID)]; ok {
		match.Status = status
	}
	return nil
}

func (s *memoryStore) CreateBidAttempt(_ context.Context, attempt *model.AgentBidAttempt) error {
	if attempt.ID == 0 {
		attempt.ID = s.nextID
		s.nextID++
	}
	if attempt.CreatedAt.IsZero() {
		attempt.CreatedAt = time.Now()
	}
	cp := *attempt
	s.attempts = append(s.attempts, &cp)
	return nil
}

func (s *memoryStore) GetBidAttemptByIdempotency(_ context.Context, auctionID uint64, idempotencyKey string) (*model.AgentBidAttempt, error) {
	for _, attempt := range s.attempts {
		if attempt.AuctionID == auctionID && attempt.IdempotencyKey == idempotencyKey {
			cp := *attempt
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *memoryStore) CountBidAttempts(_ context.Context, agentID, auctionID uint64) (int, error) {
	count := 0
	for _, attempt := range s.attempts {
		if attempt.AgentID == agentID && attempt.AuctionID == auctionID {
			count++
		}
	}
	return count, nil
}

func (s *memoryStore) LastBidAttempt(_ context.Context, agentID, auctionID uint64) (*model.AgentBidAttempt, error) {
	for i := len(s.attempts) - 1; i >= 0; i-- {
		attempt := s.attempts[i]
		if attempt.AgentID == agentID && attempt.AuctionID == auctionID {
			cp := *attempt
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *memoryStore) FindAcceptedBidAttempt(_ context.Context, auctionID, buyerID uint64) (*model.AgentBidAttempt, error) {
	for _, attempt := range s.attempts {
		if attempt.AuctionID == auctionID && attempt.BuyerID == buyerID && attempt.Result == "accepted" {
			cp := *attempt
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *memoryStore) CreatePact(_ context.Context, pact *model.AgentPact) error {
	if pact.ID == 0 {
		pact.ID = s.nextID
		s.nextID++
	}
	cp := *pact
	s.pacts[pact.ID] = &cp
	return nil
}

func (s *memoryStore) GetPact(_ context.Context, id uint64) (*model.AgentPact, error) {
	pact, ok := s.pacts[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *pact
	return &cp, nil
}

func (s *memoryStore) GetPactByOrder(_ context.Context, orderID uint64) (*model.AgentPact, error) {
	for _, pact := range s.pacts {
		if pact.OrderID == orderID {
			cp := *pact
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (s *memoryStore) ListPactsByBuyer(_ context.Context, buyerID uint64) ([]model.AgentPact, error) {
	var pacts []model.AgentPact
	for _, pact := range s.pacts {
		if pact.BuyerID == buyerID {
			pacts = append(pacts, *pact)
		}
	}
	return pacts, nil
}

func (s *memoryStore) UpdatePact(_ context.Context, pact *model.AgentPact) error {
	cp := *pact
	s.pacts[pact.ID] = &cp
	return nil
}

func (s *memoryStore) CreateAuditLog(_ context.Context, log *model.AgentAuditLog) error {
	cp := *log
	s.audits = append(s.audits, &cp)
	return nil
}

func (s *memoryStore) ListAuditLogs(_ context.Context, agentID uint64, _ int) ([]model.AgentAuditLog, error) {
	var logs []model.AgentAuditLog
	for _, log := range s.audits {
		if agentID == 0 || (log.AgentID != nil && *log.AgentID == agentID) {
			logs = append(logs, *log)
		}
	}
	return logs, nil
}

func (s *memoryStore) CreateOutboxEvent(_ context.Context, evt *model.OutboxEvent) error {
	cp := *evt
	s.outbox = append(s.outbox, &cp)
	return nil
}

func (s *memoryStore) CreateMerchantJob(_ context.Context, job *model.MerchantAgentJob) error {
	if job.ID == 0 {
		job.ID = s.nextID
		s.nextID++
	}
	cp := *job
	s.jobs = append(s.jobs, &cp)
	return nil
}

func (s *memoryStore) hasAudit(action string) bool {
	for _, audit := range s.audits {
		if audit.ActionType == action {
			return true
		}
	}
	return false
}

func matchKey(agentID, auctionID uint64) string {
	return fmt.Sprintf("%d:%d", agentID, auctionID)
}
