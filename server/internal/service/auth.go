package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
	jwtpkg "paimai/pkg/jwt"
)

// AuthService 处理用户注册、登录和身份校验。
type AuthService struct {
	store repository.AuthStore
	now   func() time.Time
}

// NewAuthService 创建认证服务。
func NewAuthService(store repository.AuthStore) *AuthService {
	return &AuthService{
		store: store,
		now:   time.Now,
	}
}

// RegisterInput 是注册请求的输入参数。
type RegisterInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	Role     string `json:"role"` // user / seller / anchor，默认 user
}

// LoginInput 是登录请求的输入参数。
type LoginInput struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthResult 是注册/登录成功后的统一返回结构。
type AuthResult struct {
	UserID   uint64 `json:"userId"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Token    string `json:"token"`
}

// MeResult 是当前用户信息的返回结构。
type MeResult struct {
	UserID    uint64 `json:"userId"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	Role      string `json:"role"`
}

var (
	// username 规则：3-32 位，字母/数字/下划线
	usernameRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)
	// password 规则：8-64 位，至少包含字母和数字
	passwordRegex = regexp.MustCompile(`^[a-zA-Z0-9!@#$%^&*()_+\-=\[\]{}|;:,.<>?]{8,64}$`)
	hasLetter     = regexp.MustCompile(`[a-zA-Z]`)
	hasDigit      = regexp.MustCompile(`[0-9]`)
)

// Register 创建新用户账号，返回 JWT token。
func (s *AuthService) Register(ctx context.Context, input RegisterInput) (*AuthResult, error) {
	input.Username = strings.TrimSpace(input.Username)
	input.Nickname = strings.TrimSpace(input.Nickname)

	if err := validateRegisterInput(input); err != nil {
		return nil, err
	}

	// 检查用户名是否已存在
	existing, err := s.store.GetUserAuthByUsername(ctx, input.Username)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("%w: username already exists", ErrInvalidInput)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	nickname := input.Nickname
	if nickname == "" {
		nickname = input.Username
	}

	// 事务：创建 User（社交资料）+ UserAuth（认证信息）
	var user *model.User
	if err := s.store.WithTx(ctx, func(tx repository.AuthStore) error {
		role := input.Role
		if role == "" {
			role = "buyer"
		}
		u := &model.User{
			Nickname: nickname,
			Role:     role,
		}
		if err := tx.CreateUser(ctx, u); err != nil {
			return err
		}
		ua := &model.UserAuth{
			UserID:       u.ID,
			Username:     input.Username,
			PasswordHash: string(hashedPassword),
		}
		if err := tx.CreateUserAuth(ctx, ua); err != nil {
			return err
		}
		user = u
		return nil
	}); err != nil {
		return nil, err
	}

	token, err := jwtpkg.GenerateToken(user.ID, input.Username, user.Role, nickname, s.now())
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		UserID:   user.ID,
		Username: input.Username,
		Nickname: nickname,
		Token:    token,
	}, nil
}

// Login 验证用户名密码，返回 JWT token。
func (s *AuthService) Login(ctx context.Context, input LoginInput) (*AuthResult, error) {
	input.Username = strings.TrimSpace(input.Username)

	if input.Username == "" || input.Password == "" {
		return nil, fmt.Errorf("%w: username and password are required", ErrInvalidInput)
	}

	userAuth, err := s.store.GetUserAuthByUsername(ctx, input.Username)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: invalid username or password", ErrUnauthorized)
		}
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(userAuth.PasswordHash), []byte(input.Password)); err != nil {
		return nil, fmt.Errorf("%w: invalid username or password", ErrUnauthorized)
	}

	user, err := s.store.GetUser(ctx, userAuth.UserID)
	if err != nil {
		return nil, err
	}

	token, err := jwtpkg.GenerateToken(user.ID, userAuth.Username, user.Role, user.Nickname, s.now())
	if err != nil {
		return nil, err
	}

	return &AuthResult{
		UserID:   user.ID,
		Username: userAuth.Username,
		Nickname: user.Nickname,
		Token:    token,
	}, nil
}

// Me 根据用户 ID 查询当前用户信息。
func (s *AuthService) Me(ctx context.Context, userID uint64) (*MeResult, error) {
	user, err := s.store.GetUser(ctx, userID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	userAuth, err := s.store.GetUserAuthByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return &MeResult{
		UserID:    user.ID,
		Username:  userAuth.Username,
		Nickname:  user.Nickname,
		AvatarURL: user.AvatarURL,
		Role:      user.Role,
	}, nil
}

func validateRegisterInput(input RegisterInput) error {
	if !usernameRegex.MatchString(input.Username) {
		return fmt.Errorf("%w: username must be 3-32 characters, letters/digits/underscore only", ErrInvalidInput)
	}
	if !passwordRegex.MatchString(input.Password) {
		return fmt.Errorf("%w: password must be 8-64 characters", ErrInvalidInput)
	}
	if !hasLetter.MatchString(input.Password) || !hasDigit.MatchString(input.Password) {
		return fmt.Errorf("%w: password must contain at least one letter and one digit", ErrInvalidInput)
	}
	if len(input.Nickname) > 64 {
		return fmt.Errorf("%w: nickname must be at most 64 characters", ErrInvalidInput)
	}
	return nil
}
