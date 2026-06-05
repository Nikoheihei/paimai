package handler

import (
	"github.com/gin-gonic/gin"

	"paimai/internal/service"
	"paimai/internal/websocket"
)

// RoomHandler 负责直播间管理的 HTTP 协议适配。
type RoomHandler struct {
	roomService *service.RoomService
	hub         *websocket.Hub
}

// RegisterRoomRoutes 注册直播间管理路由（需鉴权）。
func RegisterRoomRoutes(r gin.IRouter, roomService *service.RoomService, hub *websocket.Hub) {
	h := &RoomHandler{roomService: roomService, hub: hub}
	{
		r.POST("/rooms", h.createRoom)
		r.GET("/rooms", h.listRooms)
		r.GET("/rooms/:id", h.getRoom)
		r.PATCH("/rooms/:id", h.updateRoom)
		r.POST("/rooms/:id/live", h.goLive)
		r.POST("/rooms/:id/close", h.closeRoom)
		r.GET("/rooms/:id/stats", h.getRoomStats)
	}
}

func (h *RoomHandler) createRoom(c *gin.Context) {
	var input service.CreateRoomInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.roomService.CreateRoom(c.Request.Context(), mustGetUserID(c), input)
	writeResult(c, result, err)
}

func (h *RoomHandler) listRooms(c *gin.Context) {
	rooms, err := h.roomService.ListRooms(c.Request.Context(), mustGetUserID(c))
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

func (h *RoomHandler) getRoomStats(c *gin.Context) {
	id, ok := pathID(c)
	if !ok {
		return
	}
	stats := h.hub.GetRoomStats(id)
	writeResult(c, stats, nil)
}
