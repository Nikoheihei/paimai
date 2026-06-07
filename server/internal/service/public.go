package service

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"paimai/internal/model"
	"paimai/internal/repository"
	"paimai/internal/stream"
	redisclient "paimai/pkg/redis"
)

// ErrBidEngineUnavailable 表示当前服务没有可用 Redis 出价引擎。
var ErrBidEngineUnavailable = errors.New("bid engine unavailable")

// BidRejectError 表示出价被业务规则拒绝。
type BidRejectError struct {
	Code    string
	Message string
}

func (e *BidRejectError) Error() string { return e.Message }

// ErrInvalidInput 是输入参数校验失败的通用错误。

// PublicService 聚合用户端直播间、竞拍详情、排行榜和出价能力。
//
// 架构变更（2026-06-04）：
//   - MySQL 是唯一 Truth Source，所有业务写入经过 MySQL 事务
//   - Redis 只做热缓存和预校验，不再做最终状态写入
//   - 出价事件通过 MySQL Outbox → Redis Stream 异步分发
type PublicService struct {
	store  repository.PublicStore
	admin  repository.AdminStore
	redis  *redisclient.Clients
	stream *stream.Publisher
	settle *SettleService
	now    func() time.Time
}

// NewPublicService 创建用户端服务。
func NewPublicService(store repository.PublicStore, adminStore repository.AdminStore, redisClients *redisclient.Clients, publisher *stream.Publisher, settleService *SettleService) *PublicService {
	return &PublicService{
		store:  store,
		admin:  adminStore,
		redis:  redisClients,
		stream: publisher,
		settle: settleService,
		now:    time.Now,
	}
}

// BidInput 是用户提交出价时的输入参数。
type BidInput struct {
	UserID         uint64 `json:"userId"`
	AmountCents    int64  `json:"amountCents"`
	IdempotencyKey string `json:"idempotencyKey"`
	ClientTS       int64  `json:"clientTs"`
}

// BidResult 是一次出价处理后的响应快照。
type BidResult struct {
	Accepted          bool   `json:"accepted"`
	AuctionID         uint64 `json:"auctionId"`
	UserID            uint64 `json:"userId"`
	AmountCents       int64  `json:"amountCents"`
	CurrentPriceCents int64  `json:"currentPriceCents"`
	Status            string `json:"status"`
	EndAt             string `json:"endAt"`
	Extended          bool   `json:"extended"`
	Sold              bool   `json:"sold"`
	ReserveMet        bool   `json:"reserveMet"`
	IdempotentReplay  bool   `json:"idempotentReplay"`
	TooFrequent       bool   `json:"tooFrequent"`
}

// RankingItem 表示排行榜上的单条条目。
type RankingItem struct {
	Rank        int    `json:"rank"`
	UserID      uint64 `json:"userId"`
	AmountCents int64  `json:"amountCents"`
}

// ListLiveRooms 返回所有正在直播的直播间列表。
func (s *PublicService) ListLiveRooms(ctx context.Context) ([]model.LiveRoom, error) {
	return s.store.ListLiveRooms(ctx)
}

// RoomDetail 是直播间详情，包含主播信息。
type RoomDetail struct {
	model.LiveRoom
	AnchorNickname string `json:"anchorNickname"`
	AnchorAvatar   string `json:"anchorAvatar"`
}

// GetRoom 查询单个直播间的详情，同时返回主播信息。
func (s *PublicService) GetRoom(ctx context.Context, id uint64) (*RoomDetail, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: roomId is required", ErrInvalidInput)
	}
	room, err := s.store.GetRoom(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: room %d", ErrNotFound, id)
		}
		return nil, err
	}
	detail := &RoomDetail{LiveRoom: *room}
	user, err := s.store.GetUser(ctx, room.SellerID)
	if err == nil && user != nil {
		detail.AnchorNickname = user.Nickname
		detail.AnchorAvatar = user.AvatarURL
	}
	return detail, nil
}

