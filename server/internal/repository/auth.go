package repository

import (
	"context"

	"gorm.io/gorm"

	"paimai/internal/model"
)

// AuthStore 定义用户认证所需的数据访问能力。
type AuthStore interface {
	GetUser(ctx context.Context, id uint64) (*model.User, error)
	CreateUser(ctx context.Context, user *model.User) error
	GetUserAuthByUsername(ctx context.Context, username string) (*model.UserAuth, error)
	CreateUserAuth(ctx context.Context, auth *model.UserAuth) error
	GetUserAuthByUserID(ctx context.Context, userID uint64) (*model.UserAuth, error)
	WithTx(ctx context.Context, fn func(AuthStore) error) error
}

// GormAuthStore 是基于 GORM 的 AuthStore 实现。
type GormAuthStore struct {
	readDB  *gorm.DB
	writeDB *gorm.DB
}

// NewGormAuthStore 创建 GORM 版本的用户认证数据访问对象。
func NewGormAuthStore(db *gorm.DB) *GormAuthStore {
	return NewGormAuthStoreWithRouter(db, db)
}

func NewGormAuthStoreWithRouter(readDB, writeDB *gorm.DB) *GormAuthStore {
	return &GormAuthStore{readDB: readDB, writeDB: writeDB}
}

// txGormAuthStore 是事务内使用的 AuthStore 实现，共享同一个 *gorm.DB（事务对象）。
type txGormAuthStore struct {
	db *gorm.DB
}

// WithTx 在事务中执行 fn，fn 内使用的 AuthStore 共享同一个 DB 连接。
func (s *GormAuthStore) WithTx(ctx context.Context, fn func(AuthStore) error) error {
	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txStore := &txGormAuthStore{db: tx}
		return fn(txStore)
	})
}

func (s *txGormAuthStore) CreateUser(ctx context.Context, user *model.User) error {
	return s.db.WithContext(ctx).Create(user).Error
}

func (s *txGormAuthStore) GetUser(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	if err := s.db.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *txGormAuthStore) CreateUserAuth(ctx context.Context, auth *model.UserAuth) error {
	return s.db.WithContext(ctx).Create(auth).Error
}

func (s *txGormAuthStore) GetUserAuthByUsername(ctx context.Context, username string) (*model.UserAuth, error) {
	var auth model.UserAuth
	if err := s.db.WithContext(ctx).Where("username = ?", username).First(&auth).Error; err != nil {
		return nil, err
	}
	return &auth, nil
}

func (s *txGormAuthStore) GetUserAuthByUserID(ctx context.Context, userID uint64) (*model.UserAuth, error) {
	var auth model.UserAuth
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&auth).Error; err != nil {
		return nil, err
	}
	return &auth, nil
}

func (s *txGormAuthStore) WithTx(ctx context.Context, fn func(AuthStore) error) error {
	panic("nested transaction not supported")
}

func (s *GormAuthStore) CreateUser(ctx context.Context, user *model.User) error {
	return s.writeDB.WithContext(ctx).Create(user).Error
}

func (s *GormAuthStore) GetUser(ctx context.Context, id uint64) (*model.User, error) {
	var user model.User
	if err := s.writeDB.WithContext(ctx).First(&user, id).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

func (s *GormAuthStore) CreateUserAuth(ctx context.Context, auth *model.UserAuth) error {
	return s.writeDB.WithContext(ctx).Create(auth).Error
}

func (s *GormAuthStore) GetUserAuthByUsername(ctx context.Context, username string) (*model.UserAuth, error) {
	var auth model.UserAuth
	if err := s.writeDB.WithContext(ctx).Where("username = ?", username).First(&auth).Error; err != nil {
		return nil, err
	}
	return &auth, nil
}

func (s *GormAuthStore) GetUserAuthByUserID(ctx context.Context, userID uint64) (*model.UserAuth, error) {
	var auth model.UserAuth
	if err := s.writeDB.WithContext(ctx).Where("user_id = ?", userID).First(&auth).Error; err != nil {
		return nil, err
	}
	return &auth, nil
}
