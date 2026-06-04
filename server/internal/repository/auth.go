package repository

import (
	"context"

	"gorm.io/gorm"

	"paimai/internal/model"
)

// AuthStore 定义用户认证所需的数据访问能力。
type AuthStore interface {
	CreateUser(ctx context.Context, user *model.User) error
	GetUser(ctx context.Context, id uint64) (*model.User, error)
	CreateUserAuth(ctx context.Context, auth *model.UserAuth) error
	GetUserAuthByUsername(ctx context.Context, username string) (*model.UserAuth, error)
	GetUserAuthByUserID(ctx context.Context, userID uint64) (*model.UserAuth, error)
}

// GormAuthStore 是基于 GORM 的 AuthStore 实现。
type GormAuthStore struct {
	db *gorm.DB
}

// NewGormAuthStore 创建 GORM 版本的用户认证数据访问对象。
func NewGormAuthStore(db *gorm.DB) *GormAuthStore {
	return &GormAuthStore{db: db}
}

func (s *GormAuthStore) CreateUser(ctx context.Context, user *model.User) error {
	return s.db.WithContext(ctx).Create(user).Error
}

func (s *GormAuthStore) GetUser(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *GormAuthStore) CreateUserAuth(ctx context.Context, auth *model.UserAuth) error {
	return s.db.WithContext(ctx).Create(auth).Error
}

func (s *GormAuthStore) GetUserAuthByUsername(ctx context.Context, username string) (*model.UserAuth, error) {
	var auth model.UserAuth
	if err := s.db.WithContext(ctx).Where("username = ?", username).First(&auth).Error; err != nil {
		return nil, err
	}
	return &auth, nil
}

func (s *GormAuthStore) GetUserAuthByUserID(ctx context.Context, userID uint64) (*model.UserAuth, error) {
	var auth model.UserAuth
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&auth).Error; err != nil {
		return nil, err
	}
	return &auth, nil
}