// GetAuction 查询单个竞拍的详情。
func (s *PublicService) GetAuction(ctx context.Context, id uint64) (*model.Auction, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: auctionId is required", ErrInvalidInput)
	}
	return s.store.GetAuction(ctx, id)
}

// AuctionWithProduct 是包含商品信息的竞拍。
type AuctionWithProduct struct {
	model.Auction
	ProductName  string `json:"productName"`
	ProductImage string `json:"productImage"`
}

// ListRoomAuctions 查询指定直播间的竞拍列表，可按状态过滤，同时填充商品信息。
func (s *PublicService) ListRoomAuctions(ctx context.Context, roomID uint64, status string) ([]AuctionWithProduct, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("%w: roomId is required", ErrInvalidInput)
	}
	auctions, err := s.store.ListRoomAuctions(ctx, roomID, status)
	if err != nil {
		return nil, err
	}
	result := make([]AuctionWithProduct, 0, len(auctions))
	for _, a := range auctions {
		awp := AuctionWithProduct{Auction: a}
		product, perr := s.store.GetProduct(ctx, a.ProductID)
		if perr == nil && product != nil {
			awp.ProductName = product.Name
			awp.ProductImage = product.ImageURL
		}
		result = append(result, awp)
	}
	return result, nil
}

// GetRanking 获取指定竞拍的排行榜，优先从 Redis ZSET 读取，回退到数据库。
func (s *PublicService) GetRanking(ctx context.Context, auctionID uint64, limit int) ([]RankingItem, error) {
	if auctionID == 0 {
		return nil, fmt.Errorf("%w: auctionId is required", ErrInvalidInput)
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if s.redis != nil && s.redis.Master != nil {
		items, err := s.rankingFromRedis(ctx, auctionID, limit)
		if err == nil {
			// 按金额降序排序后分配 Rank（不依赖 Redis 返回顺序）
			sortRankingItems(items)
			return items, nil
		}
	}
	return s.rankingFromDB(ctx, auctionID, limit)
}

// PlaceBid 出价主流程。
//
// 写入路径（MySQL 唯一写入点）：
//
//	① Redis 预校验（价格比较 + 幂等 + 频率） → 快速拒绝
//	② MySQL 事务（bids + auctions 乐观锁 + outbox 事件）
//	③ outbox → Redis Stream → Consumer 更新 Redis 状态 + WS 广播
//	④ 同步结算（仅触顶成交时）
//
// Redis 仅作为缓存和预过滤层，MySQL 是唯一 Truth Source。
// 只有 MySQL 乐观锁更新成功后，Consumer 才会更新 Redis 并广播 WS 事件。
func (s *PublicService) PlaceBid(ctx context.Context, auctionID uint64, input BidInput) (*BidResult, error) {
	if err := validateBidInput(auctionID, input); err != nil {
		return nil, err
	}

	now := s.now()
	idemKey := fmt.Sprintf("idem:%d:%s", auctionID, md5Hash(input.IdempotencyKey))
	inflightKey := fmt.Sprintf("idem_inflight:%d:%s", auctionID, md5Hash(input.IdempotencyKey))

	// ① Redis 预校验：幂等/cache → inflight → 频率 → 价格比较
	// 如果 bidAmount <= Redis 当前价，直接拒绝，不进入 MySQL
	if s.redis != nil && s.redis.Master != nil {
		luaResult, err := runBidLiteScript(ctx, s.redis.Master, auctionID, input, now)
		if err != nil {
			return nil, err
		}

		// 幂等缓存命中 → 从 Redis 读取完整响应并返回
		if luaResult.code == "IDEMPOTENT_REPLAY" {
			return s.readCachedBidResult(ctx, idemKey, auctionID, input.UserID), nil
		}

		// inflight 锁（Go 层 SETNX，30s TTL）：防止同一幂等键并发处理
		set, err := s.redis.Master.SetNX(ctx, inflightKey, "1", 30*time.Second).Result()
		if err != nil || !set {
			return &BidResult{
				Accepted:  false,
				AuctionID: auctionID,
				UserID:    input.UserID,
				Status:    "IN_FLIGHT",
			}, &BidRejectError{Code: "IN_FLIGHT", Message: bidRejectMessage("IN_FLIGHT")}
		}

		// 价格预过滤：bidAmount <= Redis 当前价 → 直接拒绝，不进入 MySQL
		if luaResult.code == "BID_TOO_LOW" {
			// 清理 inflightKey
			go func() {
				cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				s.redis.Master.Del(cleanCtx, inflightKey)
			}()
			return luaResult.toBidResult(auctionID, input.UserID),
				&BidRejectError{Code: luaResult.code, Message: bidRejectMessage(luaResult.code)}
		}

		if !luaResult.accepted {
			return luaResult.toBidResult(auctionID, input.UserID),
				&BidRejectError{Code: luaResult.code, Message: bidRejectMessage(luaResult.code)}
		}
	}

	// ② MySQL 事务写入（唯一 Truth Source）
	var result *BidResult
	if err := s.admin.WithTx(ctx, func(tx repository.AdminStore) error {
		// 读取当前竞拍（事务内读取，避免 snapshot 隔离导致 version 过期）
		auction, err := tx.GetAuction(ctx, auctionID)
		if err != nil {
			return err
		}
		if auction.Status != "running" {
			result = &BidResult{Accepted: false, Status: auction.Status}
			return nil
		}
		if now.After(auction.EndAt) {
			result = &BidResult{Accepted: false, Status: "ended"}
			return nil
		}

		// 检查出价合法性
		if input.AmountCents < auction.CurrentPriceCents+auction.BidIncrementCents {
			result = &BidResult{Accepted: false}
			return nil
		}

		// 计算更新后的竞拍状态
		newPrice := input.AmountCents
		sold := false
		if auction.CapPriceCents > 0 && newPrice >= auction.CapPriceCents {
			newPrice = auction.CapPriceCents
			sold = true
		}

		newEndAt := auction.EndAt
		extended := false
		if !sold && auction.Mode == "extension" && auction.ExtendThresholdSec > 0 {
			remaining := auction.EndAt.Sub(now)
			if remaining <= time.Duration(auction.ExtendThresholdSec)*time.Second {
				newEndAt = now.Add(time.Duration(auction.ExtendDurationSec) * time.Second)
				extended = true
			}
		}

		newStatus := "running"
		if sold {
			newStatus = "sold"
		}

		// 写入出价记录（唯一索引防重复）
		serverTS := now.UnixMilli()
		clientTS := input.ClientTS
		if clientTS == 0 {
			clientTS = serverTS
		}
		bid := &model.Bid{
			AuctionID:      auctionID,
			UserID:         input.UserID,
			AmountCents:    newPrice,
			IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
			ClientTS:       clientTS,
			ServerTS:       serverTS,
			Accepted:       true,
		}
		if err := tx.CreateBid(ctx, bid); err != nil {
			// 唯一索引冲突 → 幂等重放
			if strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "UNIQUE") {
				result = s.assembleDuplicateBidResult(tx, ctx, auctionID, input, auction)
				// 仅缓存幂等结果到 Redis，不更新排行榜（Consumer 已处理过）
				if s.redis != nil && s.redis.Master != nil {
					resultJSON, _ := json.Marshal(result)
					pipe := s.redis.Master.Pipeline()
					pipe.Set(ctx, idemKey, string(resultJSON), 86400*time.Second)
					pipe.Del(ctx, inflightKey)
					pipe.Exec(ctx)
				}
				return nil
			}
			return err
		}

		// 更新竞拍状态（乐观锁）
		auction.CurrentPriceCents = newPrice
		auction.WinnerUserID = &input.UserID
		auction.EndAt = newEndAt
		auction.Status = newStatus
		if err := tx.UpdateAuctionBidState(ctx, auction); err != nil {
			return err
		}

		// 写入 outbox 事件（MySQL 事务内，保证与 bid/auction 原子性）
		eventPayload, _ := json.Marshal(map[string]interface{}{
			"type":      "bid.accepted",
			"auctionId": auctionID,
			"userId":    input.UserID,
			"amount":    newPrice,
			"price":     newPrice,
			"status":    newStatus,
			"sold":      sold,
			"extended":  extended,
			"roomId":    auction.RoomID,
		})
		eventUUID := uuid()
		if err := tx.CreateOutboxEvent(ctx, &model.OutboxEvent{
			EventType: "bid.accepted",
			Payload:   string(eventPayload),
			Status:    "pending",
			EventUUID: eventUUID,
		}); err != nil {
			return err
		}

		result = &BidResult{
			Accepted:          true,
			AuctionID:         auctionID,
			UserID:            input.UserID,
			AmountCents:       input.AmountCents,
			CurrentPriceCents: newPrice,
			Status:            newStatus,
			EndAt:             newEndAt.Format(time.RFC3339Nano),
			Extended:          extended,
			Sold:              sold,
			ReserveMet:        true,
			IdempotentReplay:  false,
			TooFrequent:       false,
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if result == nil {
		return nil, fmt.Errorf("unexpected nil result")
	}
	if !result.Accepted {
		// 事务内 reject → 清理 inflightKey
		if s.redis != nil && s.redis.Master != nil {
			go func() {
				cleanCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				s.redis.Master.Del(cleanCtx, inflightKey)
			}()
		}
		return result, &BidRejectError{Code: result.Status, Message: bidRejectMessage(result.Status)}
	}
	if result.IdempotentReplay {
		return result, nil
	}

	// ③ 仅缓存幂等响应到 Redis + 清理 inflightKey
	// Redis 状态更新（价格、排行榜）统一由 Consumer 在 outbox → stream 后完成
	if s.redis != nil && s.redis.Master != nil {
		go func() {
			cacheCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			resultJSON, err := json.Marshal(result)
			if err != nil {
				return
			}
			pipe := s.redis.Master.Pipeline()
			pipe.Set(cacheCtx, idemKey, string(resultJSON), 86400*time.Second)
			pipe.Del(cacheCtx, inflightKey)
			pipe.Exec(cacheCtx)
		}()
	}

	// ④ 同步结算（触顶成交 — 带 2 次重试）
	if result.Sold && s.settle != nil {
		go func() {
			time.Sleep(200 * time.Millisecond)
			settleWithRetry(context.Background(), auctionID, s.settle, 2)
		}()
	}
	return result, nil
}

// rankingFromRedis 从 Redis ZSET 读取排行榜热数据，返回结果按金额降序排列。
func (s *PublicService) rankingFromRedis(ctx context.Context, auctionID uint64, limit int) ([]RankingItem, error) {
	key := fmt.Sprintf("auction:%d:bids", auctionID)
	values, err := s.redis.Master.ZRevRangeWithScores(ctx, key, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	items := make([]RankingItem, 0, len(values))
	for index, value := range values {
		userID, err := strconv.ParseUint(value.Member.(string), 10, 64)
		if err != nil {
			continue
		}
		items = append(items, RankingItem{
			Rank:        index + 1,
			UserID:      userID,
			AmountCents: int64(value.Score),
		})
	}
	return items, nil
}

// sortRankingItems 按金额降序排列排行榜条目并重新分配排名。
func sortRankingItems(items []RankingItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].AmountCents > items[j].AmountCents
	})
	for i := range items {
		items[i].Rank = i + 1
	}
}

// rankingFromDB 从数据库出价记录生成兜底排行榜，按金额降序排列后分配排名。
func (s *PublicService) rankingFromDB(ctx context.Context, auctionID uint64, limit int) ([]RankingItem, error) {
	bids, err := s.store.ListAuctionBids(ctx, auctionID, limit)
	if err != nil {
		return nil, err
	}
	items := make([]RankingItem, 0, len(bids))
	for index, bid := range bids {
		items = append(items, RankingItem{
			Rank:        index + 1,
			UserID:      bid.UserID,
			AmountCents: bid.AmountCents,
		})
	}
	// 显式按金额降序排序（不依赖仓储层排序约定）
	sortRankingItems(items)
	return items, nil
}

// validateBidInput 校验出价请求的基础字段。
func validateBidInput(auctionID uint64, input BidInput) error {
	if auctionID == 0 || input.UserID == 0 {
		return fmt.Errorf("%w: auctionId and userId are required", ErrInvalidInput)
	}
	if input.AmountCents <= 0 {
		return fmt.Errorf("%w: amountCents must be positive", ErrInvalidInput)
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 128 {
		return fmt.Errorf("%w: idempotencyKey is required and must be within 128 chars", ErrInvalidInput)
	}
	return nil
}

// ================================================================
// 重构说明（2026-06-04）
//
// 移除的旧代码：
//   - persistAcceptedBid — MySQL 写入移到 PlaceBid 内的 WithTx 中
//   - bidScript（完整 Lua）— 拆分为 bidLiteScript（只做预校验）
//   - WaitReplicas — 不再等待 Redis 副本确认
//   - mustParseEndAt — 改用 time.Format 直接格式化
//
// 新增的代码：
//   - runBidLiteScript — 只读预校验 Lua
//   - sortRankingItems — 显式排序排行榜
//   - Outbox 事件写入（在 MySQL 事务中）
//
// 保留的旧代码（测试依赖）：
//   - BidResult / BidInput / RankingItem 结构体
//   - validateBidInput
//   - bidRejectMessage
//   - bidLuaResult / toBidResult / runBidScript（老脚本，后续可删）
//   - rankingFromRedis / rankingFromDB（接口签名不变）
// ================================================================

// ================================================================
// 以下为旧代码保留区 — 供测试引用，后续逐步清理
// ================================================================

// bidRejectMessage 返回拒绝码对应的中文提示。
func bidRejectMessage(code string) string {
	switch code {
	case "IDEMPOTENT_REPLAY":
		return "" // 幂等重放不视为错误
	case "IN_FLIGHT":
		return "出价正在处理中，请重试"
	case "AUCTION_NOT_RUNNING":
		return "竞拍未在进行中"
	case "AUCTION_ENDED":
		return "竞拍已经结束"
	case "BID_TOO_LOW":
		return "出价低于最低加价幅度"
	case "BID_STEP_INVALID":
		return "出价金额不符合加价步长"
	case "BID_TOO_FREQUENT":
		return "出价过于频繁，请稍后再试"
	case "AUCTION_CACHE_MISSING":
		return "竞拍缓存不可用，请刷新页面"
	case "INVALID_RULE":
		return "竞拍规则配置异常"
	default:
		return "出价被拒绝"
	}
}

// bidLuaResult 保留用于向后兼容。
type bidLuaResult struct {
	accepted          bool
	amountCents       int64
	currentPriceCents int64
	status            string
	endAtUnixMilli    int64
	extended          bool
	sold              bool
	reserveMet        bool
	tooFrequent       bool
	idempotentReplay  bool
	code              string
}

func (r bidLuaResult) toBidResult(auctionID uint64, userID uint64) *BidResult {
	endAt := ""
	if r.endAtUnixMilli > 0 {
		endAt = time.UnixMilli(r.endAtUnixMilli).Format(time.RFC3339Nano)
	}
	return &BidResult{
		Accepted:          r.accepted,
		AuctionID:         auctionID,
		UserID:            userID,
		AmountCents:       r.amountCents,
		CurrentPriceCents: r.currentPriceCents,
		Status:            r.status,
		EndAt:             endAt,
		Extended:          r.extended,
		Sold:              r.sold,
		ReserveMet:        r.reserveMet,
		IdempotentReplay:  false,
		TooFrequent:       r.tooFrequent,
	}
}

// runBidScript 保留向后兼容，新代码应使用 runBidLiteScript。
func runBidScript(ctx context.Context, client *goredis.Client, auctionID uint64, input BidInput, now time.Time) (bidLuaResult, error) {
	return runBidLiteScript(ctx, client, auctionID, input, now)
}

// bidLiteScript 只做预校验的 Lua 脚本，不写入任何 Redis 状态。
// 增加价格比较：bidAmount <= Redis 当前价 → 直接拒绝，不进入 MySQL。
var bidLiteScript = goredis.NewScript(`
local stateKey = KEYS[1]
local idemKey = KEYS[2]

local userId = ARGV[1]
local amount = tonumber(ARGV[2])
local nowMs = tonumber(ARGV[3])

local status = redis.call("HGET", stateKey, "status")
if not status then
  return {0, 0, 0, "", 0, 0, 0, 0, 0, "AUCTION_CACHE_MISSING"}
end


-- ① 幂等缓存检查（长 TTL 24h）：完整响应已缓存 → 直接返回
local cachedJson = redis.call("GET", idemKey)
if cachedJson then
  return {1, 0, 0, "", 0, 0, 0, 0, 0, "IDEMPOTENT_REPLAY"}
end



local current = tonumber(redis.call("HGET", stateKey, "currentPriceCents") or "0")
local endAt = tonumber(redis.call("HGET", stateKey, "endAtUnixMilli") or "0")

-- ② 价格预过滤：bidAmount <= Redis 当前价 → 直接拒绝，不进入 MySQL
local increment = tonumber(redis.call("HGET", stateKey, "bidIncrementCents") or "0")
if increment > 0 and amount <= current then
  return {0, amount, current, status, endAt, 0, 0, 0, 0, "BID_TOO_LOW"}
end

-- ③ 频率检查（幂等检查之后，幂等重放不走此路径）
local lastBidKey = KEYS[3]
local minIntervalMs = 1000
local lastTs = redis.call("GET", lastBidKey)
if lastTs and tonumber(lastTs) + minIntervalMs > nowMs then
  return {0, amount, current, status, endAt, 0, 0, 0, 1, "BID_TOO_FREQUENT"}
end

if status ~= "running" then
  return {0, amount, current, status, endAt, 0, 0, 0, 0, "AUCTION_NOT_RUNNING"}
end
if nowMs >= endAt then
  return {0, amount, current, status, endAt, 0, 0, 0, 0, "AUCTION_ENDED"}
end

local capPrice = tonumber(redis.call("HGET", stateKey, "capPriceCents") or "0")
local startPrice = tonumber(redis.call("HGET", stateKey, "startPriceCents") or "0")

if increment <= 0 then
  return {0, amount, current, status, endAt, 0, 0, 0, 0, "INVALID_RULE"}
end
if amount < current + increment then
  return {0, amount, current, status, endAt, 0, 0, 0, 0, "BID_TOO_LOW"}
end
if ((amount - startPrice) % increment) ~= 0 then
  return {0, amount, current, status, endAt, 0, 0, 0, 0, "BID_STEP_INVALID"}
end

local finalAmount = amount
local sold = 0
local extended = 0
if capPrice > 0 and finalAmount >= capPrice then
  finalAmount = capPrice
  sold = 1
end

return {1, finalAmount, current, "running", endAt, extended, sold, 0, 0, "OK"}
`)

// runBidLiteScript 执行只读预校验 Lua 脚本。
// 不做状态写入、不设幂等键、不更新排行榜。
func runBidLiteScript(ctx context.Context, client *goredis.Client, auctionID uint64, input BidInput, now time.Time) (bidLuaResult, error) {
	stateKey := fmt.Sprintf("auction:%d:state", auctionID)
	lastBidTsKey := fmt.Sprintf("auction:%d:last_bid_ts:%d", auctionID, input.UserID)
	idemKey := fmt.Sprintf("idem:%d:%s", auctionID, md5Hash(input.IdempotencyKey))

	raw, err := bidLiteScript.Run(ctx, client, []string{stateKey, idemKey, lastBidTsKey},
		strconv.FormatUint(input.UserID, 10),
		strconv.FormatInt(input.AmountCents, 10),
		strconv.FormatInt(now.UnixMilli(), 10),
	).Result()
	if err != nil {
		return bidLuaResult{}, err
	}

	values, ok := raw.([]interface{})
	if !ok || len(values) < 10 {
		return bidLuaResult{}, fmt.Errorf("bid script: unexpected result type %T or length %d", raw, len(values))
	}

	result := bidLuaResult{
		accepted:          luaInt(values[0]) == 1,
		amountCents:       luaInt(values[1]),
		currentPriceCents: luaInt(values[2]),
		status:            luaString(values[3]),
		endAtUnixMilli:    luaInt(values[4]),
		extended:          luaInt(values[5]) == 1,
		sold:              luaInt(values[6]) == 1,
		reserveMet:        luaInt(values[7]) == 1,
		tooFrequent:       luaInt(values[8]) == 1,
		idempotentReplay:  luaString(values[9]) == "IDEMPOTENT_REPLAY",
		code:              luaString(values[9]),
	}
	return result, nil
}

// luaInt 将 Lua 返回值安全转为 int64。
func luaInt(value interface{}) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case string:
		parsed, _ := strconv.ParseInt(typed, 10, 64)
		return parsed
	case []byte:
		parsed, _ := strconv.ParseInt(string(typed), 10, 64)
		return parsed
	}
	return 0
}

