package agent

import (
	"context"
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
	RuleScopeGlobal           = "global"
	RuleTypeApprovalThreshold = "approval_threshold"
	RuleTypeAvoidKeyword      = "avoid_keyword"
	RuleSourceSystemDefault   = "system_default"
	RuleSourceUserApproved    = "user_approved"
	DefaultApprovalThreshold  = 0.9
)

var ErrAgentNeedsUserConfirmation = fmt.Errorf("%w: agent bid requires user confirmation", service.ErrInvalidTransition)

type BiddingPolicy struct {
	ApprovalThresholdRatio float64
	AvoidKeywords          []string
	Rules                  []model.AgentBiddingRule
}

type bidGuardInput struct {
	Agent     *model.AgentProfile
	Auction   *model.Auction
	Product   *model.Product
	Skill     StrategySkill
	BidAmount int64
	Policy    BiddingPolicy
}

func defaultApprovalRule(userID uint64) *model.AgentBiddingRule {
	data, _ := json.Marshal(DefaultApprovalThreshold)
	return &model.AgentBiddingRule{
		UserID:    userID,
		Scope:     RuleScopeGlobal,
		RuleType:  RuleTypeApprovalThreshold,
		ValueJSON: string(data),
		Source:    RuleSourceSystemDefault,
		Enabled:   true,
	}
}

func (s *Service) ensureDefaultBiddingRules(ctx context.Context, userID uint64) error {
	return s.store.EnsureBiddingRule(ctx, defaultApprovalRule(userID))
}

func (s *Service) loadBiddingPolicy(ctx context.Context, userID uint64) (BiddingPolicy, error) {
	if err := s.ensureDefaultBiddingRules(ctx, userID); err != nil {
		return BiddingPolicy{}, err
	}
	rules, err := s.store.ListBiddingRules(ctx, userID)
	if err != nil {
		return BiddingPolicy{}, err
	}
	policy := BiddingPolicy{
		ApprovalThresholdRatio: DefaultApprovalThreshold,
		Rules:                  rules,
	}
	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}
		switch rule.RuleType {
		case RuleTypeApprovalThreshold:
			var ratio float64
			if err := json.Unmarshal([]byte(rule.ValueJSON), &ratio); err == nil && ratio > 0 && ratio <= 1 {
				policy.ApprovalThresholdRatio = ratio
			}
		case RuleTypeAvoidKeyword:
			var values []string
			if err := json.Unmarshal([]byte(rule.ValueJSON), &values); err == nil {
				policy.AvoidKeywords = append(policy.AvoidKeywords, normalizeKeywords(values)...)
			}
		}
	}
	return policy, nil
}

func (s *Service) checkBidGuard(ctx context.Context, in bidGuardInput) error {
	if in.Agent == nil || in.Auction == nil || in.Product == nil {
		return fmt.Errorf("%w: missing bid guard input", service.ErrInvalidInput)
	}
	if in.Skill.BuyerID != in.Agent.OwnerUserID {
		return service.ErrUnauthorized
	}
	if !in.Skill.RequireHumanPay {
		return fmt.Errorf("%w: requireHumanPay must be true", service.ErrInvalidInput)
	}
	if in.BidAmount > in.Skill.MaxBudgetCents || in.BidAmount > in.Agent.MaxBudgetCents {
		return fmt.Errorf("%w: bid exceeds strategy maxBudgetCents", service.ErrInvalidInput)
	}
	if err := ensureAuctionMatchesSkill(in.Skill, in.Auction); err != nil {
		return err
	}
	if err := s.checkAvoidKeywords(in.Product, in.Policy.AvoidKeywords); err != nil {
		return err
	}
	if ratio := in.Policy.ApprovalThresholdRatio; ratio > 0 && ratio < 1 {
		threshold := int64(float64(in.Agent.MaxBudgetCents) * ratio)
		if in.BidAmount > threshold {
			return ErrAgentNeedsUserConfirmation
		}
	}
	if in.Skill.MaxBidTimes > 0 {
		count, err := s.store.CountBidAttempts(ctx, in.Agent.ID, in.Auction.ID)
		if err != nil {
			return err
		}
		if count >= in.Skill.MaxBidTimes {
			return fmt.Errorf("%w: maxBidTimes reached", service.ErrInvalidInput)
		}
	}
	if in.Skill.MinIntervalMs > 0 {
		last, err := s.store.LastBidAttempt(ctx, in.Agent.ID, in.Auction.ID)
		if err != nil && !isRecordNotFound(err) {
			return err
		}
		if err == nil && !last.CreatedAt.IsZero() {
			elapsed := s.now().Sub(last.CreatedAt)
			if elapsed < time.Duration(in.Skill.MinIntervalMs)*time.Millisecond {
				return fmt.Errorf("%w: minIntervalMs not reached", service.ErrInvalidInput)
			}
		}
	}
	return nil
}

func (s *Service) checkAvoidKeywords(product *model.Product, avoidKeywords []string) error {
	if len(avoidKeywords) == 0 || product == nil {
		return nil
	}
	text := strings.ToLower(product.Name + " " + product.Description)
	for _, keyword := range avoidKeywords {
		if keyword != "" && strings.Contains(text, strings.ToLower(keyword)) {
			return fmt.Errorf("%w: product violates user bidding rule", service.ErrInvalidInput)
		}
	}
	return nil
}

func isRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func (s *Service) saveSessionState(ctx context.Context, state AgentSessionState, ttl time.Duration) {
	if s.sessionStore == nil {
		return
	}
	state.UpdatedAtMS = s.now().UnixMilli()
	if state.PlanState == "" {
		state.PlanState = PlanStateWatching
	}
	if len(state.RecentEvents) > 12 {
		state.RecentEvents = state.RecentEvents[len(state.RecentEvents)-12:]
	}
	_ = s.sessionStore.Save(ctx, state, ttl)
}

func buildSessionState(agent *model.AgentProfile, auction *model.Auction, skill StrategySkill, policy BiddingPolicy, planState string, events []string, riskFlags []string) AgentSessionState {
	return AgentSessionState{
		AgentID:   agent.ID,
		UserID:    agent.OwnerUserID,
		AuctionID: auction.ID,
		Goal:      "bid_for_item",
		Constraints: SessionConstraints{
			MaxBudgetCents:         agent.MaxBudgetCents,
			AvoidKeywords:          policy.AvoidKeywords,
			ApprovalThresholdRatio: policy.ApprovalThresholdRatio,
			Strategy:               skill.Strategy,
			RequireHumanPay:        skill.RequireHumanPay,
		},
		CurrentState: map[string]interface{}{
			"auction_status":      auction.Status,
			"current_price_cents": auction.CurrentPriceCents,
			"bid_increment_cents": auction.BidIncrementCents,
			"leader_user_id":      auction.WinnerUserID,
		},
		RecentEvents: events,
		PlanState:    planState,
		RiskFlags:    riskFlags,
	}
}
