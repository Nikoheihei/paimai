package config

import (
	"os"
)

// Config 保存拍卖系统的所有配置项。
type Config struct {
	ServerPort      string
	MySQLDSN        string
	RedisMasterAddr string
	RedisSlaveAddr  string
}

// LoadConfig 从环境变量加载配置，如果不存在则返回默认配置。
func LoadConfig() *Config {
	return &Config{
		ServerPort:      getEnv("SERVER_PORT", "8080"),
		MySQLDSN:        getEnv("MYSQL_DSN", "root:rootpassword@tcp(localhost:3306)/paimai?charset=utf8mb4&parseTime=True&loc=Local"),
		RedisMasterAddr: getEnv("REDIS_MASTER_ADDR", "localhost:6379"),
		RedisSlaveAddr:  getEnv("REDIS_SLAVE_ADDR", "localhost:6380"),
	}
}

// getEnv 读取指定环境变量；当变量不存在时返回传入的默认值。
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
