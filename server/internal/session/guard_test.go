package session

import (
	"testing"
	"time"
)

func TestGuard_SingleSession(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewGuard(90 * time.Second)
	g.now = func() time.Time { return now }

	// user1 登录占用
	if _, ok := g.TryAcquire(1, "alice"); !ok {
		t.Fatal("user1 should acquire empty lock")
	}
	// user2 登录被拒，返回当前持有者
	if name, ok := g.TryAcquire(2, "bob"); ok || name != "alice" {
		t.Fatalf("user2 should be rejected, holder=alice; got name=%s ok=%v", name, ok)
	}
	// 同一用户可重复登录
	if _, ok := g.TryAcquire(1, "alice"); !ok {
		t.Fatal("same user should re-acquire")
	}
}

func TestGuard_ReleaseFrees(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewGuard(90 * time.Second)
	g.now = func() time.Time { return now }

	g.TryAcquire(1, "alice")
	g.Release(2) // 非持有者释放无效
	if _, ok := g.TryAcquire(2, "bob"); ok {
		t.Fatal("non-holder release must not free the lock")
	}
	g.Release(1) // 持有者释放
	if _, ok := g.TryAcquire(2, "bob"); !ok {
		t.Fatal("after holder release, others can acquire")
	}
}

func TestGuard_IdleExpiry(t *testing.T) {
	now := time.Unix(1000, 0)
	g := NewGuard(60 * time.Second)
	g.now = func() time.Time { return now }

	g.TryAcquire(1, "alice")
	// 31s 后续期，仍在线
	now = now.Add(31 * time.Second)
	g.Touch(1)
	if _, ok := g.TryAcquire(2, "bob"); ok {
		t.Fatal("holder still active within TTL, bob must be rejected")
	}
	// 持有者停止活动，超过 TTL → 自动释放
	now = now.Add(61 * time.Second)
	if _, ok := g.TryAcquire(2, "bob"); !ok {
		t.Fatal("after idle TTL, lock should be free for bob")
	}
}
