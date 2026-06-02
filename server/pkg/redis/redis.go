package redis

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"
)

// Clients 保存了指向 Redis 主（Master）节点和从（Slave）节点的连接客户端。
type Clients struct {
	Master *redis.Client
	Slave  *redis.Client
}

// NewRedisClients 初始化 Redis 主从节点连接，并通过 Ping 提前验证连接可用性。
func NewRedisClients(masterAddr, slaveAddr string) (*Clients, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	log.Printf("Connecting to Redis Master at %s...", masterAddr)
	masterClient := redis.NewClient(&redis.Options{
		Addr:         masterAddr,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     100,
	})

	if err := masterClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping Redis Master: %w", err)
	}

	log.Printf("Connecting to Redis Slave at %s...", slaveAddr)
	slaveClient := redis.NewClient(&redis.Options{
		Addr:         slaveAddr,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     100,
	})

	if err := slaveClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to ping Redis Slave: %w", err)
	}

	log.Println("Successfully connected to Redis Master and Slave.")
	return &Clients{
		Master: masterClient,
		Slave:  slaveClient,
	}, nil
}

// WaitReplicas 阻塞等待，直到指定数量的从副本确认了前面的写入，或者达到指定的超时时间。
func (c *Clients) WaitReplicas(ctx context.Context, numReplicas int, timeout time.Duration) (int, error) {
	timeoutMs := timeout.Milliseconds()
	res, err := c.Master.Do(ctx, "WAIT", numReplicas, timeoutMs).Int()
	if err != nil {
		return 0, err
	}
	if res < numReplicas {
		return res, errors.New("replication lag: replica sync timeout reached before target replica count acknowledged")
	}
	return res, nil
}

// Close 关闭 Redis 主从客户端连接，释放网络连接池资源。
func (c *Clients) Close() {
	if c.Master != nil {
		_ = c.Master.Close()
	}
	if c.Slave != nil {
		_ = c.Slave.Close()
	}
}
