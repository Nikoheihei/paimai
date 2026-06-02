package redis

import (
	"context"
	"testing"
	"time"
)

// TestNewRedisClients_SkipIfOffline 验证 Redis 主从客户端可连接；本地容器未启动时跳过测试。
func TestNewRedisClients_SkipIfOffline(t *testing.T) {
	// 如果 Docker 容器尚未启动，标准单元测试应当优雅降级（跳过测试）。
	clients, err := NewRedisClients("localhost:6379", "localhost:6380")
	if err != nil {
		t.Skipf("Skipping Redis client connection test (containers might not be running): %v", err)
		return
	}
	defer clients.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err = clients.Master.Set(ctx, "test_key", "test_val", 5*time.Second).Err()
	if err != nil {
		t.Fatalf("Failed to write to master: %v", err)
	}

	val, err := clients.Master.Get(ctx, "test_key").Result()
	if err != nil {
		t.Fatalf("Failed to get from master: %v", err)
	}
	if val != "test_val" {
		t.Errorf("expected test_val, got %s", val)
	}
}