// luaString 将 Lua 返回值安全转为 string。
func luaString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	}
	return ""
}

// readCachedBidResult 从 Redis 读取缓存的完整出价响应并返回。
// 缓存 miss 时降级返回一个轻量幂等确认。
func (s *PublicService) readCachedBidResult(ctx context.Context, idemKey string, auctionID, userID uint64) *BidResult {
	cached, err := s.redis.Master.Get(ctx, idemKey).Result()
	if err != nil || cached == "" {
		// 缓存失效 → 降级返回简化的幂等确认
		return &BidResult{
			Accepted:         true,
			IdempotentReplay: true,
			AuctionID:        auctionID,
			UserID:           userID,
		}
	}

	var cachedResult BidResult
	if err := json.Unmarshal([]byte(cached), &cachedResult); err != nil {
		return &BidResult{
			Accepted:         true,
			IdempotentReplay: true,
			AuctionID:        auctionID,
			UserID:           userID,
		}
	}

	cachedResult.IdempotentReplay = true
	return &cachedResult
}

// assembleDuplicateBidResult 在唯一索引冲突时查询 MySQL 已有的出价记录，
// 拼装完整 BidResult 供缓存和返回。
func (s *PublicService) assembleDuplicateBidResult(tx repository.AdminStore, ctx context.Context, auctionID uint64, input BidInput, auction *model.Auction) *BidResult {
	// 查出现有出价记录以获取真实金额
	bids, err := tx.ListAuctionBids(ctx, auctionID, 50)
	if err != nil || len(bids) == 0 {
		// 降级：用 input 中的金额
		return &BidResult{
			Accepted:          true,
			IdempotentReplay:  true,
			AuctionID:         auctionID,
			UserID:            input.UserID,
			AmountCents:       input.AmountCents,
			CurrentPriceCents: auction.CurrentPriceCents,
			Status:            auction.Status,
			EndAt:             auction.EndAt.Format(time.RFC3339Nano),
		}
	}

	// 找到匹配 idempotencyKey 的出价
	var existingBid *model.Bid
	for i := range bids {
		if bids[i].IdempotencyKey == input.IdempotencyKey {
			existingBid = &bids[i]
			break
		}
	}

	amount := input.AmountCents
	if existingBid != nil {
		amount = existingBid.AmountCents
	}

	return &BidResult{
		Accepted:          true,
		IdempotentReplay:  true,
		AuctionID:         auctionID,
		UserID:            input.UserID,
		AmountCents:       amount,
		CurrentPriceCents: auction.CurrentPriceCents,
		Status:            auction.Status,
		EndAt:             auction.EndAt.Format(time.RFC3339Nano),
	}
}

