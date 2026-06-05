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

	// running 或 sold（PlaceBid 触顶成交时已设置）均可结算
	if auction.Status != string(statemachine.StateRunning) && auction.Status != string(statemachine.StateSold) {
		return nil, fmt.Errorf("%w: 只能结算进行中的竞拍, 当前状态: %s", ErrInvalidTransition, auction.Status)
	}

	return s.doExecuteSettle(ctx, auction)
}

// doExecuteSettle 执行实际的结算逻辑，不幂等校验。
// 所有 DB 操作在同一个事务中执行，保证原子性。
func (s *SettleService) doExecuteSettle(ctx context.Context, auction *model.Auction) (*SettleResult, error) {
	var result *SettleResult
	if err := s.adminStore.WithTx(ctx, func(tx repository.AdminStore) error {
		now := s.now()

		// 无人出价 → 流拍
		if auction.WinnerUserID == nil {
			auction.Status = string(statemachine.StateFailed)
			if err := tx.UpdateAuction(ctx, auction); err != nil {
				return err
			}
			result = &SettleResult{
				AuctionID:       auction.ID,
				Settled:         true,
				Status:          string(statemachine.StateFailed),
				FinalPriceCents: auction.CurrentPriceCents,
			}
			return nil
		}

		// 保留价未达到 → 流拍
		if auction.ReservePriceCents != nil && auction.CurrentPriceCents < *auction.ReservePriceCents {
			auction.Status = string(statemachine.StateFailed)
			if err := tx.UpdateAuction(ctx, auction); err != nil {
				return err
			}
			result = &SettleResult{
				AuctionID:       auction.ID,
				Settled:         true,
				Status:          string(statemachine.StateFailed),
				FinalPriceCents: auction.CurrentPriceCents,
			}
			return nil
		}

		// 成交：设置竞拍状态为 sold（手动/启动时结算需要；PlaceBid 触发时虽然已设置，但幂等 UpdateAuction 不影响）
		auction.Status = string(statemachine.StateSold)
		if err := tx.UpdateAuction(ctx, auction); err != nil {
			return err
		}

		room, roomErr := tx.GetRoom(ctx, auction.RoomID)
		if roomErr != nil {
			return fmt.Errorf("获取直播间信息失败: %w", roomErr)
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
		if err := tx.CreateOrder(ctx, order); err != nil {
			return err
		}
		result = &SettleResult{
			AuctionID:       auction.ID,
			Settled:         true,
			Status:          string(statemachine.StateSold),
			OrderID:         &order.ID,
			FinalPriceCents: auction.CurrentPriceCents,
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// PayOrderInput 是支付订单的输入参数。
type PayOrderInput struct {
	AddressID       *uint64 `json:"addressId"`
	AddressSnapshot string  `json:"addressSnapshot"`
}

// PayOrder 模拟支付——直接将订单状态从 pending_payment 设为 paid，并记录收货地址。
// 幂等：已支付的订单直接返回成功。
func (s *SettleService) PayOrder(ctx context.Context, orderID uint64, input PayOrderInput) (*model.Order, error) {
	order, err := s.adminStore.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	// 已支付 → 幂等返回
	if order.Status == "paid" {
		return order, nil
	}

	// 条件更新：只有 pending_payment 才能转为 paid
	now := s.now()
	if err := s.adminStore.UpdateOrderStatus(ctx, orderID, "paid", &now, input.AddressID, input.AddressSnapshot); err != nil {
		// RowsAffected=0 或 status 不匹配 → 重新读取确认真实状态
		refreshed, refreshErr := s.adminStore.GetOrder(ctx, orderID)
		if refreshErr == nil {
			if refreshed.Status == "paid" {
				return refreshed, nil
			}
			if refreshed.Status == "closed" {
				return nil, fmt.Errorf("%w: 订单已关闭，无法支付", ErrInvalidTransition)
			}
		}
		return nil, err
	}

	order.Status = "paid"
	order.PaidAt = &now
	if input.AddressID != nil {
		order.AddressID = input.AddressID
	}
	if input.AddressSnapshot != "" {
		order.AddressSnapshot = input.AddressSnapshot
	}
	return order, nil
}

// GetOrder 查询订单详情。
func (s *SettleService) GetOrder(ctx context.Context, orderID uint64) (*model.Order, error) {
	order, err := s.adminStore.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
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
	var errs []error
	for _, auction := range auctions {
		a := auction
		if _, err := s.doExecuteSettle(ctx, &a); err != nil {
			errs = append(errs, fmt.Errorf("竞拍 %d: %w", auction.ID, err))
			continue
		}
		count++
	}
	if len(errs) > 0 {
		return count, fmt.Errorf("部分结算失败: %v", errors.Join(errs...))
	}
	return count, nil
}
