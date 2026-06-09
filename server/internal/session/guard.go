// Package session 提供「全站同一时刻只允许一个用户登录」的会话锁。
//
// 语义：
//   - 有人登录且会话仍活跃时，其他账号登录被拒绝。
//   - 同一账号可重复登录（刷新会话）。
//   - 持有者的每次鉴权请求都会续期；持有者关闭页面后超过 idleTTL 无活动则自动释放，
//     其他人即可登录（避免关页面后永久锁死）。
//   - 显式登出立即释放。
//
// 内存实现，单实例足够；多实例部署需换成 Redis。
package session

import (
	"sync"
	"time"
)

type holder struct {
	userID   uint64
	username string
	lastSeen time.Time
}

// Guard 是全站单会话锁。
type Guard struct {
	mu      sync.Mutex
	cur     *holder
	idleTTL time.Duration
	now     func() time.Time
}

// NewGuard 创建会话锁，idleTTL <= 0 时取默认 90s。
func NewGuard(idleTTL time.Duration) *Guard {
	if idleTTL <= 0 {
		idleTTL = 90 * time.Second
	}
	return &Guard{idleTTL: idleTTL, now: time.Now}
}

// Default 是全局默认会话锁。
var Default = NewGuard(90 * time.Second)

// Configure 设置默认锁的空闲超时。
func Configure(idleTTL time.Duration) {
	if idleTTL <= 0 {
		idleTTL = 90 * time.Second
	}
	Default.mu.Lock()
	Default.idleTTL = idleTTL
	Default.mu.Unlock()
}

// freeLocked 判断当前是否空闲（无持有者或持有者已空闲超时）。调用方须持锁。
func (g *Guard) freeLocked(t time.Time) bool {
	return g.cur == nil || t.Sub(g.cur.lastSeen) > g.idleTTL
}

// TryAcquire 尝试占用会话。
// 返回 (当前持有者用户名, 是否成功)。失败时第一个返回值是正在占用的账号名。
func (g *Guard) TryAcquire(userID uint64, username string) (string, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	t := g.now()
	if !g.freeLocked(t) && g.cur.userID != userID {
		return g.cur.username, false
	}
	g.cur = &holder{userID: userID, username: username, lastSeen: t}
	return username, true
}

// Touch 续期：仅当 userID 是当前持有者时刷新活跃时间。
func (g *Guard) Touch(userID uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cur != nil && g.cur.userID == userID {
		g.cur.lastSeen = g.now()
	}
}

// Release 释放：仅当 userID 是当前持有者时清空。
func (g *Guard) Release(userID uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.cur != nil && g.cur.userID == userID {
		g.cur = nil
	}
}

// Current 返回当前活跃持有者的 (userID, username)；空闲返回 (0, "")。
func (g *Guard) Current() (uint64, string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.freeLocked(g.now()) {
		return 0, ""
	}
	return g.cur.userID, g.cur.username
}
