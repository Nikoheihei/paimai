package jwt

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// jwtSecret 用于对 JWT 进行签名，生产环境应从配置中读取。
var jwtSecret = []byte("paimai_secret_key_123456")

// Claims 定义了 JWT 载荷部分的内容。
type Claims struct {
	UserID   uint64 `json:"userId"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"` // "buyer" (买家), "seller" (卖家), "anchor" (主播)
	jwt.RegisteredClaims
}

// GenerateToken 根据用户基础信息生成带过期时间的 JWT 字符串。
func GenerateToken(userID uint64, role string, nickname string) (string, error) {
	now := time.Now()
	claims := Claims{
		UserID:   userID,
		Nickname: nickname,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)), // Token 有效期 24 小时
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "paimai_auth",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(jwtSecret)
}

// ParseToken 解析并验证 JWT 字符串，成功后返回业务 Claims。
func ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
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
