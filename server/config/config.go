package config

import (
	"os"
	"strconv"
)

// Config 保存拍卖系统的所有配置项。
type Config struct {
	ServerPort               string
	MySQLDSN                 string
	RedisMasterAddr          string
	RedisSlaveAddr           string
	AllowAllWebSocketOrigins bool

	// Agent Runner（常驻买家 agent 任务运行器）。
	AgentRunnerEnabled    bool
	AgentRunnerIntervalMS int

	// 全站单会话：持有者空闲超过该秒数后自动释放，其他人可登录。
	SessionIdleTTLSec int
}

// LoadConfig 从环境变量加载配置，如果不存在则返回默认配置。
func LoadConfig() *Config {
	return &Config{
		ServerPort:               getEnv("SERVER_PORT", "8080"),
		MySQLDSN:                 getEnv("MYSQL_DSN", "root:rootpassword@tcp(localhost:3306)/paimai?charset=utf8mb4&parseTime=True&loc=Local"),
		RedisMasterAddr:          getEnv("REDIS_MASTER_ADDR", "localhost:6379"),
		RedisSlaveAddr:           getEnv("REDIS_SLAVE_ADDR", "localhost:6380"),
		AllowAllWebSocketOrigins: getEnv("ALLOW_ALL_WS_ORIGINS", "true") == "true",
		AgentRunnerEnabled:       getEnv("AGENT_RUNNER_ENABLED", "true") == "true",
		AgentRunnerIntervalMS:    getEnvInt("AGENT_RUNNER_INTERVAL_MS", 2000),
		SessionIdleTTLSec:        getEnvInt("SESSION_IDLE_TTL_SEC", 90),
	}
}

// getEnvInt 读取整型环境变量，缺失或非法时返回 fallback。
func getEnvInt(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		if n, err := strconv.Atoi(value); err == nil {
			return n
		}
	}
	return fallback
}

// getEnv 读取指定环境变量；当变量不存在时返回传入的默认值。
func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}
