package agent

import (
	"context"
	"errors"
	"log"
	"time"

	"gorm.io/gorm"
)

// Buyer Agent Runner：常驻任务运行器。
//
// 职责（全部通过现有安全路径，不绕过核心）：
//  1. 周期性扫描 active buyer agent × running 竞拍，匹配意图、按策略决策、经
//     SubmitBuyerBid（含全部安全校验）提交本人出价。
//  2. Win 观察：对已 accepted 的 agent 出价，若其竞拍已 sold 且尚无 Pact，补建 Pact。
//
// 决策本身在 policy.go；安全校验在 service.go。Runner 只负责编排与节奏。

type Runner struct {
	svc      *Service
	interval time.Duration
}

// NewRunner 创建常驻运行器。interval <= 0 时取默认 2s。
func NewRunner(svc *Service, interval time.Duration) *Runner {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &Runner{svc: svc, interval: interval}
}

// Start 阻塞运行直到 ctx 取消；建议在独立 goroutine 中调用。
func (r *Runner) Start(ctx context.Context) {
	log.Printf("[agent-runner] 启动，扫描周期 %s", r.interval)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[agent-runner] 停止")
			return
		case <-ticker.C:
			bids, pacts, err := r.svc.RunnerTickOnce(ctx)
			if err != nil {
				log.Printf("[agent-runner] tick 错误: %v", err)
				continue
			}
			if bids > 0 || pacts > 0 {
				log.Printf("[agent-runner] 本轮自动出价 %d 次，补建 Pact %d 个", bids, pacts)
			}
		}
	}
}

// RunnerTickOnce 执行一轮决策 + Win 观察，返回（出价次数, 补建Pact数）。
// 纯编排，可单独调用用于测试或事件驱动触发。
func (s *Service) RunnerTickOnce(ctx context.Context) (int, int, error) {
	bidCount := s.autoBidPass(ctx)
	pactCount := s.reconcileWinsPass(ctx)
	return bidCount, pactCount, nil
}

// autoBidPass：active buyer agent × running 竞拍 的匹配与出价。
func (s *Service) autoBidPass(ctx context.Context) int {
	agents, err := s.store.ListActiveBuyerAgents(ctx)
	if err != nil || len(agents) == 0 {
		return 0
	}
	auctions, err := s.store.ListRunningAuctions(ctx, 200)
	if err != nil || len(auctions) == 0 {
		return 0
	}

	count := 0
	for i := range agents {
		agent := agents[i]
		// 过期 agent 跳过（SubmitBuyerBid 内部也会再校验并落地 expired）。
		if agent.ExpiresAt != nil && s.now().After(*agent.ExpiresAt) {
			continue
		}
		for j := range auctions {
			auction := auctions[j]
			product, err := s.core.GetProduct(ctx, auction.ProductID)
			if err != nil {
				continue
			}
			bids, err := s.core.ListAuctionBids(ctx, auction.ID, 1000)
			if err != nil {
				continue
			}
			decision := DecideBid(&agent, &auction, product, bids)
			if !decision.ShouldBid {
				continue
			}
			// 经现有安全路径出价；金额由策略引擎决定。
			_, err = s.SubmitBuyerBid(ctx, agent.OwnerUserID, agent.ID, AgentBidInput{
				AuctionID:   auction.ID,
				AmountCents: decision.AmountCents,
			})
			if err == nil {
				count++
				log.Printf("[agent-runner] agent #%d (buyer %d) 出价 auction #%d 金额 %d 分 已提交",
					agent.ID, agent.OwnerUserID, auction.ID, decision.AmountCents)
			} else {
				log.Printf("[agent-runner] agent #%d 出价 auction #%d 失败: %v", agent.ID, auction.ID, err)
			}
		}
	}
	return count
}

// reconcileWinsPass：对 accepted 的 agent 出价，若竞拍已 sold 且无 Pact，则补建 Pact。
// 覆盖「竞拍由结算定时器收尾而非由最后一次出价直接判定 sold」的场景。
func (s *Service) reconcileWinsPass(ctx context.Context) int {
	attempts, err := s.store.ListRecentAcceptedAttempts(ctx, 200)
	if err != nil {
		return 0
	}
	count := 0
	seen := map[uint64]bool{} // 同一 auction 只处理一次
	for i := range attempts {
		a := attempts[i]
		if seen[a.AuctionID] {
			continue
		}
		seen[a.AuctionID] = true

		auction, err := s.core.GetAuction(ctx, a.AuctionID)
		if err != nil || auction.Status != "sold" {
			continue // 竞拍尚未 sold（仍在 running / failed），暂不建 Pact
		}
		order, err := s.core.GetOrderByAuction(ctx, a.AuctionID)
		if err != nil {
			// 已 sold 但订单还没由结算任务补齐，下一轮重试。
			log.Printf("[agent-runner] auction #%d 已 sold 但订单未就绪，等待结算补单: %v", a.AuctionID, err)
			continue
		}
		if _, err := s.store.GetPactByOrder(ctx, order.ID); err == nil {
			continue // Pact 已存在
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		// 仅对赢家是该 agent 买家本人时补建。
		if order.BuyerID != a.BuyerID {
			log.Printf("[agent-runner] auction #%d 赢家 buyer=%d 与 agent attempt buyer=%d 不一致，跳过建 Pact",
				a.AuctionID, order.BuyerID, a.BuyerID)
			continue
		}
		if _, err := s.CreatePactFromWin(ctx, a.AgentID, a.AuctionID, a.TraceID); err == nil {
			count++
			log.Printf("[agent-runner] 已为 auction #%d / order #%d 补建 Pact (buyer %d)", a.AuctionID, order.ID, order.BuyerID)
		} else {
			log.Printf("[agent-runner] auction #%d 建 Pact 失败: %v", a.AuctionID, err)
		}
	}
	return count
}
