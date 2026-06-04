package db

import (
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"paimai/internal/model"
)

// InitDB 初始化 GORM 数据库连接、配置连接池，并执行业务表结构自动迁移。
func InitDB(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 设置连接池限制
	sqlDB.SetMaxIdleConns(50)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	log.Println("Migrating database schemas...")
	err = db.AutoMigrate(
		&model.User{},
		&model.LiveRoom{},
		&model.Product{},
		&model.Auction{},
		&model.Bid{},
		&model.Order{},
		&model.UserAuth{},
	)
	if err != nil {
		return nil, err
	}

	log.Println("Database migration completed successfully.")
	return db, nil
}
