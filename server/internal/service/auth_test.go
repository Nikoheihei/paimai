package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"paimai/internal/model"
)

// authStoreStub 内存版 AuthStore，用于用户认证单元测试。
type authStoreStub struct {
	users    map[uint64]*model.User
	userAuth map[string]*model.UserAuth // key: username
	nextID   uint64
}

func newAuthStoreStub() *authStoreStub {
	return &authStoreStub{
		users:    make(map[uint64]*model.User),
		userAuth: make(map[string]*model.UserAuth),
		nextID:   1,
	}
}

func (s *authStoreStub) CreateUser(_ context.Context, user *model.User) error {
	user.ID = s.nextID
	s.nextID++
	cp := *user
	s.users[user.ID] = &cp
	return nil
}

func (s *authStoreStub) GetUser(_ context.Context, id uint64) (*model.User, error) {
	user, ok := s.users[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *user
	return &cp, nil
}

func (s *authStoreStub) CreateUserAuth(_ context.Context, auth *model.UserAuth) error {
	if _, exists := s.userAuth[auth.Username]; exists {
		return gorm.ErrDuplicatedKey // 模拟唯一约束冲突
	}
	cp := *auth
	s.userAuth[auth.Username] = &cp
	return nil
}

func (s *authStoreStub) GetUserAuthByUsername(_ context.Context, username string) (*model.UserAuth, error) {
	auth, ok := s.userAuth[username]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *auth
	return &cp, nil
}

func (s *authStoreStub) GetUserAuthByUserID(_ context.Context, userID uint64) (*model.UserAuth, error) {
	for _, auth := range s.userAuth {
		if auth.UserID == userID {
			cp := *auth
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// TestRegisterSuccess 验证注册成功返回 token 和用户信息。
func TestRegisterSuccess(t *testing.T) {
	svc := newAuthTestHarness()

	result, err := svc.Register(context.Background(), RegisterInput{
		Username: "alice",
		Password: "pass123456",
		Nickname: "爱丽丝",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if result.UserID == 0 {
		t.Fatal("expected non-zero userId")
	}
	if result.Username != "alice" {
		t.Fatalf("expected username=alice, got %s", result.Username)
	}
	if result.Nickname != "爱丽丝" {
		t.Fatalf("expected nickname=爱丽丝, got %s", result.Nickname)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token")
	}
}

// TestRegisterUsernameTooShort 验证用户名过短时返回错误。
func TestRegisterUsernameTooShort(t *testing.T) {
	svc := newAuthTestHarness()

	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "ab",
		Password: "pass123456",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

// TestRegisterPasswordNoLetter 验证密码不含字母时返回错误。
func TestRegisterPasswordNoLetter(t *testing.T) {
	svc := newAuthTestHarness()

	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "bob",
		Password: "12345678",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

// TestRegisterDuplicateUsername 验证重复用户名返回错误。
func TestRegisterDuplicateUsername(t *testing.T) {
	svc := newAuthTestHarness()

	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "alice",
		Password: "pass123456",
	})
	if err != nil {
		t.Fatalf("first register failed: %v", err)
	}

	_, err = svc.Register(context.Background(), RegisterInput{
		Username: "alice",
		Password: "pass123456",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for duplicate, got %v", err)
	}
}

// TestRegisterFallbackNickname 验证未传 nickname 时默认用 username。
func TestRegisterFallbackNickname(t *testing.T) {
	svc := newAuthTestHarness()

	result, err := svc.Register(context.Background(), RegisterInput{
		Username: "charlie",
		Password: "pass123456",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}
	if result.Nickname != "charlie" {
		t.Fatalf("expected nickname fallback to charlie, got %s", result.Nickname)
	}
}

// TestLoginSuccess 验证登录成功返回 token。
func TestLoginSuccess(t *testing.T) {
	svc := newAuthTestHarness()

	// 先注册
	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "alice",
		Password: "pass123456",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	// 再登录
	result, err := svc.Login(context.Background(), LoginInput{
		Username: "alice",
		Password: "pass123456",
	})
	if err != nil {
		t.Fatalf("login failed: %v", err)
	}
	if result.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if result.Username != "alice" {
		t.Fatalf("expected username=alice, got %s", result.Username)
	}
}

// TestLoginWrongPassword 验证密码错误返回 ErrUnauthorized。
func TestLoginWrongPassword(t *testing.T) {
	svc := newAuthTestHarness()

	_, err := svc.Register(context.Background(), RegisterInput{
		Username: "alice",
		Password: "pass123456",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	_, err = svc.Login(context.Background(), LoginInput{
		Username: "alice",
		Password: "wrongpassword1",
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

// TestLoginUserNotFound 验证不存在的用户返回 ErrUnauthorized（不暴露用户是否存在）。
func TestLoginUserNotFound(t *testing.T) {
	svc := newAuthTestHarness()

	_, err := svc.Login(context.Background(), LoginInput{
		Username: "nonexistent",
		Password: "pass123456",
	})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("expected ErrUnauthorized, got %v", err)
	}
}

// TestMe 验证当前用户信息查询。
func TestMe(t *testing.T) {
	svc, store := newAuthTestHarnessWithStore()

	result, err := svc.Register(context.Background(), RegisterInput{
		Username: "alice",
		Password: "pass123456",
		Nickname: "爱丽丝",
	})
	if err != nil {
		t.Fatalf("register failed: %v", err)
	}

	me, err := svc.Me(context.Background(), result.UserID)
	if err != nil {
		t.Fatalf("Me failed: %v", err)
	}
	if me.Username != "alice" {
		t.Fatalf("expected username=alice, got %s", me.Username)
	}
	if me.Nickname != "爱丽丝" {
		t.Fatalf("expected nickname=爱丽丝, got %s", me.Nickname)
	}
	if me.Role != "buyer" {
		t.Fatalf("expected role=buyer, got %s", me.Role)
	}
	_ = store
}

// TestMeNotFound 验证不存在的用户返回 ErrNotFound。
func TestMeNotFound(t *testing.T) {
	svc := newAuthTestHarness()

	_, err := svc.Me(context.Background(), 999)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// --- 测试辅助 ---

func newAuthTestHarness() *AuthService {
	svc, _ := newAuthTestHarnessWithStore()
	return svc
}

func newAuthTestHarnessWithStore() (*AuthService, *authStoreStub) {
	store := newAuthStoreStub()
	svc := NewAuthService(store)
	return svc, store
}
