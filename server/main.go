package main

import (
	"context"
	"log"
	"net/http"
	_ "net/http/pprof"
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

	// WebSocket 诊断端点
	r.GET("/api/ws-stats", func(c *gin.Context) {
		stats := hub.Stats()
		mCount, mTotalMs := websocketpkg.GetMarshalStats()
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"connections":          stats.TotalConnections,
				"rooms":                stats.TotalRooms,
				"broadcast_count":      stats.BroadcastCount,
				"broadcast_msgs":       stats.BroadcastMsgs,
				"slow_broadcasts":      stats.SlowBroadcasts,
				"slow_clients_dropped": stats.SlowClientsDropped,
				"event_queue_len":      hub.EventQueueLen(),
				"broadcast_wait_p50":   stats.BroadcastWaitP50,
				"broadcast_wait_p95":   stats.BroadcastWaitP95,
				"broadcast_wait_p99":   stats.BroadcastWaitP99,
				"broadcast_cost_p50":   stats.BroadcastCostP50,
				"broadcast_cost_p95":   stats.BroadcastCostP95,
				"broadcast_cost_p99":   stats.BroadcastCostP99,
				"send_channel_full":    stats.SendChannelFull,
				"marshal_count":        mCount,
				"marshal_total_ms":     mTotalMs,
				"write_pump_count":     stats.WritePumpCount,
				"write_pump_total_ms":  stats.WritePumpTotalMs,
				"write_pump_msg_count": stats.WritePumpMsgCount,
				"write_cost_p50":       stats.WriteCostP50,
				"write_cost_p95":       stats.WriteCostP95,
				"write_cost_p99":       stats.WriteCostP99,
				"write_loop_p50":       stats.WriteLoopP50,
				"write_loop_p95":       stats.WriteLoopP95,
				"write_loop_p99":       stats.WriteLoopP99,
			},
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
		if count, err := adminService.StartDueScheduledAuctions(context.Background()); err == nil && count > 0 {
			log.Printf("启动时自动上架了 %d 个定时竞拍", count)
		}
		if count, err := settleService.SettleExpiredAuctions(context.Background()); err == nil && count > 0 {
			log.Printf("启动时结算了 %d 个过期竞拍", count)
		}
		if count, err := settleService.CloseExpiredPaymentOrders(context.Background(), 500); err == nil && count > 0 {
			log.Printf("启动时关闭了 %d 个支付超时订单", count)
		}

		// 定时结算过期竞拍（每 3 秒）
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if count, err := adminService.StartDueScheduledAuctions(context.Background()); err == nil && count > 0 {
					log.Printf("定时自动上架了 %d 个竞拍", count)
				}
				if count, err := settleService.SettleExpiredAuctions(context.Background()); err == nil && count > 0 {
					log.Printf("定时结算了 %d 个过期竞拍", count)
				}
			}
		}()

		// 定时关闭支付超时订单（每 10 秒）
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				if count, err := settleService.CloseExpiredPaymentOrders(context.Background(), 500); err == nil && count > 0 {
					log.Printf("定时关闭了 %d 个支付超时订单", count)
				} else if err != nil {
					log.Printf("定时关闭支付超时订单失败: %v", err)
				}
			}
		}()
	}

	// 6. 启动 pprof 调试端口（单独 goroutine，不走 Gin 中间件）
	go func() {
		log.Println("pprof 调试端点: http://localhost:6060/debug/pprof/")
		if err := http.ListenAndServe(":6060", nil); err != nil {
			log.Printf("pprof server: %v", err)
		}
	}()

	// 7. 启动服务
	log.Printf("启动 Web 服务，监听端口: %s", cfg.ServerPort)
	if err := r.Run(":" + cfg.ServerPort); err != nil {
		log.Fatalf("启动服务失败: %v", err)
	}
}
