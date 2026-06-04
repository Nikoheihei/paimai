package handler

import (
	"testing"

	"github.com/gin-gonic/gin"
)

// TestMustGetUserID 验证 mustGetUserID 在各种情况下的安全行为。
func TestMustGetUserID(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(c *gin.Context)
		want   uint64
	}{
		{
			name: "userId 存在且为 uint64",
			setup: func(c *gin.Context) {
				c.Set("userId", uint64(42))
			},
			want: 42,
		},
		{
			name: "userId 未设置",
			setup: func(c *gin.Context) {
				// 不设置 userId
			},
			want: 0,
		},
		{
			name: "userId 类型错误（非 uint64）",
			setup: func(c *gin.Context) {
				c.Set("userId", "not-a-uint64")
			},
			want: 0,
		},
		{
			name: "userId 为 uint64(0)",
			setup: func(c *gin.Context) {
				c.Set("userId", uint64(0))
			},
			want: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(nil)
			tt.setup(c)
			got := mustGetUserID(c)
			if got != tt.want {
				t.Errorf("mustGetUserID() = %d, want %d", got, tt.want)
			}
		})
	}
}
