package repository

import (
	"context"

	"gorm.io/gorm"

	"paimai/internal/model"
)

type AddressStore interface {
	ListAddresses(ctx context.Context, userID uint64) ([]model.Address, error)
	CreateAddress(ctx context.Context, address *model.Address) error
	UpdateAddress(ctx context.Context, userID, id uint64, input model.Address) (*model.Address, error)
	DeleteAddress(ctx context.Context, userID, id uint64) error
}

type GormAddressStore struct {
	readDB  *gorm.DB
	writeDB *gorm.DB
}

func NewGormAddressStore(db *gorm.DB) *GormAddressStore {
	return NewGormAddressStoreWithRouter(db, db)
}

func NewGormAddressStoreWithRouter(readDB, writeDB *gorm.DB) *GormAddressStore {
	return &GormAddressStore{readDB: readDB, writeDB: writeDB}
}

func (s *GormAddressStore) ListAddresses(ctx context.Context, userID uint64) ([]model.Address, error) {
	var result []model.Address
	err := s.readDB.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("is_default DESC, id DESC").
		Find(&result).Error
	return result, err
}

func (s *GormAddressStore) CreateAddress(ctx context.Context, address *model.Address) error {
	return s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if address.IsDefault {
			if err := tx.Model(&model.Address{}).Where("user_id = ?", address.UserID).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Create(address).Error
	})
}

func (s *GormAddressStore) UpdateAddress(ctx context.Context, userID, id uint64, input model.Address) (*model.Address, error) {
	var updated model.Address
	err := s.writeDB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("id = ? AND user_id = ?", id, userID).First(&updated).Error; err != nil {
			return err
		}
		updated.Name = input.Name
		updated.Phone = input.Phone
		updated.Province = input.Province
		updated.City = input.City
		updated.District = input.District
		updated.Detail = input.Detail
		updated.IsDefault = input.IsDefault
		if input.IsDefault {
			if err := tx.Model(&model.Address{}).Where("user_id = ? AND id <> ?", userID, id).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Save(&updated).Error
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *GormAddressStore) DeleteAddress(ctx context.Context, userID, id uint64) error {
	return s.writeDB.WithContext(ctx).
		Where("id = ? AND user_id = ?", id, userID).
		Delete(&model.Address{}).Error
}
