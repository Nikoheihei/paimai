package main

import (
	"context"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"paimai/config"
	"paimai/internal/handler"
	"paimai/internal/repository"
	"paimai/internal/service"
	"paimai/internal/stream"
	websocketpkg "paimai/internal/websocket"
	"paimai/pkg/db"
	"paimai/pkg/middleware"
	"paimai/pkg/redis"
)

// main 是后端服务的启动入口，负责加载配置、初始化基础设施并注册 HTTP 路由。
func main() {
	// 1. 加载配置
	cfg := config.LoadConfig()

	// 2. 初始化数据库连接 (如果容器未启动，仅记录错误但不崩溃，便于非 Docker 环境编译运行)
	database, err := db.InitDB(cfg.MySQLDSN)
	if err != nil {
		log.Printf("[警告] 无法连接到数据库: %v。请确保 MySQL 容器已启动并在运行中。", err)
	} else {
		log.Println("数据库初始化成功，表结构已自动迁移。")
		_ = database
	}

	// 3. 初始化 Redis 主从客户端连接
	redisClients, err := redis.NewRedisClients(cfg.RedisMasterAddr, cfg.RedisSlaveAddr)
	if err != nil {
		log.Printf("[警告] 无法连接到 Redis: %v。请确保 Redis Master/Slave 容器已启动。", err)
	} else {
		log.Println("Redis 客户端连接成功。")
		defer redisClients.Close()
	}

	// 4. 初始化 WebSocket Hub 和 Stream 事件基础设施
	hub := websocketpkg.NewHub()
	go hub.Run()

	var streamPublisher *stream.Publisher
	if redisClients != nil && redisClients.Master != nil {
		streamPublisher = stream.NewPublisher(redisClients.Master)
		streamConsumer := stream.NewConsumer(redisClients.Master, hub)
		go streamConsumer.Start(context.Background())
	}

	// 5. 初始化 Gin 路由
	r := gin.Default()

	// 使用 CORS 中间件
	r.Use(middleware.CORS())

	// 基础路由
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	if database != nil {
		adminStore := repository.NewGormAdminStore(database)
		adminService := service.NewAdminService(adminStore, redisClients)
		handler.RegisterAdminRoutes(r, adminService)

		publicStore := repository.NewGormPublicStore(database)
		publicService := service.NewPublicService(publicStore, redisClients, streamPublisher)
		handler.RegisterPublicRoutes(r, publicService, hub)
	}

	// 6. 启动服务
	log.Printf("启动 Web 服务，监听端口: %s", cfg.ServerPort)
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
