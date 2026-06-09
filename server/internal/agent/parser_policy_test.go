package agent

import (
	"context"
	"testing"

	"paimai/internal/model"
)

func TestParseIntent_ChineseBudgetAndKeywords(t *testing.T) {
	s := ParseIntent(context.Background(), "帮我在翡翠专场拍一件冰种挂件，最高 800 元，超过预算不要拍", 0, nil)
	if s.MaxBudgetCents != 80000 {
		t.Fatalf("budget = %d, want 80000", s.MaxBudgetCents)
	}
	if len(s.ProductKeywords) == 0 {
		t.Fatalf("expected keywords parsed from prompt")
	}
	joined := ""
	for _, k := range s.ProductKeywords {
		joined += k + "|"
	}
	if !containsKeyword(s.ProductKeywords, "翡翠") && !containsKeyword(s.ProductKeywords, "冰种") {
		t.Fatalf("expected 翡翠/冰种 in keywords, got %v", s.ProductKeywords)
	}
}

func TestParseIntent_ExplicitOverrides(t *testing.T) {
	s := ParseIntent(context.Background(), "随便拍点东西 最高100元", 50000, []string{"和田玉"})
	if s.MaxBudgetCents != 50000 {
		t.Fatalf("explicit budget should win, got %d", s.MaxBudgetCents)
	}
	if len(s.ProductKeywords) != 1 || s.ProductKeywords[0] != "和田玉" {
		t.Fatalf("explicit keywords should win, got %v", s.ProductKeywords)
	}
}

func TestParseBudget_WanUnit(t *testing.T) {
	if got := parseBudgetCents("预算1.2万"); got != 1200000 {
		t.Fatalf("1.2万 = %d, want 1200000", got)
	}
}

func TestDecideBid_WithinBudgetNotLeading(t *testing.T) {
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{"productKeywords":["翡翠"]}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 10000, BidIncrementCents: 1000, CapPriceCents: 0}
	product := &model.Product{Name: "冰种翡翠挂件"}
	bids := []model.Bid{{UserID: 9, AmountCents: 10000, Accepted: true}}

	d := DecideBid(agent, auction, product, bids)
	if !d.ShouldBid || d.AmountCents != 11000 {
		t.Fatalf("expected bid 11000, got %+v", d)
	}
}

func TestDecideBid_AlreadyLeadingSkips(t *testing.T) {
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{"productKeywords":["x"]}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 10000, BidIncrementCents: 1000}
	product := &model.Product{Name: "x"}
	bids := []model.Bid{{UserID: 7, AmountCents: 10000, Accepted: true}}

	if d := DecideBid(agent, auction, product, bids); d.ShouldBid {
		t.Fatalf("should skip when already leading, got %+v", d)
	}
}

