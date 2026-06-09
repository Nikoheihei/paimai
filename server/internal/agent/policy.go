package agent

import (
	"encoding/json"
	"strings"

	"paimai/internal/model"
)

// 实时策略决策引擎：给定 agent 策略 + 当前竞拍快照，决定「是否出价 / 出多少」。
//
// 纯函数、无副作用，便于单测与被 Runner 复用。所有金额单位为分。
// 安全边界仍由 Service.SubmitBuyerBid 二次校验，这里只做决策。
//
// 决策逻辑由3个正交维度驱动：
//   - Trigger（触发方式）：lead=主动出价 / follow=跟价模式
//   - Pace（出价节奏）：min_step=最小步长 / reserve=保留价优先
//   - StopRatio（停止条件）：0=仅预算硬约束 / 0-1=预算比例停止

type BidDecision struct {
	ShouldBid   bool   `json:"shouldBid"`
	AmountCents int64  `json:"amountCents"`
	Reason      string `json:"reason"`
}

// DecideBid 计算买家 agent 对某个 running 竞拍的出价决策。
//   - agent：买家 agent（含预算与策略）
//   - auction：竞拍当前快照（当前价、加价、封顶）
//   - product：竞拍商品（用于关键词匹配）
//   - bids：当前出价链（按时间升序或降序均可），用于判断是否已是最高出价者
func DecideBid(agent *model.AgentProfile, auction *model.Auction, product *model.Product, bids []model.Bid) BidDecision {
	if agent == nil || auction == nil || product == nil {
		return BidDecision{Reason: "missing input"}
	}
	if auction.Status != "running" {
		return BidDecision{Reason: "auction not running"}
	}
	skill, err := decodeStrategySkill(agent)
	if err != nil {
		return BidDecision{Reason: "invalid strategy skill: " + err.Error()}
	}
	if err := ensureAuctionMatchesSkill(skill, auction); err != nil {
		return BidDecision{Reason: "auction outside strategy scope"}
	}
	if skill.BuyerID != agent.OwnerUserID {
		return BidDecision{Reason: "strategy buyer does not own agent"}
	}
	if !skill.RequireHumanPay {
		return BidDecision{Reason: "human payment approval required"}
	}

	// 1. 商品意图匹配。
	// 自动出价要求 agent 必须有明确关键词，否则会对全场每个竞拍无差别出价
	// （"幽灵竞争者"问题）。无关键词的 agent 不自动出价，只能手动出价。
	if !hasKeywords(agent.StrategyJSON) {
		return BidDecision{Reason: "agent has no product keywords; auto-bid disabled to avoid bidding on every auction"}
	}
	if !productMatchesStrategy(agent.StrategyJSON, product) {
		return BidDecision{Reason: "product does not match intent"}
	}

	// 2. 已是最高出价者则不抬价（避免自我加价 / 出价风暴）。
	topBidder := topAcceptedBidder(bids)
	if topBidder == agent.OwnerUserID {
		return BidDecision{Reason: "already highest bidder"}
	}

	// 3. 维度1：触发方式（Trigger）
	if skill.Trigger == TriggerFollow {
		if topBidder == 0 {
			return BidDecision{Reason: "follow mode waits until another buyer leads"}
		}
	}
	// TriggerLead：不需要任何前提条件，直接继续

	// 4. 维度2：出价节奏（Pace）+ 计算目标价
	increment := auction.BidIncrementCents
	if increment <= 0 {
		increment = 1
	}
	target := auction.CurrentPriceCents + increment
	reason := "lead strategy bids next increment"

	if skill.Pace == PaceReserve {
		if auction.ReservePriceCents != nil && *auction.ReservePriceCents > target {
			target = *auction.ReservePriceCents
			reason = "reserve priority: bid to reserve price first"
		}
	}
	// PaceMinStep：target 已经是 currentPrice + increment

	// 5. 维度3：停止条件（StopRatio）
	if skill.StopRatio > 0 && skill.StopRatio < 1 {
		stopPrice := int64(float64(skill.MaxBudgetCents) * skill.StopRatio)
		if auction.CurrentPriceCents >= stopPrice {
			return BidDecision{Reason: "price reached stop ratio threshold"}
		}
		if target > stopPrice {
			target = stopPrice
			reason = "target capped by stop ratio"
		}
	}
	// StopRatio == 0：仅受预算硬约束，不额外限制

	// 6. 补充 reason：标注 trigger 模式
	if skill.Trigger == TriggerFollow {
		reason = "follow mode: " + reason
	}

	// 7. 封顶价约束
	if auction.CapPriceCents > 0 && target > auction.CapPriceCents {
		target = auction.CapPriceCents
	}

	// 8. 预算硬约束。
	if target > skill.MaxBudgetCents || target > agent.MaxBudgetCents {
		return BidDecision{Reason: "next bid would exceed budget"}
	}
	// 达不到保留价就放弃
	if auction.ReservePriceCents != nil && target < *auction.ReservePriceCents {
		return BidDecision{Reason: "cannot reach reserve price within cap/budget"}
	}
	if target <= auction.CurrentPriceCents {
		return BidDecision{Reason: "no legal increment available"}
	}

	return BidDecision{
		ShouldBid:   true,
		AmountCents: target,
		Reason:      reason,
	}
}

// hasKeywords 判断 agent 策略是否含至少一个有效关键词。
func hasKeywords(strategyJSON string) bool {
	var strategy StrategySkill
	if err := json.Unmarshal([]byte(strategyJSON), &strategy); err != nil {
		return false
	}
	return len(normalizeKeywords(strategy.ProductKeywords)) > 0
}

// productMatchesStrategy 判断商品是否命中策略关键词；无关键词视为广义匹配（命中）。
func productMatchesStrategy(strategyJSON string, product *model.Product) bool {
	var strategy StrategySkill
	if err := json.Unmarshal([]byte(strategyJSON), &strategy); err != nil {
		return false
	}
	keywords := normalizeKeywords(strategy.ProductKeywords)
	if len(keywords) == 0 {
		return true
	}
	text := strings.ToLower(product.Name + " " + product.Description)
	for _, kw := range keywords {
		if strings.Contains(text, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}

// topAcceptedBidder 返回当前出价链中金额最高的有效出价者 userID；无则返回 0。
func topAcceptedBidder(bids []model.Bid) uint64 {
	var topUser uint64
	var topAmount int64 = -1
	for _, b := range bids {
		if !b.Accepted {
			continue
		}
		if b.AmountCents > topAmount {
			topAmount = b.AmountCents
			topUser = b.UserID
		}
	}
	return topUser
}
