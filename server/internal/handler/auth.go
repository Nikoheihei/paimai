package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
	"paimai/internal/service"
	"paimai/internal/session"
	"paimai/pkg/response"
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
	r.POST("/api/auth/logout", h.logout)
}

// logout 释放全站单会话锁（仅持有者本人有效）。
func (h *AuthHandler) logout(c *gin.Context) {
	if uid, ok := c.Get("userId"); ok {
		if id, ok := uid.(uint64); ok {
			session.Default.Release(id)
		}
	}
	response.Success(c, gin.H{"loggedOut": true})
}

// RegisterAddressRoutes 注册收货地址路由（挂在鉴权中间件之后）。
func RegisterAddressRoutes(r gin.IRouter, store repository.AddressStore) {
	h := &AddressHandler{store: store}
	r.GET("/addresses", h.listAddresses)
	r.POST("/addresses", h.createAddress)
	r.PUT("/addresses/:id", h.updateAddress)
	r.DELETE("/addresses/:id", h.deleteAddress)
}

type AddressHandler struct {
	store repository.AddressStore
}

func (h *AddressHandler) listAddresses(c *gin.Context) {
	userID := mustGetUserID(c)
	if userID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	result, err := h.store.ListAddresses(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, err.Error())
		return
	}
	response.Success(c, result)
}

func (h *AddressHandler) createAddress(c *gin.Context) {
	userID := mustGetUserID(c)
	if userID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	var input model.Address
	if !bindJSON(c, &input) {
		return
	}
	normalizeAddress(&input)
	input.UserID = userID
	if err := validateAddress(input); err != nil {
		response.Error(c, http.StatusBadRequest, 400, err.Error())
		return
	}

	if err := h.store.CreateAddress(c.Request.Context(), &input); err != nil {
		response.Error(c, http.StatusInternalServerError, 500, err.Error())
		return
	}
	response.Success(c, input)
}

func (h *AddressHandler) updateAddress(c *gin.Context) {
	userID := mustGetUserID(c)
	if userID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	var input model.Address
	if !bindJSON(c, &input) {
		return
	}
	normalizeAddress(&input)
	if err := validateAddress(input); err != nil {
		response.Error(c, http.StatusBadRequest, 400, err.Error())
		return
	}

	updated, err := h.store.UpdateAddress(c.Request.Context(), userID, id, input)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, http.StatusNotFound, 404, "address not found")
		return
	}
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500, err.Error())
		return
	}
	response.Success(c, updated)
}

func (h *AddressHandler) deleteAddress(c *gin.Context) {
	userID := mustGetUserID(c)
	if userID == 0 {
		response.Error(c, http.StatusUnauthorized, 401, "unauthorized")
		return
	}
	id, ok := pathID(c)
	if !ok {
		return
	}
	if err := h.store.DeleteAddress(c.Request.Context(), userID, id); err != nil {
		response.Error(c, http.StatusInternalServerError, 500, err.Error())
		return
	}
	response.Success(c, nil)
}

func normalizeAddress(address *model.Address) {
	address.ID = 0
	address.UserID = 0
	address.Name = strings.TrimSpace(address.Name)
	address.Phone = strings.TrimSpace(address.Phone)
	address.Province = strings.TrimSpace(address.Province)
	address.City = strings.TrimSpace(address.City)
	address.District = strings.TrimSpace(address.District)
	address.Detail = strings.TrimSpace(address.Detail)
}

func validateAddress(address model.Address) error {
	if address.Name == "" {
		return errors.New("收货人不能为空")
	}
	if address.Phone == "" {
		return errors.New("手机号不能为空")
	}
	if address.Detail == "" {
		return errors.New("详细地址不能为空")
	}
	return nil
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
// 全站单会话：已有其他账号在线时拒绝登录；同一账号可重复登录刷新会话。
func (h *AuthHandler) login(c *gin.Context) {
	var input service.LoginInput
	if !bindJSON(c, &input) {
		return
	}
	result, err := h.authService.Login(c.Request.Context(), input)
	if err != nil {
		writeResult(c, result, err)
		return
	}
	if holderName, ok := session.Default.TryAcquire(result.UserID, result.Username); !ok {
		response.Error(c, http.StatusConflict, 409,
			"当前已有用户【"+holderName+"】在线，全站同一时刻只允许一个用户登录，请等待其退出后再登录")
		return
	}
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
