//go:build integration
// +build integration

package service

import (
	"context"
	"testing"

	"paimai/internal/repository"
)

// TestRegisterLoginMeIntegration 验证 Register → Login → Me 全链路数据一致性。
func TestRegisterLoginMeIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()

	authStore := repository.NewGormAuthStore(db)
	authSvc := NewAuthService(authStore)

	username := "int_user_" + t.Name()
	input := RegisterInput{
		Username: username,
		Password: "Pass1234",
		Nickname: "集成测试用户",
	}

	// Step 1: 注册
	regResult, err := authSvc.Register(ctx, input)
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if regResult.UserID == 0 {
		t.Fatal("expected non-zero UserID")
	}
	if regResult.Username != username {
		t.Errorf("expected username %q, got %q", username, regResult.Username)
	}
	if regResult.Nickname != "集成测试用户" {
		t.Errorf("expected nickname '集成测试用户', got %q", regResult.Nickname)
	}
	if regResult.Token == "" {
		t.Fatal("expected non-empty token")
	}

	// Step 2: 登录
	loginResult, err := authSvc.Login(ctx, LoginInput{
		Username: username,
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loginResult.UserID != regResult.UserID {
		t.Errorf("Login UserID %d != Register UserID %d", loginResult.UserID, regResult.UserID)
	}
	if loginResult.Token == "" {
		t.Fatal("expected non-empty token from login")
	}

	// Step 3: Me
	meResult, err := authSvc.Me(ctx, regResult.UserID)
	if err != nil {
		t.Fatalf("Me() error = %v", err)
	}
	if meResult.UserID != regResult.UserID {
		t.Errorf("Me UserID %d != Register UserID %d", meResult.UserID, regResult.UserID)
	}
	if meResult.Username != username {
		t.Errorf("Me username %q != Register username %q", meResult.Username, username)
	}
	if meResult.Role == "" {
		t.Errorf("expected non-empty role")
	}
}

// TestLoginWrongPasswordIntegration 验证注册后错误密码返回 ErrUnauthorized。
func TestLoginWrongPasswordIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()

	authStore := repository.NewGormAuthStore(db)
	authSvc := NewAuthService(authStore)

	username := "int_pw_" + t.Name()
	_, err = authSvc.Register(ctx, RegisterInput{
		Username: username,
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err = authSvc.Login(ctx, LoginInput{
		Username: username,
		Password: "WrongPass1",
	})
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
	// 验证是 ErrUnauthorized
	if err.Error() != "unauthorized: invalid username or password" {
		t.Errorf("expected unauthorized error, got: %v", err)
	}
}

// TestRegisterDuplicateUsernameIntegration 验证同一用户名注册两次返回 ErrInvalidInput。
func TestRegisterDuplicateUsernameIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()

	authStore := repository.NewGormAuthStore(db)
	authSvc := NewAuthService(authStore)

	username := "int_dup_" + t.Name()
	_, err = authSvc.Register(ctx, RegisterInput{
		Username: username,
		Password: "Pass1234",
	})
	if err != nil {
		t.Fatalf("第一次 Register() error = %v", err)
	}

	_, err = authSvc.Register(ctx, RegisterInput{
		Username: username,
		Password: "OtherPass1",
	})
	if err == nil {
		t.Fatal("expected error for duplicate username")
	}
	if err.Error() != "invalid input: username already exists" {
		t.Errorf("expected duplicate username error, got: %v", err)
	}
}

// TestRegisterFallbackNicknameIntegration 验证未提供 nickname 时自动回退为 username。
func TestRegisterFallbackNicknameIntegration(t *testing.T) {
	db, err := realDB()
	if err != nil {
		t.Skipf("跳过集成测试（数据库不可用）: %v", err)
	}
	ctx := context.Background()

	authStore := repository.NewGormAuthStore(db)
	authSvc := NewAuthService(authStore)

	username := "int_fb_" + t.Name()
	result, err := authSvc.Register(ctx, RegisterInput{
		Username: username,
		Password: "Pass1234",
		// 不传 Nickname，应该回退为 username
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if result.Nickname != username {
		t.Errorf("expected nickname fallback to username %q, got %q", username, result.Nickname)
	}
}