func TestDecideBid_ExceedsBudgetSkips(t *testing.T) {
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 10500, StrategyJSON: `{"productKeywords":["x"]}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 10000, BidIncrementCents: 1000}
	product := &model.Product{Name: "x"}
	bids := []model.Bid{{UserID: 9, AmountCents: 10000, Accepted: true}}

	if d := DecideBid(agent, auction, product, bids); d.ShouldBid {
		t.Fatalf("should skip when next bid exceeds budget, got %+v", d)
	}
}

func TestDecideBid_CapPriceConverges(t *testing.T) {
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{"productKeywords":["x"]}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 10000, BidIncrementCents: 5000, CapPriceCents: 12000}
	product := &model.Product{Name: "x"}
	bids := []model.Bid{{UserID: 9, AmountCents: 10000, Accepted: true}}

	d := DecideBid(agent, auction, product, bids)
	if !d.ShouldBid || d.AmountCents != 12000 {
		t.Fatalf("expected capped bid 12000, got %+v", d)
	}
}

func TestDecideBid_MeetsReservePrice(t *testing.T) {
	reserve := int64(50000)
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{"buyerId":7,"productKeywords":["x"],"maxBudgetCents":80000,"strategy":"reserve_then_follow","maxBidTimes":5,"minIntervalMs":3000,"requireHumanPay":true}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 10000, BidIncrementCents: 1000, ReservePriceCents: &reserve}
	product := &model.Product{Name: "x"}
	bids := []model.Bid{{UserID: 9, AmountCents: 10000, Accepted: true}}

	d := DecideBid(agent, auction, product, bids)
	if !d.ShouldBid || d.AmountCents != 50000 {
		t.Fatalf("expected bid raised to reserve 50000, got %+v", d)
	}
}

func TestDecideBid_ReserveAboveBudgetSkips(t *testing.T) {
	reserve := int64(90000)
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{"buyerId":7,"productKeywords":["x"],"maxBudgetCents":80000,"strategy":"reserve_then_follow","maxBidTimes":5,"minIntervalMs":3000,"requireHumanPay":true}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 10000, BidIncrementCents: 1000, ReservePriceCents: &reserve}
	product := &model.Product{Name: "x"}
	bids := []model.Bid{{UserID: 9, AmountCents: 10000, Accepted: true}}

	if d := DecideBid(agent, auction, product, bids); d.ShouldBid {
		t.Fatalf("should skip when reserve exceeds budget, got %+v", d)
	}
}

func TestDecideBid_NoKeywordsDisablesAutoBid(t *testing.T) {
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 10000, BidIncrementCents: 1000}
	product := &model.Product{Name: "任意商品"}
	bids := []model.Bid{{UserID: 9, AmountCents: 10000, Accepted: true}}

	if d := DecideBid(agent, auction, product, bids); d.ShouldBid {
		t.Fatalf("agent without keywords must not auto-bid (carpet-bid guard), got %+v", d)
	}
}

func TestDecideBid_ConservativeWaitsAboveSixtyPercent(t *testing.T) {
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{"buyerId":7,"productKeywords":["x"],"maxBudgetCents":80000,"strategy":"conservative","maxBidTimes":5,"minIntervalMs":3000,"requireHumanPay":true}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 49000, BidIncrementCents: 1000}
	product := &model.Product{Name: "x"}
	bids := []model.Bid{{UserID: 9, AmountCents: 49000, Accepted: true}}

	if d := DecideBid(agent, auction, product, bids); d.ShouldBid {
		t.Fatalf("conservative should wait above 60%% budget, got %+v", d)
	}
}

func TestDecideBid_FollowUpDoesNotOpenAuction(t *testing.T) {
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{"buyerId":7,"productKeywords":["x"],"maxBudgetCents":80000,"strategy":"follow_up","maxBidTimes":5,"minIntervalMs":3000,"requireHumanPay":true}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 10000, BidIncrementCents: 1000}
	product := &model.Product{Name: "x"}

	if d := DecideBid(agent, auction, product, nil); d.ShouldBid {
		t.Fatalf("follow_up should wait for another leading buyer, got %+v", d)
	}
}

func TestDecideBid_CustomBudgetRatio(t *testing.T) {
	agent := &model.AgentProfile{OwnerUserID: 7, MaxBudgetCents: 80000, StrategyJSON: `{"buyerId":7,"productKeywords":["x"],"maxBudgetCents":80000,"strategy":"custom","maxBidTimes":5,"minIntervalMs":3000,"requireHumanPay":true,"custom":{"budgetRatio":0.7,"followUp":true}}`}
	auction := &model.Auction{Status: "running", CurrentPriceCents: 57000, BidIncrementCents: 1000}
	product := &model.Product{Name: "x"}
	bids := []model.Bid{{UserID: 9, AmountCents: 57000, Accepted: true}}

	if d := DecideBid(agent, auction, product, bids); d.ShouldBid {
		t.Fatalf("custom should wait above budget ratio, got %+v", d)
	}
}

func TestBuildStrategySkill_CapOnlyFoldsOverBudget(t *testing.T) {
	parsed := ParsedStrategy{MaxBudgetCents: 80000, ProductKeywords: []string{"球鞋"}}
	in := CreateBuyerAgentInput{
		Strategy:        StrategyCapOnly,
		MaxBudgetCents:  80000,
		OverBudgetCents: 5000,
		ProductKeywords: []string{"球鞋"},
	}
	skill, err := buildStrategySkill(7, in, parsed)
	if err != nil {
		t.Fatalf("buildStrategySkill error: %v", err)
	}
	if skill.MaxBudgetCents != 85000 {
		t.Fatalf("effective budget = %d, want 85000 (base+over)", skill.MaxBudgetCents)
	}
	if skill.OverBudgetCents != 5000 || skill.BaseBudgetCents != 80000 {
		t.Fatalf("over/base not recorded: %+v", skill)
	}
}

func TestBuildStrategySkill_CustomText(t *testing.T) {
	parsed := ParsedStrategy{MaxBudgetCents: 50000, ProductKeywords: []string{"手办"}}
	in := CreateBuyerAgentInput{
		Strategy:        StrategyCustom,
		MaxBudgetCents:  50000,
		CustomText:      "只在最后跟价，最多加价3次",
		ProductKeywords: []string{"手办"},
	}
	skill, err := buildStrategySkill(7, in, parsed)
	if err != nil {
		t.Fatalf("buildStrategySkill error: %v", err)
	}
	if skill.CustomText != "只在最后跟价，最多加价3次" {
		t.Fatalf("custom text not stored: %q", skill.CustomText)
	}
	if !skill.Custom.FollowUp {
		t.Fatalf("custom text 跟价 should set FollowUp: %+v", skill.Custom)
	}
}

func containsKeyword(list []string, want string) bool {
	for _, k := range list {
		if k == want {
			return true
		}
	}
	return false
}
