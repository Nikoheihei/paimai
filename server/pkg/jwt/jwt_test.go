package jwt

import (
	"testing"
	"time"

	jwtlib "github.com/golang-jwt/jwt/v5"
)

// TestGenerateAndParse 验证 GenerateToken 生成的 token 能被 ParseToken 正确解析。
func TestGenerateAndParse(t *testing.T) {
	now := time.Now()
	tokenStr, err := GenerateToken(1, "alice", "buyer", "Alice", now)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}
	if tokenStr == "" {
		t.Fatal("GenerateToken() returned empty token")
	}

	claims, err := ParseToken(tokenStr)
	if err != nil {
		t.Fatalf("ParseToken() error = %v", err)
	}
	if claims.UserID != 1 {
		t.Errorf("UserID = %d, want 1", claims.UserID)
	}
	if claims.Username != "alice" {
		t.Errorf("Username = %s, want alice", claims.Username)
	}
	if claims.Role != "buyer" {
		t.Errorf("Role = %s, want buyer", claims.Role)
	}
}

// TestParseTokenExpired 验证过期 token 会被拒绝。
func TestParseTokenExpired(t *testing.T) {
	// 签发的 token 在 7 天前过期
	now := time.Now()
	past := now.Add(-8 * 24 * time.Hour)
	tokenStr, err := GenerateToken(2, "bob", "seller", "Bob", past)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	_, err = ParseToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for expired token, got nil")
	}
}

// TestParseTokenInvalid 验证无效 token 字符串会被拒绝。
func TestParseTokenInvalid(t *testing.T) {
	_, err := ParseToken("invalid.jwt.string")
	if err == nil {
		t.Fatal("expected error for invalid token, got nil")
	}
}

// TestParseTokenEmpty 验证空 token 会被拒绝。
func TestParseTokenEmpty(t *testing.T) {
	_, err := ParseToken("")
	if err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
}

// TestParseTokenInvalidAlg 验证非 HS256 算法的 token 会被拒绝。
func TestParseTokenInvalidAlg(t *testing.T) {
	now := time.Now()
	claims := Claims{
		UserID:   3,
		Username: "mallory",
		Role:     "buyer",
		RegisteredClaims: jwtlib.RegisteredClaims{
			ExpiresAt: jwtlib.NewNumericDate(now.Add(7 * 24 * time.Hour)),
			Issuer:    "paimai_auth",
		},
	}
	// 使用 HS512 签发（与预期 HS256 不同）
	token := jwtlib.NewWithClaims(jwtlib.SigningMethodHS512, claims)
	tokenStr, err := token.SignedString(jwtSecret)
	if err != nil {
		t.Fatalf("SignedString() error = %v", err)
	}

	_, err = ParseToken(tokenStr)
	if err == nil {
		t.Fatal("expected error for non-HS256 token, got nil")
	}
}
