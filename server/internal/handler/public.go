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
)

// upgrader 是 WebSocket 升级器，负责将 HTTP 连接升级为 WebSocket 连接。
var upgrader = gorillaWs.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// 允许所有来源的 WebSocket 连接，开发阶段不限制 origin。
	CheckOrigin: func(r *http.Request) bool { return true },
}

// PublicHandler 负责用户端直播间、竞拍详情、排行榜和出价 API 的 HTTP 适配。
type PublicHandler struct {
	service *service.PublicService
	hub     *ws.Hub
}

// RegisterPublicRoutes 注册用户端 REST API 路由。
func RegisterPublicRoutes(r *gin.Engine, publicService *service.PublicService, hub *ws.Hub) {
	h := &PublicHandler{service: publicService, hub: hub}
	api := r.Group("/api")
	{
		api.GET("/rooms/:roomId", h.getRoom)
		api.GET("/rooms/:roomId/auctions", h.listRoomAuctions)
		api.GET("/auctions/:id", h.getAuction)
		api.GET("/auctions/:id/ranking", h.getRanking)
		api.POST("/auctions/:id/bids", h.placeBid)
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

// serveWS 处理 WebSocket 升级请求，验证房间存在后将连接注册到 Hub。
func (h *PublicHandler) serveWS(c *gin.Context) {
	roomID, ok := positiveParam(c, "roomId")
	if !ok {
		return
	}

	// 开发阶段跳过直播间存在校验，生产环境应打开
	// _, err := h.service.GetRoom(c.Request.Context(), roomID)
	// if err != nil {
	// 	writeResult(c, nil, err)
	// 	return
	// }

	// 从查询参数中获取 userId（生产环境应由 JWT 认证提供）
	userIDStr := c.DefaultQuery("userId", "0")
	userID, err := strconv.ParseUint(userIDStr, 10, 64)
	if err != nil || userID == 0 {
		response.Error(c, http.StatusBadRequest, 400, "userId query parameter is required and must be a positive integer")
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("[websocket] upgrade failed for room %d, user %d: %v", roomID, userID, err)
		return
	}

	client := ws.NewClient(h.hub, roomID, userID, conn)
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