// formatEndAt 格式化结束时间为 RFC3339Nano 字符串。
func formatEndAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339Nano)
}

// md5Hash 计算字符串的 MD5 十六进制摘要，用于生成等幂 key。
func md5Hash(s string) string {
	h := md5.Sum([]byte(s))
	return fmt.Sprintf("%x", h)
}

// settleWithRetry 执行结算并带指定次数重试。
func settleWithRetry(ctx context.Context, auctionID uint64, settle *SettleService, retries int) {
	var lastErr error
	for i := 0; i <= retries; i++ {
		sCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := func() error {
			defer cancel()
			_, e := settle.SettleAuction(sCtx, auctionID)
			return e
		}()
		if err == nil {
			return
		}
		lastErr = err
		log.Printf("[settle] 结算竞拍 %d 失败(第%d次): %v", auctionID, i+1, err)
	}
	log.Printf("[settle] 结算竞拍 %d 重试 %d 次后仍失败: %v", auctionID, retries, lastErr)
}

// ListBuyerOrders 查询买家订单列表，可通过 auctionID 过滤。
func (s *PublicService) ListBuyerOrders(ctx context.Context, buyerID uint64, auctionID uint64) ([]model.Order, error) {
	if buyerID == 0 {
		return nil, fmt.Errorf("%w: buyerId is required", ErrInvalidInput)
	}
	orders, err := s.store.ListBuyerOrders(ctx, buyerID)
	if err != nil {
		return nil, err
	}
	if auctionID > 0 {
		filtered := make([]model.Order, 0, len(orders))
		for _, o := range orders {
			if o.AuctionID == auctionID {
				filtered = append(filtered, o)
			}
		}
		return filtered, nil
	}
	return orders, nil
}

