package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"paimai/internal/repository"
	"paimai/internal/service"
	"paimai/pkg/response"
)

// AdminHandler 负责后台管理端 HTTP 协议适配。
//
// handler 层只处理参数解析、HTTP 状态码和响应格式；
// 业务规则全部交给 service.AdminService，避免控制器越来越厚。
type AdminHandler struct {
	service *service.AdminService
}

// RegisterAdminRoutes 注册后台管理端商品和竞拍路由。
func RegisterAdminRoutes(r gin.IRouter, adminService *service.AdminService) {
	h := &AdminHandler{service: adminService}
	admin := r
	{
		admin.POST("/products", h.createProduct)
		admin.GET("/products", h.listProducts)
		admin.GET("/products/:id", h.getProduct)
		admin.PATCH("/products/:id", h.updateProduct)
		admin.DELETE("/products/:id", h.deleteProduct)

		admin.POST("/auctions", h.createAuction)
		admin.GET("/auctions", h.listAuctions)
		admin.PATCH("/auctions/:id", h.updateAuction)
		admin.POST("/auctions/:id/publish", h.publishAuction)
		admin.POST("/auctions/:id/start", h.startAuction)
		admin.POST("/auctions/:id/cancel", h.cancelAuction)

		admin.GET("/auctions/:id/bids", h.listAuctionBids)
	}
}

// createProduct 解析创建商品请求，并调用服务层完成商品创建。
func (h *AdminHandler) createProduct(c *gin.Context) {
	var input service.ProductInput
	if !bindJSON(c, &input) {
		return
	}
	product, err := h.service.CreateProduct(c.Request.Context(), mustGetUserID(c), input)
	writeResult(c, product, err)
}

// listProducts 解析商品列表筛选参数，并返回后台商品列表。
func (h *AdminHandler) listProducts(c *gin.Context) {
	sellerID := mustGetUserID(c)
	products, err := h.service.ListProducts(c.Request.Context(), &sellerID)
	writeResult(c, products, err)
}

// createAuction 解析创建竞拍请求，并调用服务层保存竞拍草稿。
func (h *AdminHandler) createAuction(c *gin.Context) {
	var input service.AuctionInput
	if !bindJSON(c, &input) {
		return
	}
	auction, err := h.service.CreateAuction(c.Request.Context(), input)
	writeResult(c, auction, err)
}

// listAuctions 解析竞拍列表筛选参数，并返回后台竞拍列表。
func (h *AdminHandler) listAuctions(c *gin.Context) {
	roomID, ok := optionalUint64Query(c, "roomId")
	if !ok {
		return
	}
	filter := repository.AuctionFilter{
		RoomID: roomID,
		Status: c.Query("status"),
	}
	auctions, err := h.service.ListAuctions(c.Request.Context(), filter)
	writeResult(c, auctions, err)
}

// updateAuction 解析竞拍规则修改请求，并限制只修改未开始竞拍。
func (h *AdminHandler) updateAuction(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.AuctionPatchInput
	if !bindJSON(c, &input) {
		return
	}
	auction, err := h.service.UpdateAuction(c.Request.Context(), id, input)
	writeResult(c, auction, err)
}

// publishAuction 将竞拍从草稿状态发布到待开始状态。
func (h *AdminHandler) publishAuction(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	auction, err := h.service.PublishAuction(c.Request.Context(), id)
	writeResult(c, auction, err)
}

// startAuction 将已发布竞拍切换到运行中，并初始化热数据。
func (h *AdminHandler) startAuction(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.StartAuctionInput
	if c.Request.ContentLength > 0 && !bindJSON(c, &input) {
		return
	}
	auction, err := h.service.StartAuction(c.Request.Context(), id, input)
	writeResult(c, auction, err)
}

// getProduct 查询商品详情。
func (h *AdminHandler) getProduct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	product, err := h.service.GetProduct(c.Request.Context(), id)
	writeResult(c, product, err)
}

