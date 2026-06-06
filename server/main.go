package main

import (
	"context"
	"log"
	"net/http"
	"time"

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
		streamCtx, streamCancel := context.WithCancel(context.Background())
		defer streamCancel()
		go streamConsumer.Start(streamCtx)
	}

	// 5. 初始化 Gin 路由
	r := gin.Default()

	// 使用 CORS 中间件
	r.Use(middleware.CORS())

	// 基础路由（无需鉴权）
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	r.GET("/api/server-time", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"serverTime": time.Now().UnixMilli(),
		})
	})

	if database != nil {
		adminStore := repository.NewGormAdminStore(database)

		// 启动 Outbox → Redis Stream 轮询器
		if redisClients != nil && redisClients.Master != nil {
			outboxPoller := stream.NewOutboxPoller(adminStore, redisClients.Master)
			go outboxPoller.Start(context.Background())
		}

		// 认证服务（注册/登录无需鉴权，在全局中间件前注册）
		authStore := repository.NewGormAuthStore(database)
		authService := service.NewAuthService(authStore)
		handler.RegisterAuthRoutes(r, authService)

		// 管理端服务（初始化，路由在中间件后注册）
		adminService := service.NewAdminService(adminStore, redisClients)
		settleService := service.NewSettleService(adminStore)
		roomService := service.NewRoomService(adminStore, settleService)

		// 用户端公开路由（无鉴权，在前端首页访问前注册）
		publicStore := repository.NewGormPublicStore(database)
		publicService := service.NewPublicService(publicStore, adminStore, redisClients, streamPublisher, settleService)
		upgraderCfg := &handler.UpgraderConfig{AllowAllOrigins: cfg.AllowAllWebSocketOrigins}
		handler.RegisterPublicRoutes(r, publicService, hub, upgraderCfg)

		// 所有 API 路由挂载鉴权中间件（认证路由、ping、公开路由已在前面注册）
		r.Use(middleware.AuthRequired())

		// 以下路由都需要鉴权
		// admin 路由额外校验角色
		adminGroup := r.Group("/api/admin")
		adminGroup.Use(middleware.AdminRequired())
		handler.RegisterAdminRoutes(adminGroup, adminService)
		handler.RegisterAdminSettleRoutes(adminGroup, settleService)
		handler.RegisterRoomRoutes(adminGroup, roomService, hub)

		// 买家端订单路由（鉴权，非 Admin）
		handler.RegisterBuyerSettleRoutes(r, settleService)

		handler.RegisterUploadRoutes(r)
		handler.RegisterAuthMeRoute(r, authService)
		handler.RegisterAddressRoutes(r.Group("/api"))

		// 启动时结算已过期的 running 竞拍
		if count, err := settleService.SettleExpiredAuctions(context.Background()); err == nil && count > 0 {
			log.Printf("启动时结算了 %d 个过期竞拍", count)
		}

		// 定时结算过期竞拍（每 10 秒）
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if count, err := settleService.SettleExpiredAuctions(context.Background()); err == nil && count > 0 {
					log.Printf("定时结算了 %d 个过期竞拍", count)
				}
			}
		}()
	}

	// 6. 启动服务
	log.Printf("启动 Web 服务，监听端口: %s", cfg.ServerPort)
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
