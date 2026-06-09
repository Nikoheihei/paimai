package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"paimai/internal/service"
	"paimai/pkg/response"
)

// SettleHandler 负责结算与订单管理的 HTTP 协议适配。
type SettleHandler struct {
	settleService *service.SettleService
	pactGate      PactPaymentGate
}

// PactPaymentGate is an optional pre-payment guard for agent-assisted orders.
// Manual buyer orders pass through when the gate finds no Pact for the order.
type PactPaymentGate interface {
	CheckBuyerPaymentAllowed(ctx context.Context, orderID, buyerID uint64) error
	AuditPaymentResult(ctx context.Context, orderID, buyerID uint64, paid bool, payErr error)
}

// RegisterAdminSettleRoutes 注册后台结算与订单管理路由。
func RegisterAdminSettleRoutes(r gin.IRouter, settleService *service.SettleService) {
	h := &SettleHandler{settleService: settleService}
	{
		r.POST("/auctions/:id/settle", h.settleAuction)
		r.POST("/orders/:id/pay", h.payOrder)
		r.GET("/orders", h.listOrders)
		r.GET("/orders/:id", h.getOrder)
	}
}

// settleAuction 手动触发指定竞拍的结算。
func (h *SettleHandler) settleAuction(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	result, err := h.settleService.SettleAuction(c.Request.Context(), id)
	writeResult(c, result, err)
}

// payOrder 模拟支付指定订单（pending_payment → paid），支持传入收货地址。
func (h *SettleHandler) payOrder(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.PayOrderInput
	_ = c.ShouldBindJSON(&input) // 可选参数，允许空 body
	order, err := h.settleService.PayOrder(c.Request.Context(), id, input)
	writeResult(c, order, err)
}

// listOrders 返回当前商家的订单列表（多租户过滤）。
func (h *SettleHandler) listOrders(c *gin.Context) {
	orders, err := h.settleService.ListSellerOrders(c.Request.Context(), mustGetUserID(c))
	if orders == nil {
		response.Success(c, []struct{}{})
		return
	}
	writeResult(c, orders, err)
}

// RegisterBuyerSettleRoutes 注册买家端订单路由（鉴权但非 Admin）。
// 注意：用 r.GET("/api/orders", ...) 而非 r.Group("/api").GET("/orders", ...)
// 避免 Gin v1.10 的 radix tree 在 /api/admin/orders 和 /api/orders 共存时的冲突 bug。
func RegisterBuyerSettleRoutes(r gin.IRouter, settleService *service.SettleService) {
	RegisterBuyerSettleRoutesWithPactGate(r, settleService, nil)
}

// RegisterBuyerSettleRoutesWithPactGate 注册带 Pact 前置校验的买家端订单路由。
func RegisterBuyerSettleRoutesWithPactGate(r gin.IRouter, settleService *service.SettleService, pactGate PactPaymentGate) {
	h := &SettleHandler{settleService: settleService}
	h.pactGate = pactGate
	r.GET("/api/orders", h.listBuyerOrders)
	r.POST("/api/orders/:id/pay", h.payBuyerOrder)
	r.GET("/api/orders/:id", h.getBuyerOrder)
}

// listBuyerOrders 返回买家自己的订单列表，可按 auctionId 过滤。
func (h *SettleHandler) listBuyerOrders(c *gin.Context) {
	auctionID, ok := optionalAuctionID(c)
	if !ok {
		return
	}
	orders, err := h.settleService.ListBuyerOrders(c.Request.Context(), mustGetUserID(c))
	if err != nil {
		writeResult(c, nil, err)
		return
	}
	if orders == nil {
		response.Success(c, []struct{}{})
		return
	}
	if auctionID > 0 {
		filtered := orders[:0]
		for _, order := range orders {
			if order.AuctionID == auctionID {
				filtered = append(filtered, order)
			}
		}
		response.Success(c, filtered)
		return
	}
	writeResult(c, orders, err)
}

// getBuyerOrder 查询买家自己的订单详情。
func (h *SettleHandler) getBuyerOrder(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	order, err := h.settleService.GetBuyerOrder(c.Request.Context(), id, mustGetUserID(c))
	writeResult(c, order, err)
}

// payBuyerOrder 模拟支付买家自己的订单。
func (h *SettleHandler) payBuyerOrder(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.PayOrderInput
	_ = c.ShouldBindJSON(&input)
	buyerID := mustGetUserID(c)
	if h.pactGate != nil {
		if err := h.pactGate.CheckBuyerPaymentAllowed(c.Request.Context(), id, buyerID); err != nil {
			writeResult(c, nil, err)
			return
		}
	}
	order, err := h.settleService.PayBuyerOrder(c.Request.Context(), id, buyerID, input)
	if h.pactGate != nil {
		h.pactGate.AuditPaymentResult(c.Request.Context(), id, buyerID, err == nil, err)
	}
	writeResult(c, order, err)
}

// getOrder 查询订单详情（买家/商家均可访问）。
func (h *SettleHandler) getOrder(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	order, err := h.settleService.GetOrderDetail(c.Request.Context(), id)
	writeResult(c, order, err)
}

func optionalAuctionID(c *gin.Context) (uint64, bool) {
	raw := c.Query("auctionId")
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		response.Error(c, http.StatusBadRequest, 400, "auctionId must be a positive integer")
		return 0, false
	}
	return value, true
}
