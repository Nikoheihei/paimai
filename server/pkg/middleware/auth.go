package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	jwtpkg "paimai/pkg/jwt"
	"paimai/pkg/response"
)

// AuthRequired 验证请求头中的 JWT token，并将 userId / username 注入 gin.Context。
//
// token 来源（按优先级）：
// 1. Authorization: Bearer <token> 头（标准 REST API）
// 2. ?token=<token> 查询参数（WebSocket 升级，无法自定义请求头）
//
// 开发阶段兼容模式：没有 token 时不拒绝，仅跳过 context 注入。
// 生产环境应将下方 fallback 判断改为直接 Abort。
func AuthRequired() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从 Authorization 头提取
		tokenString := extractBearerToken(c.GetHeader("Authorization"))

		// 如果没有 Authorization 头，尝试从查询参数提取（WebSocket 场景）
		if tokenString == "" {
			tokenString = c.Query("token")
		}

		if tokenString == "" {
			// 开发阶段：允许无 token 请求通过
			// TODO: 生产环境应改为 response.Error + Abort
			c.Next()
			return
		}

		claims, err := jwtpkg.ParseToken(tokenString)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, 401, "invalid or expired token")
			c.Abort()
			return
		}

		c.Set("userId", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("nickname", claims.Nickname)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// extractBearerToken 从 Authorization 头中提取 Bearer token 字符串。
func extractBearerToken(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}
