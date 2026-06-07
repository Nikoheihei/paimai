package handler

import (
	"github.com/gin-gonic/gin"

	"net/http"
	"paimai/internal/service"
	"paimai/pkg/response"
	"sync"
)

// AuthHandler 负责用户认证的 HTTP 协议适配。
type AuthHandler struct {
	authService *service.AuthService
}

// RegisterAuthRoutes 注册认证相关路由（无需鉴权）。
func RegisterAuthRoutes(r *gin.Engine, authService *service.AuthService) {
	h := &AuthHandler{authService: authService}
	auth := r.Group("/api/auth")
	{
		auth.POST("/register", h.register)
		auth.POST("/login", h.login)
	}
}

// RegisterAuthMeRoute 注册需要鉴权的用户信息路由（需挂在鉴权中间件之后）。
func RegisterAuthMeRoute(r *gin.Engine, authService *service.AuthService) {
	h := &AuthHandler{authService: authService}
	r.GET("/api/auth/me", h.me)
}

// RegisterAddressRoutes 注册收货地址路由（挂在鉴权中间件之后）。
func RegisterAddressRoutes(r gin.IRouter) {
	r.GET("/addresses", listAddresses)
	r.POST("/addresses", createAddress)
	r.PUT("/addresses/:id", updateAddress)
	r.DELETE("/addresses/:id", deleteAddress)
}

type Address struct {
	ID        uint64 `json:"id"`
	UserID    uint64 `json:"userId"`
	Name      string `json:"name"`
	Phone     string `json:"phone"`
	Province  string `json:"province"`
	City      string `json:"city"`
	District  string `json:"district"`
	Detail    string `json:"detail"`
	IsDefault bool   `json:"isDefault"`
}

var addressStore sync.Map // userID -> []Address
var addressIDCounter uint64
var addressMu sync.Mutex

func listAddresses(c *gin.Context) {
	userID := mustGetUserID(c)
	if userID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	result := make([]Address, 0)
	if raw, ok := addressStore.Load(userID); ok {
		result = raw.([]Address)
	}
	response.Success(c, result)
}

func createAddress(c *gin.Context) {
	userID := mustGetUserID(c)
	if userID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	var input Address
	if !bindJSON(c, &input) {
		return
	}
	addressMu.Lock()
	addressIDCounter++
	input.ID = addressIDCounter
	addressMu.Unlock()
	input.UserID = userID

	var list []Address
	if raw, ok := addressStore.Load(userID); ok {
		list = raw.([]Address)
	}
	if input.IsDefault {
		for i := range list {
			list[i].IsDefault = false
		}
	}
	list = append(list, input)
	addressStore.Store(userID, list)
	response.Success(c, input)
}

func updateAddress(c *gin.Context) {
	userID := mustGetUserID(c)
	if userID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input Address
	if !bindJSON(c, &input) {
		return
	}
	raw, ok := addressStore.Load(userID)
	if !ok {
		response.Error(c, http.StatusNotFound, 404, "address not found")
		return
	}
	list := raw.([]Address)
	found := false
	for i, a := range list {
		if a.ID == id {
			list[i].Name = input.Name
			list[i].Phone = input.Phone
			list[i].Province = input.Province
			list[i].City = input.City
			list[i].District = input.District
			list[i].Detail = input.Detail
			if input.IsDefault {
				for j := range list {
					list[j].IsDefault = false
				}
				list[i].IsDefault = true
			}
			found = true
			response.Success(c, list[i])
			break
		}
	}
	if !found {
		response.Error(c, http.StatusNotFound, 404, "address not found")
		return
	}
	addressStore.Store(userID, list)
}

func deleteAddress(c *gin.Context) {
	userID := mustGetUserID(c)
	if userID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	raw, ok := addressStore.Load(userID)
	if !ok {
		response.Success(c, nil)
		return
	}
	list := raw.([]Address)
	newList := make([]Address, 0, len(list))
	for _, a := range list {
		if a.ID != id {
			newList = append(newList, a)
		}
	}
	addressStore.Store(userID, newList)
	response.Success(c, nil)
}

// register 处理用户注册请求。
func (h *AuthHandler) register(c *gin.Context) {
	var input service.RegisterInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.authService.Register(c.Request.Context(), input)
	writeResult(c, result, err)
}

// login 处理用户登录请求。
func (h *AuthHandler) login(c *gin.Context) {
	var input service.LoginInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.authService.Login(c.Request.Context(), input)
	writeResult(c, result, err)
}

// me 返回当前登录用户的信息。
func (h *AuthHandler) me(c *gin.Context) {
	userID, exists := c.Get("userId")
	if !exists {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	uid, ok := userID.(uint64)
	if !ok {
		response.Error(c, http.StatusUnauthorized, 401, "invalid user identity")
		return
	}
	result, err := h.authService.Me(c.Request.Context(), uid)
	writeResult(c, result, err)
}