// deleteProduct 删除商品（仅无活跃竞拍的商品可删）。
func (h *AdminHandler) deleteProduct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	err := h.service.DeleteProduct(c.Request.Context(), id)
	writeResult(c, nil, err)
}

// updateProduct 编辑商品信息。
func (h *AdminHandler) updateProduct(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.UpdateProductInput
	if !bindJSON(c, &input) {
		return
	}
	product, err := h.service.UpdateProduct(c.Request.Context(), id, input)
	writeResult(c, product, err)
}

// getOrder 查询订单详情。
func (h *AdminHandler) getOrder(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	order, err := h.service.GetOrder(c.Request.Context(), id)
	writeResult(c, order, err)
}

// listAuctionBids 查询指定竞拍的出价历史。
func (h *AdminHandler) listAuctionBids(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	bids, err := h.service.ListAuctionBids(c.Request.Context(), id)
	writeResult(c, bids, err)
}

// cancelAuction 处理主播或商家的异常取消竞拍请求。
func (h *AdminHandler) cancelAuction(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.CancelAuctionInput
	if c.Request.ContentLength > 0 && !bindJSON(c, &input) {
		return
	}
	auction, err := h.service.CancelAuction(c.Request.Context(), id, input)
	writeResult(c, auction, err)
}

// mustGetUserID 从 gin.Context 安全获取 userId，不存在时返回 0。
func mustGetUserID(c *gin.Context) uint64 {
	v, exists := c.Get("userId")
	if !exists {
		return 0
	}
	uid, ok := v.(uint64)
	if !ok {
		return 0
	}
	return uid
}

// mustGetRole 从 gin.Context 安全获取角色。
func mustGetRole(c *gin.Context) string {
	v, exists := c.Get("role")
	if !exists {
		return ""
	}
	role, ok := v.(string)
	if !ok {
		return ""
	}
	return role
}

// bindJSON 统一绑定 JSON 请求体，并在格式错误时写入 400 响应。
func bindJSON(c *gin.Context, target interface{}) bool {
	if err := c.ShouldBindJSON(target); err != nil {
		response.Error(c, http.StatusBadRequest, 400, "invalid request body")
		return false
	}
	return true
}

// optionalUint64Query 解析可选的正整数查询参数。
func optionalUint64Query(c *gin.Context, key string) (*uint64, bool) {
	raw := c.Query(key)
	if raw == "" {
		return nil, true
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		response.Error(c, http.StatusBadRequest, 400, key+" must be a positive integer")
		return nil, false
	}
	return &value, true
}

// pathID 从路由参数中解析正整数 ID。
func pathID(c *gin.Context) (uint64, bool) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil || id == 0 {
		response.Error(c, http.StatusBadRequest, 400, "id must be a positive integer")
		return 0, false
	}
	return id, true
}

// writeResult 将服务层结果统一转换为 API 响应。
func writeResult(c *gin.Context, data interface{}, err error) {
	if err == nil {
		response.Success(c, data)
		return
	}

	// service 层返回稳定的业务错误类型，handler 在这里统一映射为 HTTP 语义。
	// 这样新增接口时不需要重复散落一堆错误判断，也方便前端形成一致的错误处理。
	switch {
	case errors.Is(err, service.ErrInvalidInput):
		response.Error(c, http.StatusBadRequest, 400, err.Error())
	case errors.Is(err, service.ErrNotFound):
		response.Error(c, http.StatusNotFound, 404, "resource not found")
	case errors.Is(err, service.ErrBidEngineUnavailable):
		response.Error(c, http.StatusServiceUnavailable, 503, "bid engine unavailable")
	case errors.Is(err, service.ErrInvalidTransition):
		response.Error(c, http.StatusConflict, 409, err.Error())
	case errors.Is(err, service.ErrUnauthorized):
		response.Error(c, http.StatusUnauthorized, 401, err.Error())
	case errors.Is(err, service.ErrAuctionNotEditable):
		response.Error(c, http.StatusConflict, 409, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, 500, "internal server error")
	}
}
