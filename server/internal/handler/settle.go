package handler

import (
	"github.com/gin-gonic/gin"

	"paimai/internal/service"
	"paimai/pkg/response"
)

// SettleHandler 负责结算与订单管理的 HTTP 协议适配。
type SettleHandler struct {
	settleService *service.SettleService
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

// getOrder 查询订单详情（买家/商家均可访问）。
func (h *SettleHandler) getOrder(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	order, err := h.settleService.GetOrder(c.Request.Context(), id)
	writeResult(c, order, err)
}