// GetBuyerOrder 查询买家单个订单详情。
func (s *PublicService) GetBuyerOrder(ctx context.Context, id uint64, userID uint64) (*model.Order, error) {
	if id == 0 {
		return nil, fmt.Errorf("%w: orderId is required", ErrInvalidInput)
	}
	order, err := s.store.GetOrder(ctx, id)
	if err != nil {
		return nil, err
	}
	if order.BuyerID != userID {
		return nil, fmt.Errorf("%w: order does not belong to user", ErrNotFound)
	}
	return order, nil
}

// PayBuyerOrder 买家模拟支付订单。
func (s *PublicService) PayBuyerOrder(ctx context.Context, orderID uint64, userID uint64, input PayOrderInput) (*model.Order, error) {
	if orderID == 0 {
		return nil, fmt.Errorf("%w: orderId is required", ErrInvalidInput)
	}
	if s.settle == nil {
		return nil, errors.New("settle service unavailable")
	}
	// 校验订单归属
	order, err := s.store.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if order.BuyerID != userID {
		return nil, fmt.Errorf("%w: order does not belong to user", ErrNotFound)
	}
	return s.settle.PayOrder(ctx, orderID, input)
}

// uuid 生成一个简单的 v4 风格 UUID（无横线，32 位 hex），用于事件去重。
func uuid() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		log.Fatalf("uuid generation failed: %v", err)
	}
	// v4 UUID: 第 7 字节的高 4 位为 4，第 9 字节的高 2 位为 10
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x%x%x%x%x%x%x%x%x%x%x%x%x%x%x%x",
		b[0], b[1], b[2], b[3], b[4], b[5], b[6], b[7],
		b[8], b[9], b[10], b[11], b[12], b[13], b[14], b[15])
}
