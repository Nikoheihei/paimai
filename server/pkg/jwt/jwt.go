package jwt

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwtSecret 用于对 JWT 进行签名，从环境变量 JWT_SECRET 读取。
// 启动前必须设置此环境变量，否则 GenerateToken/ParseToken 均会 panic。
var jwtSecret = func() []byte {
	s := os.Getenv("JWT_SECRET")
	if s == "" {
		panic("JWT_SECRET environment variable is required")
	}
	return []byte(s)
}()

// Claims 定义了 JWT 载荷部分的内容。
type Claims struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"` // "buyer", "seller", "anchor"
	jwt.RegisteredClaims
}

// GenerateToken 根据用户基础信息生成带过期时间的 JWT 字符串。
// now 参数传入当前时间（可由调用方注入固定时间便于测试）。
func GenerateToken(userID uint64, username, role, nickname string, now time.Time) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Nickname: nickname,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(7 * 24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "paimai_auth",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// ParseToken 解析并验证 JWT 字符串，成功后返回业务 Claims。
// 只接受 HS256 算法（通过 WithValidMethods 严格限制）。
func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.NewParser(jwt.WithValidMethods([]string{"HS256"})).ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, errors.New("invalid token")
}
