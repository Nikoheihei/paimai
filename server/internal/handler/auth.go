package handler

import (
	"github.com/gin-gonic/gin"

	"paimai/internal/service"
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
	userID, _ := c.Get("userId")
	result, err := h.authService.Me(c.Request.Context(), userID.(uint64))
	writeResult(c, result, err)
}
