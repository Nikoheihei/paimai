package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	jwtpkg "paimai/pkg/jwt"
)

// TestAuthRequiredNoToken 验证无 token 时返回 401。
func TestAuthRequiredNoToken(t *testing.T) {
	w := performRequest(nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestAuthRequiredInvalidToken 验证无效 token 返回 401。
func TestAuthRequiredInvalidToken(t *testing.T) {
	w := performRequest(strPtr("invalid.jwt.string"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestAuthRequiredValidToken 验证有效 token 返回 200 且 context 中包含 userId。
func TestAuthRequiredValidToken(t *testing.T) {
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	token, err := jwtpkg.GenerateToken(42, "testuser", "buyer", "Test", now)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := performRequest(&token)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if w.Body.String() != "userId=42" {
		t.Fatalf("expected userId=42, got %s", w.Body.String())
	}
}

// TestAuthRequiredExpiredToken 验证过期 token 返回 401。
func TestAuthRequiredExpiredToken(t *testing.T) {
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	past := now.Add(-8 * 24 * time.Hour)
	token, err := jwtpkg.GenerateToken(99, "olduser", "seller", "Old", past)
	if err != nil {
		t.Fatalf("GenerateToken() error = %v", err)
	}

	w := performRequest(&token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

// TestAdminRequiredNoRole 验证无 role 时返回 403。
func TestAdminRequiredNoRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AdminRequired())
	r.GET("/admin", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", w.Code)
	}
}

// TestAdminRequiredBuyerRole 验证 buyer 角色返回 403。
func TestAdminRequiredBuyerRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("role", "buyer")
		c.Next()
	}, AdminRequired())
	r.GET("/admin", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for buyer, got %d", w.Code)
	}
}

// TestAdminRequiredSellerRole 验证 seller 角色通过。
func TestAdminRequiredSellerRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("role", "seller")
		c.Next()
	}, AdminRequired())
	r.GET("/admin", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for seller, got %d", w.Code)
	}
}

// TestAdminRequiredAnchorRole 验证 anchor 角色通过。
func TestAdminRequiredAnchorRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("role", "anchor")
		c.Next()
	}, AdminRequired())
	r.GET("/admin", func(c *gin.Context) {
		c.String(200, "ok")
	})

	req := httptest.NewRequest("GET", "/admin", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for anchor, got %d", w.Code)
	}
}

// performRequest 创建 Gin 引擎并发送带可选 token 的请求。
func performRequest(token *string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuthRequired())
	r.GET("/protected", func(c *gin.Context) {
		if c.IsAborted() {
			return
		}
		uid, _ := c.Get("userId")
		c.String(200, "userId=%d", uid.(uint64))
	})

	req := httptest.NewRequest("GET", "/protected", nil)
	if token != nil {
		req.Header.Set("Authorization", "Bearer "+*token)
	}

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// strPtr 返回字符串指针。
func strPtr(s string) *string {
	return &s
}
