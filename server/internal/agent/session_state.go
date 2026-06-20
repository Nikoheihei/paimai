package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	PlanStateWatching         = "watching"
	PlanStateEligibleToBid    = "eligible_to_bid"
	PlanStateNeedsUserConfirm = "needs_user_confirm"
	PlanStateStoppedByGuard   = "stopped_by_guard"
)

type AgentSessionState struct {
	AgentID      uint64                 `json:"agent_id"`
	UserID       uint64                 `json:"user_id"`
	AuctionID    uint64                 `json:"auction_id"`
	Goal         string                 `json:"goal"`
	Constraints  SessionConstraints     `json:"constraints"`
	CurrentState map[string]interface{} `json:"current_state"`
	RecentEvents []string               `json:"recent_events"`
	PlanState    string                 `json:"plan_state"`
	RiskFlags    []string               `json:"risk_flags"`
	UpdatedAtMS  int64                  `json:"updated_at_ms"`
}

type SessionConstraints struct {
	MaxBudgetCents         int64    `json:"max_budget_cents"`
	AvoidKeywords          []string `json:"avoid_keywords,omitempty"`
	ApprovalThresholdRatio float64  `json:"approval_threshold_ratio"`
	Strategy               string   `json:"strategy"`
	RequireHumanPay        bool     `json:"require_human_pay"`
}

type SessionStateStore interface {
	Save(ctx context.Context, state AgentSessionState, ttl time.Duration) error
	Get(ctx context.Context, agentID, auctionID uint64) (*AgentSessionState, error)
}

type MemorySessionStore struct {
	mu     sync.Mutex
	states map[string]AgentSessionState
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{states: make(map[string]AgentSessionState)}
}

func (s *MemorySessionStore) Save(_ context.Context, state AgentSessionState, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.states[sessionKey(state.AgentID, state.AuctionID)] = state
	return nil
}

func (s *MemorySessionStore) Get(_ context.Context, agentID, auctionID uint64) (*AgentSessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state, ok := s.states[sessionKey(agentID, auctionID)]
	if !ok {
		return nil, nil
	}
	cp := state
	return &cp, nil
}

type RedisSessionStore struct {
	client *goredis.Client
}

func NewRedisSessionStore(client *goredis.Client) *RedisSessionStore {
	return &RedisSessionStore{client: client}
}

func (s *RedisSessionStore) Save(ctx context.Context, state AgentSessionState, ttl time.Duration) error {
	if s == nil || s.client == nil {
		return nil
	}
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, redisSessionKey(state.AgentID, state.AuctionID), data, ttl).Err()
}

func (s *RedisSessionStore) Get(ctx context.Context, agentID, auctionID uint64) (*AgentSessionState, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	data, err := s.client.Get(ctx, redisSessionKey(agentID, auctionID)).Result()
	if err == goredis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var state AgentSessionState
	if err := json.Unmarshal([]byte(data), &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func sessionKey(agentID, auctionID uint64) string {
	return fmt.Sprintf("%d:%d", agentID, auctionID)
}

func redisSessionKey(agentID, auctionID uint64) string {
	return fmt.Sprintf("agent_session_state:%d:%d", agentID, auctionID)
}
