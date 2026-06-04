package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	gorillaWs "github.com/gorilla/websocket"

	"paimai/internal/service"
	ws "paimai/internal/websocket"
	"paimai/pkg/response"
	"strings"
)

// UpgraderConfig 保存 WebSocket Upgrader 的配置选项。
type UpgraderConfig struct {
	// AllowAllOrigins 为 true 时允许所有 Origin（开发阶段）
	AllowAllOrigins bool
}

// PublicHandler 负责用户端直播间、竞拍详情、排行榜和出价 API 的 HTTP 适配。
type PublicHandler struct {
	service *service.PublicService
	cfg     *UpgraderConfig
	hub     *ws.Hub
}

// RegisterPublicRoutes 注册用户端 REST API 路由。
func RegisterPublicRoutes(r *gin.Engine, publicService *service.PublicService, hub *ws.Hub, cfg *UpgraderConfig) {
	h := &PublicHandler{service: publicService, hub: hub, cfg: cfg}
	api := r.Group("/api")
	{
		api.GET("/rooms", h.listLiveRooms)
		api.GET("/rooms/:roomId", h.getRoom)
		api.GET("/rooms/:roomId/auctions", h.listRoomAuctions)
		api.GET("/auctions/:id", h.getAuction)
		api.GET("/auctions/:id/ranking", h.getRanking)
		api.POST("/auctions/:id/bids", h.placeBid)

		api.GET("/orders", h.listBuyerOrders)
		api.GET("/orders/:id", h.getBuyerOrder)
		api.POST("/orders/:id/pay", h.payBuyerOrder)

		api.GET("/rooms/:roomId/ws", h.serveWS)
	}
}

// getRoom 查询直播间详情。
func (h *PublicHandler) getRoom(c *gin.Context) {
	roomID, ok := positiveParam(c, "roomId")
	if !ok {
		return
	}
	room, err := h.service.GetRoom(c.Request.Context(), roomID)
	writeResult(c, room, err)
}

// listLiveRooms 返回所有直播中的直播间列表（平台首页）。
func (h *PublicHandler) listLiveRooms(c *gin.Context) {
	rooms, err := h.service.ListLiveRooms(c.Request.Context())
	if rooms == nil {
		response.Success(c, []struct{}{})
		return
	}
	writeResult(c, rooms, err)
}

// listRoomAuctions 查询直播间竞拍列表，可通过 status 查询参数过滤。
func (h *PublicHandler) listRoomAuctions(c *gin.Context) {
	roomID, ok := positiveParam(c, "roomId")
	if !ok {
		return
	}
	auctions, err := h.service.ListRoomAuctions(c.Request.Context(), roomID, c.Query("status"))
	writeResult(c, auctions, err)
}

// getAuction 查询竞拍详情。
func (h *PublicHandler) getAuction(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	auction, err := h.service.GetAuction(c.Request.Context(), id)
	writeResult(c, auction, err)
}

// getRanking 查询竞拍排行榜。
func (h *PublicHandler) getRanking(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	limit, ok := optionalIntQuery(c, "limit")
	if !ok {
		return
	}
	ranking, err := h.service.GetRanking(c.Request.Context(), id, limit)
	writeResult(c, ranking, err)
}

// placeBid 处理用户出价请求。
func (h *PublicHandler) placeBid(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.BidInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.service.PlaceBid(c.Request.Context(), id, input)
	if reject, ok := err.(*service.BidRejectError); ok {
		response.Error(c, http.StatusConflict, 409, reject.Message)
		return
	}
	writeResult(c, result, err)
}

// listBuyerOrders 返回当前用户的订单列表。
func (h *PublicHandler) listBuyerOrders(c *gin.Context) {
	userID, _ := c.Get("userId")
	orders, err := h.service.ListBuyerOrders(c.Request.Context(), userID.(uint64))
	if orders == nil {
		response.Success(c, []struct{}{})
		return
	}
	writeResult(c, orders, err)
}

// getBuyerOrder 返回当前用户的订单详情。
func (h *PublicHandler) getBuyerOrder(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	order, err := h.service.GetBuyerOrder(c.Request.Context(), id)
	writeResult(c, order, err)
}

// payBuyerOrder 模拟支付当前用户的订单。
func (h *PublicHandler) payBuyerOrder(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	order, err := h.service.PayBuyerOrder(c.Request.Context(), id)
	writeResult(c, order, err)
}

// serveWS 处理 WebSocket 升级请求，验证房间存在后将连接注册到 Hub。
func (h *PublicHandler) serveWS(c *gin.Context) {
	roomID, ok := positiveParam(c, "roomId")
	if !ok {
		return
	}

	// 校验直播间存在性
	_, err := h.service.GetRoom(c.Request.Context(), roomID)
	if err != nil {
		writeResult(c, nil, err)
		return
	}

	// 优先从 JWT context 获取 userId，兼容开发阶段仍然传 query 参数
	userID, _ := c.Get("userId")
	var uid uint64
	if userID != nil {
		uid = userID.(uint64)
	}
	if uid == 0 {
		userIDStr := c.DefaultQuery("userId", "0")
		uid, _ = strconv.ParseUint(userIDStr, 10, 64)
		if uid == 0 {
			response.Error(c, http.StatusBadRequest, 400, "userId is required")
			return
		}
	}
	// 创建 upgrader（使用配置控制 CheckOrigin 策略）
	upgrader := gorillaWs.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			if h.cfg.AllowAllOrigins {
				return true
			}
			origin := r.Header.Get("Origin")
			return origin == "" || strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1")
		},
	}
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[websocket] upgrade failed for room %d, user %d: %v", roomID, uid, err)
		return
	}

	client := ws.NewClient(h.hub, roomID, uid, conn)
	h.hub.Register(client)

	go client.WritePump()
	go client.ReadPump()
}

// positiveParam 从指定路由参数解析正整数。
func positiveParam(c *gin.Context, key string) (uint64, bool) {
	value, err := strconv.ParseUint(c.Param(key), 10, 64)
	if err != nil || value == 0 {
		response.Error(c, http.StatusBadRequest, 400, key+" must be a positive integer")
		return 0, false
	}
	return value, true
}

// optionalIntQuery 从查询参数中解析可选整数。
func optionalIntQuery(c *gin.Context, key string) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		response.Error(c, http.StatusBadRequest, 400, key+" must be a non-negative integer")
		return 0, false
	}
	return value, true
}
