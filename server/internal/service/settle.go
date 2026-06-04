package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
	"paimai/internal/statemachine"
)

// SettleService 负责竞拍结束后的结算、订单生成和支付模拟。
type SettleService struct {
	adminStore repository.AdminStore
	now        func() time.Time
}

// NewSettleService 创建结算服务。
func NewSettleService(adminStore repository.AdminStore) *SettleService {
	return &SettleService{
		adminStore: adminStore,
		now:        time.Now,
	}
}

// SettleResult 是单次结算的结果。
type SettleResult struct {
	AuctionID       uint64  `json:"auctionId"`
	Settled         bool    `json:"settled"`         // 是否执行了结算（已结算过的返回 false）
	Status          string  `json:"status"`          // 结算后的竞拍状态
	OrderID         *uint64 `json:"orderId,omitempty"` // 成交时生成的订单 ID
	FinalPriceCents int64   `json:"finalPriceCents"`
}

// SettleAuction 对指定竞拍执行结算。
// 幂等：已结算（sold/failed/cancelled）的竞拍直接返回已有订单信息。
func (s *SettleService) SettleAuction(ctx context.Context, auctionID uint64) (*SettleResult, error) {
	auction, err := s.adminStore.GetAuction(ctx, auctionID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// 幂等：已结算的竞拍不再处理
	if auction.Status == string(statemachine.StateSold) {
		order, err := s.adminStore.GetOrderByAuction(ctx, auctionID)
		if err == nil {
			return &SettleResult{
				AuctionID:       auctionID,
				Settled:         false,
				Status:          auction.Status,
				OrderID:         &order.ID,
				FinalPriceCents: auction.CurrentPriceCents,
			}, nil
		}
		// 有 sold 状态但没有订单（数据异常），尝试生成
	}
	if auction.Status == string(statemachine.StateFailed) || auction.Status == string(statemachine.StateCancelled) {
		return &SettleResult{
			AuctionID: auctionID,
			Settled:   false,
			Status:    auction.Status,
		}, nil
	}

	// 只有 running 状态才允许结算
	if auction.Status != string(statemachine.StateRunning) {
		return nil, fmt.Errorf("%w: 只能结算进行中的竞拍, 当前状态: %s", ErrInvalidTransition, auction.Status)
	}

	return s.doExecuteSettle(ctx, auction)
}

// doExecuteSettle 执行实际的结算逻辑，不幂等校验。
func (s *SettleService) doExecuteSettle(ctx context.Context, auction *model.Auction) (*SettleResult, error) {
	now := s.now()

	// 判断成交还是流拍
	if auction.WinnerUserID == nil {
		// 无人出价 → 流拍
		auction.Status = string(statemachine.StateFailed)
		if err := s.adminStore.UpdateAuction(ctx, auction); err != nil {
			return nil, err
		}
		return &SettleResult{
			AuctionID:       auction.ID,
			Settled:         true,
			Status:          string(statemachine.StateFailed),
			FinalPriceCents: auction.CurrentPriceCents,
		}, nil
	}

	// 有保留价但未达到 → 流拍
	if auction.ReservePriceCents != nil && auction.CurrentPriceCents < *auction.ReservePriceCents {
		auction.Status = string(statemachine.StateFailed)
		if err := s.adminStore.UpdateAuction(ctx, auction); err != nil {
			return nil, err
		}
		return &SettleResult{
			AuctionID:       auction.ID,
			Settled:         true,
			Status:          string(statemachine.StateFailed),
			FinalPriceCents: auction.CurrentPriceCents,
		}, nil
	}

	// 成交
	auction.Status = string(statemachine.StateSold)
	if err := s.adminStore.UpdateAuction(ctx, auction); err != nil {
		return nil, err
	}

	// 从直播间获取真正的 SellerID
	room, roomErr := s.adminStore.GetRoom(ctx, auction.RoomID)
	if roomErr != nil {
		return nil, fmt.Errorf("获取直播间信息失败: %w", roomErr)
	}
	order := &model.Order{
		AuctionID:       auction.ID,
		ProductID:       auction.ProductID,
		BuyerID:         *auction.WinnerUserID,
		SellerID:        room.SellerID,
		FinalPriceCents: auction.CurrentPriceCents,
		Status:          "pending_payment",
		CreatedAt:       now,
	}
	if err := s.adminStore.CreateOrder(ctx, order); err != nil {
		return nil, err
	}

	return &SettleResult{
		AuctionID:       auction.ID,
		Settled:         true,
		Status:          string(statemachine.StateSold),
		OrderID:         &order.ID,
		FinalPriceCents: auction.CurrentPriceCents,
	}, nil
}

// PayOrder 模拟支付——直接将订单状态从 pending_payment 设为 paid。
// 幂等：已支付的订单直接返回成功。
func (s *SettleService) PayOrder(ctx context.Context, orderID uint64) (*model.Order, error) {
	order, err := s.adminStore.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if order.Status == "paid" {
		return order, nil
	}
	if order.Status == "closed" {
		return nil, fmt.Errorf("%w: 订单已关闭，无法支付", ErrInvalidTransition)
	}

	now := s.now()
	order.Status = "paid"
	order.PaidAt = &now
	if err := s.adminStore.UpdateOrder(ctx, order); err != nil {
		return nil, err
	}
	return order, nil
}

// ListOrders 返回订单列表（按创建时间倒序）。
func (s *SettleService) ListOrders(ctx context.Context) ([]model.Order, error) {
	return s.adminStore.ListOrders(ctx)
}

// ListSellerOrders 返回指定商家的订单列表。
func (s *SettleService) ListSellerOrders(ctx context.Context, sellerID uint64) ([]model.Order, error) {
	return s.adminStore.ListOrdersBySeller(ctx, sellerID)
}

// SettleExpiredAuctions 结算所有已过期但仍在 running 的竞拍（启动时调用）。
func (s *SettleService) SettleExpiredAuctions(ctx context.Context) (int, error) {
	auctions, err := s.adminStore.ListRunningExpiredAuctions(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, auction := range auctions {
		// 取指针副本
		a := auction
		if _, err := s.doExecuteSettle(ctx, &a); err != nil {
			return count, fmt.Errorf("结算竞拍 %d 失败: %w", auction.ID, err)
		}
		count++
	}
	return count, nil
}
