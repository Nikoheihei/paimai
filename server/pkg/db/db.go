package db

import (
	"log"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"paimai/internal/model"
)

// Router keeps write and read handles explicit. Write is the only handle used
// for migrations, commands, transactions, and post-write strong-consistency
// reads. Read is for initial snapshots and eventually consistent list/detail
// queries.
type Router struct {
	Write *gorm.DB
	Read  *gorm.DB
}

// InitDB 初始化 GORM 数据库连接、配置连接池，并执行业务表结构自动迁移。
func InitDB(dsn string) (*gorm.DB, error) {
	router, err := InitRouter(dsn, dsn)
	if err != nil {
		return nil, err
	}
	return router.Write, nil
}

func InitRouter(writeDSN, readDSN string) (*Router, error) {
	writeDB, err := openDB(writeDSN)
	if err != nil {
		return nil, err
	}
	if readDSN == "" {
		readDSN = writeDSN
	}

	readDB := writeDB
	if readDSN != writeDSN {
		readDB, err = openDB(readDSN)
		if err != nil {
			return nil, err
		}
	}

	log.Println("Migrating database schemas on write DB...")
	if err := migrateSchemas(writeDB); err != nil {
		return nil, err
	}
	if err := migrateCompatibility(writeDB); err != nil {
		return nil, err
	}

	log.Println("Database router initialized successfully.")
	return &Router{Write: writeDB, Read: readDB}, nil
}

func openDB(dsn string) (*gorm.DB, error) {
	database, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, err
	}

	// 设置连接池限制
	sqlDB.SetMaxIdleConns(50)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return database, nil
}

func migrateSchemas(database *gorm.DB) error {
	return database.AutoMigrate(
		&model.User{},
		&model.LiveRoom{},
		&model.Product{},
		&model.Auction{},
		&model.Bid{},
		&model.Order{},
		&model.UserAuth{},
		&model.Address{},
		&model.OutboxEvent{},
		&model.AgentProfile{},
		&model.AgentAuctionMatch{},
		&model.AgentBidAttempt{},
		&model.AgentPact{},
		&model.AgentAuditLog{},
		&model.AgentBiddingRule{},
		&model.AgentEpisodeSummary{},
		&model.MerchantAgentJob{},
	)
}

func migrateCompatibility(db *gorm.DB) error {
	if err := db.Exec("ALTER TABLE auctions MODIFY COLUMN status ENUM('draft','scheduled','running','sold','failed','cancelled','payment_timeout') NOT NULL DEFAULT 'draft'").Error; err != nil {
		return err
	}
	if err := db.Exec("ALTER TABLE products MODIFY COLUMN status ENUM('available','locked','offline') NOT NULL DEFAULT 'available'").Error; err != nil {
		return err
	}
	if err := db.Exec("UPDATE products SET status = 'available' WHERE status IS NULL OR status = ''").Error; err != nil {
		return err
	}
	return nil
}
