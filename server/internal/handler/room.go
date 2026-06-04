package handler

import (
	"github.com/gin-gonic/gin"

	"paimai/internal/service"
)

// RoomHandler 负责直播间管理的 HTTP 协议适配。
type RoomHandler struct {
	roomService *service.RoomService
}

// RegisterRoomRoutes 注册直播间管理路由（需鉴权）。
func RegisterRoomRoutes(r *gin.Engine, roomService *service.RoomService) {
	h := &RoomHandler{roomService: roomService}
	admin := r.Group("/api/admin")
	{
		admin.POST("/rooms", h.createRoom)
		admin.GET("/rooms", h.listRooms)
		admin.GET("/rooms/:id", h.getRoom)
		admin.PATCH("/rooms/:id", h.updateRoom)
		admin.POST("/rooms/:id/live", h.goLive)
		admin.POST("/rooms/:id/close", h.closeRoom)
	}
}

func (h *RoomHandler) createRoom(c *gin.Context) {
	var input service.CreateRoomInput
	if !bindJSON(c, &input) {
		return
	}
	sellerID, _ := c.Get("userId")
	result, err := h.roomService.CreateRoom(c.Request.Context(), sellerID.(uint64), input)
	writeResult(c, result, err)
}

func (h *RoomHandler) listRooms(c *gin.Context) {
	sellerID, _ := c.Get("userId")
	rooms, err := h.roomService.ListRooms(c.Request.Context(), sellerID.(uint64))
	writeResult(c, rooms, err)
}

func (h *RoomHandler) getRoom(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	room, err := h.roomService.GetRoom(c.Request.Context(), id)
	writeResult(c, room, err)
}

func (h *RoomHandler) updateRoom(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input service.CreateRoomInput
	if !bindJSON(c, &input) {
		return
	}
	room, err := h.roomService.UpdateRoom(c.Request.Context(), id, input)
	writeResult(c, room, err)
}

func (h *RoomHandler) goLive(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	room, err := h.roomService.GoLive(c.Request.Context(), id)
	writeResult(c, room, err)
}

func (h *RoomHandler) closeRoom(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	result, err := h.roomService.CloseRoom(c.Request.Context(), id)
	writeResult(c, result, err)
}
