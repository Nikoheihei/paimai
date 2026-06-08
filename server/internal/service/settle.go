package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
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

const PaymentWindow = 5 * time.Minute

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
	Settled         bool    `json:"settled"`           // 是否执行了结算（已结算过的返回 false）
	Status          string  `json:"status"`            // 结算后的竞拍状态
	OrderID         *uint64 `json:"orderId,omitempty"` // 成交时生成的订单 ID
	FinalPriceCents int64   `json:"finalPriceCents"`
}

// OrderDetail 是买家端订单展示 DTO，在原订单字段基础上补充商品和商家信息。
type OrderDetail struct {
	model.Order
	ProductName    string `json:"productName"`
	ProductImage   string `json:"productImage"`
	SellerNickname string `json:"sellerNickname"`
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
	if auction.Status == string(statemachine.StateFailed) ||
		auction.Status == string(statemachine.StateCancelled) ||
		auction.Status == string(statemachine.StatePaymentTimeout) {
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
			if err := tx.UpdateProductStatus(ctx, auction.ProductID, ProductStatusAvailable); err != nil {
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
			if err := tx.UpdateProductStatus(ctx, auction.ProductID, ProductStatusAvailable); err != nil {
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

		// 写入 Outbox 事件，通知前端刷新订单
		eventPayload, _ := json.Marshal(map[string]interface{}{
			"type":      "order.created",
			"auctionId": auction.ID,
			"orderId":   order.ID,
			"buyerId":   order.BuyerID,
			"sellerId":  order.SellerID,
			"roomId":    auction.RoomID,
		})
		if err := tx.CreateOutboxEvent(ctx, &model.OutboxEvent{
			EventType: "order.created",
			Payload:   string(eventPayload),
			Status:    "pending",
			EventUUID: fmt.Sprintf("order-%d-%d", auction.ID, order.ID),
		}); err != nil {
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

// PayOrder 模拟支付，并以 MySQL 订单状态作为权威判断支付窗口。
// 幂等：已支付的订单直接返回成功；超时订单会在事务内关闭并释放商品。
func (s *SettleService) PayOrder(ctx context.Context, orderID uint64, input PayOrderInput) (*model.Order, error) {
	var paid *model.Order
	if err := s.adminStore.WithTx(ctx, func(tx repository.AdminStore) error {
		order, err := tx.GetOrder(ctx, orderID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		switch order.Status {
		case "paid":
			paid = order
			return nil
		case "closed":
			return fmt.Errorf("%w: 订单已关闭，无法支付", ErrOrderPaymentTimeout)
		case "pending_payment":
		default:
			return fmt.Errorf("%w: 当前订单状态不允许支付: %s", ErrInvalidTransition, order.Status)
		}

		now := s.now()
		if isPaymentExpired(order, now) {
			if _, err := s.closePaymentTimeoutTx(ctx, tx, order, now); err != nil {
				return err
			}
			return ErrOrderPaymentTimeout
		}

		if err := tx.UpdateOrderStatus(ctx, orderID, "paid", &now, input.AddressID, input.AddressSnapshot); err != nil {
			refreshed, refreshErr := tx.GetOrder(ctx, orderID)
			if refreshErr == nil {
				if refreshed.Status == "paid" {
					paid = refreshed
					return nil
				}
				if refreshed.Status == "closed" {
					return ErrOrderPaymentTimeout
				}
			}
			return err
		}

		order.Status = "paid"
		order.PaidAt = &now
		if input.AddressID != nil {
			order.AddressID = input.AddressID
		}
		if input.AddressSnapshot != "" {
			order.AddressSnapshot = input.AddressSnapshot
		}
		if err := createOrderPaidEvent(ctx, tx, order); err != nil {
			return err
		}
		paid = order
		return nil
	}); err != nil {
		return nil, err
	}
	return paid, nil
}

// PayBuyerOrder 模拟支付当前买家的订单，并校验订单归属。
func (s *SettleService) PayBuyerOrder(ctx context.Context, orderID uint64, buyerID uint64, input PayOrderInput) (*OrderDetail, error) {
	if buyerID == 0 {
		return nil, ErrUnauthorized
	}
	order, err := s.adminStore.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if order.BuyerID != buyerID {
		return nil, ErrUnauthorized
	}
	if order.Status != "paid" && (input.AddressID == nil || strings.TrimSpace(input.AddressSnapshot) == "") {
		return nil, fmt.Errorf("%w: address is required before payment", ErrInvalidInput)
	}
	paid, err := s.PayOrder(ctx, orderID, input)
	if err != nil {
		return nil, err
	}
	return s.enrichOrder(ctx, *paid), nil
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

// GetBuyerOrder 查询当前买家的订单详情，并校验订单归属。
func (s *SettleService) GetBuyerOrder(ctx context.Context, orderID uint64, buyerID uint64) (*OrderDetail, error) {
	if buyerID == 0 {
		return nil, ErrUnauthorized
	}
	order, err := s.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.BuyerID != buyerID {
		return nil, ErrUnauthorized
	}
	return s.enrichOrder(ctx, *order), nil
}

// ListOrders 返回订单列表（按创建时间倒序）。
func (s *SettleService) ListOrders(ctx context.Context) ([]model.Order, error) {
	return s.adminStore.ListOrders(ctx)
}

// ListSellerOrders 返回指定商家的订单列表。
func (s *SettleService) ListSellerOrders(ctx context.Context, sellerID uint64) ([]model.Order, error) {
	return s.adminStore.ListOrdersBySeller(ctx, sellerID)
}

// ListBuyerOrders 返回指定买家的订单列表。
func (s *SettleService) ListBuyerOrders(ctx context.Context, buyerID uint64) ([]OrderDetail, error) {
	orders, err := s.adminStore.ListOrdersByBuyer(ctx, buyerID)
	if err != nil {
		return nil, err
	}
	return s.enrichOrders(ctx, orders), nil
}

// CloseExpiredPaymentOrders 扫描并关闭支付超时的待支付订单。
// 该任务可重复执行；单个订单的状态条件更新和事件 UUID 保证幂等。
func (s *SettleService) CloseExpiredPaymentOrders(ctx context.Context, limit int) (int, error) {
	cutoff := s.now().Add(-PaymentWindow)
	orders, err := s.adminStore.ListExpiredPendingOrders(ctx, cutoff, limit)
	if err != nil {
		return 0, err
	}

	closed := 0
	var errs []error
	for _, order := range orders {
		ok, err := s.closePaymentTimeoutOrder(ctx, order.ID)
		if err != nil {
			if errors.Is(err, ErrInvalidTransition) || errors.Is(err, ErrOrderPaymentTimeout) {
				continue
			}
			errs = append(errs, fmt.Errorf("订单 %d: %w", order.ID, err))
			continue
		}
		if ok {
			closed++
		}
	}
	if len(errs) > 0 {
		return closed, fmt.Errorf("部分支付超时订单关闭失败: %v", errors.Join(errs...))
	}
	return closed, nil
}

func (s *SettleService) closePaymentTimeoutOrder(ctx context.Context, orderID uint64) (bool, error) {
	closed := false
	err := s.adminStore.WithTx(ctx, func(tx repository.AdminStore) error {
		order, err := tx.GetOrder(ctx, orderID)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		ok, err := s.closePaymentTimeoutTx(ctx, tx, order, s.now())
		if err != nil {
			return err
		}
		closed = ok
		return nil
	})
	return closed, err
}

func (s *SettleService) closePaymentTimeoutTx(ctx context.Context, tx repository.AdminStore, order *model.Order, now time.Time) (bool, error) {
	switch order.Status {
	case "paid":
		return false, nil
	case "closed":
		return false, nil
	case "pending_payment":
	default:
		return false, fmt.Errorf("%w: 当前订单状态不允许关闭: %s", ErrInvalidTransition, order.Status)
	}
	if !isPaymentExpired(order, now) {
		return false, nil
	}

	if err := tx.UpdateOrderStatus(ctx, order.ID, "closed", nil, nil, ""); err != nil {
		refreshed, refreshErr := tx.GetOrder(ctx, order.ID)
		if refreshErr == nil {
			switch refreshed.Status {
			case "paid", "closed":
				return false, nil
			}
		}
		return false, err
	}
	order.Status = "closed"

	auction, err := tx.GetAuction(ctx, order.AuctionID)
	if err != nil {
		return false, err
	}
	if auction.Status == string(statemachine.StateSold) {
		next, err := transition(auction.Status, statemachine.EventPaymentTimeout)
		if err != nil {
			return false, err
		}
		auction.Status = string(next)
		if err := tx.UpdateAuction(ctx, auction); err != nil {
			return false, err
		}
	}
	if err := tx.UpdateProductStatus(ctx, order.ProductID, ProductStatusAvailable); err != nil {
		return false, err
	}
	if err := createOrderClosedEvent(ctx, tx, order, auction, "payment_timeout"); err != nil {
		return false, err
	}
	if err := createAuctionPaymentTimeoutEvent(ctx, tx, order, auction); err != nil {
		return false, err
	}
	return true, nil
}

func isPaymentExpired(order *model.Order, now time.Time) bool {
	return now.After(order.CreatedAt.Add(PaymentWindow))
}

func createOrderPaidEvent(ctx context.Context, store repository.AdminStore, order *model.Order) error {
	auction, err := store.GetAuction(ctx, order.AuctionID)
	if err != nil {
		return err
	}
	eventID := fmt.Sprintf("order-paid-%d-%d", order.AuctionID, order.ID)
	eventPayload, _ := json.Marshal(map[string]interface{}{
		"type":      "order.paid",
		"auctionId": order.AuctionID,
		"orderId":   order.ID,
		"buyerId":   order.BuyerID,
		"sellerId":  order.SellerID,
		"roomId":    auction.RoomID,
		"status":    order.Status,
		"eventId":   eventID,
	})
	return store.CreateOutboxEvent(ctx, &model.OutboxEvent{
		EventType: "order.paid",
		Payload:   string(eventPayload),
		Status:    "pending",
		EventUUID: eventID,
	})
}

func createOrderClosedEvent(ctx context.Context, store repository.AdminStore, order *model.Order, auction *model.Auction, reason string) error {
	eventID := fmt.Sprintf("order-closed-%d-%s", order.ID, reason)
	eventPayload, _ := json.Marshal(map[string]interface{}{
		"type":      "order.closed",
		"eventId":   eventID,
		"orderId":   order.ID,
		"auctionId": order.AuctionID,
		"productId": order.ProductID,
		"buyerId":   order.BuyerID,
		"sellerId":  order.SellerID,
		"roomId":    auction.RoomID,
		"status":    order.Status,
		"reason":    reason,
	})
	return store.CreateOutboxEvent(ctx, &model.OutboxEvent{
		EventType: "order.closed",
		Payload:   string(eventPayload),
		Status:    "pending",
		EventUUID: eventID,
	})
}

func createAuctionPaymentTimeoutEvent(ctx context.Context, store repository.AdminStore, order *model.Order, auction *model.Auction) error {
	eventID := fmt.Sprintf("auction-payment-timeout-%d-%d", auction.ID, order.ID)
	eventPayload, _ := json.Marshal(map[string]interface{}{
		"type":      "auction.payment_timeout",
		"eventId":   eventID,
		"auctionId": auction.ID,
		"productId": auction.ProductID,
		"roomId":    auction.RoomID,
		"orderId":   order.ID,
		"status":    auction.Status,
		"reason":    "payment_timeout",
	})
	return store.CreateOutboxEvent(ctx, &model.OutboxEvent{
		EventType: "auction.payment_timeout",
		Payload:   string(eventPayload),
		Status:    "pending",
		EventUUID: eventID,
	})
}

func (s *SettleService) enrichOrders(ctx context.Context, orders []model.Order) []OrderDetail {
	result := make([]OrderDetail, 0, len(orders))
	for _, order := range orders {
		result = append(result, *s.enrichOrder(ctx, order))
	}
	return result
}

func (s *SettleService) enrichOrder(ctx context.Context, order model.Order) *OrderDetail {
	detail := &OrderDetail{Order: order}
	if product, err := s.adminStore.GetProduct(ctx, order.ProductID); err == nil && product != nil {
		detail.ProductName = product.Name
		detail.ProductImage = product.ImageURL
	}
	if seller, err := s.adminStore.GetUser(ctx, order.SellerID); err == nil && seller != nil {
		detail.SellerNickname = seller.Nickname
	}
	return detail
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
