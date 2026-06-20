package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"paimai/internal/model"
)

func sessionTTL(auction *model.Auction, now func() time.Time) time.Duration {
	if auction == nil {
		return 24 * time.Hour
	}
	endAt := auction.EndAt.Add(24 * time.Hour)
	ttl := endAt.Sub(now())
	if ttl <= 0 || ttl > 7*24*time.Hour {
		return 24 * time.Hour
	}
	return ttl
}

func (s *Service) createEpisodeSummaryFromPact(ctx context.Context, pact *model.AgentPact, outcome string) {
	if pact == nil {
		return
	}
	agent, err := s.store.GetAgent(ctx, pact.AgentID)
	if err != nil {
		return
	}
	productName := "item"
	category := "general"
	var product map[string]interface{}
	if err := json.Unmarshal([]byte(pact.ProductSnapshotJSON), &product); err == nil {
		if name, ok := product["name"].(string); ok && strings.TrimSpace(name) != "" {
			productName = name
			category = inferCategory(name)
		}
	}
	ratio := float64(0)
	if pact.MaxBudgetCents > 0 {
		ratio = float64(pact.FinalPriceCents) / float64(pact.MaxBudgetCents)
	}
	signal := map[string]interface{}{
		"budget_usage_ratio": ratio,
		"near_budget":        ratio >= DefaultApprovalThreshold,
	}
	recommendation := map[string]interface{}{
		"type": "none",
	}
	if outcome == "rejected" && ratio >= DefaultApprovalThreshold {
		signal["near_budget_rejection"] = true
		recommendation = map[string]interface{}{
			"type":                 "approval_threshold",
			"scope":                category,
			"value":                0.85,
			"source":               RuleSourceUserApproved,
			"requiresUserApproval": true,
		}
	}
	signalJSON, _ := json.Marshal(signal)
	recommendationJSON, _ := json.Marshal(recommendation)
	summary := fmt.Sprintf("Agent won %s at %.1f%% of budget; user outcome: %s.", productName, ratio*100, outcome)
	record := &model.AgentEpisodeSummary{
		AgentID:            pact.AgentID,
		UserID:             pact.BuyerID,
		AuctionID:          pact.AuctionID,
		Category:           category,
		Outcome:            outcome,
		Summary:            summary,
		DerivedSignalJSON:  string(signalJSON),
		RecommendationJSON: string(recommendationJSON),
		TraceID:            pact.TraceID,
	}
	if err := s.store.CreateEpisodeSummary(ctx, record); err != nil {
		return
	}
	_ = s.audit(ctx, pact.TraceID, &agent.ID, pact.BuyerID, "agent.episode.summarized", "agent_system", map[string]interface{}{
		"episodeId":      record.ID,
		"auctionId":      pact.AuctionID,
		"outcome":        outcome,
		"recommendation": recommendation,
	})
}

func inferCategory(name string) string {
	text := strings.ToLower(name)
	switch {
	case strings.Contains(text, "jade") || strings.Contains(text, "翡翠"):
		return "jade"
	default:
		return "general"
	}
}
