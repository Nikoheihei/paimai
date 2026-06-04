package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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

// ErrBidEngineUnavailable 表示当前服务没有可用 Redis 出价引擎，不能接受实时出价。
var ErrBidEngineUnavailable = errors.New("bid engine unavailable")

// BidRejectError 表示出价被业务规则拒绝，Code 用于前端做稳定分支处理。
type BidRejectError struct {
	Code    string
	Message string
}

// Error 返回出价拒绝错误的可读说明。
func (e *BidRejectError) Error() string {
	return e.Message
}

// PublicService 聚合用户端直播间、竞拍详情、排行榜和出价能力。
//
// 查询能力主要读取数据库；实时出价必须依赖 Redis Lua 原子脚本。
// 这样可以把“读详情”和“写出价”的一致性边界分开，避免所有用户端逻辑绑在一个重事务里。
type PublicService struct {
	settle  *SettleService // 结算服务（可选），用于出价时自动结算已过期或已成交竞拍
	store repository.PublicStore
	redis *redisclient.Clients
	stream  *stream.Publisher
	now   func() time.Time
}

// NewPublicService 创建用户端服务，并注入用户侧数据仓储和可选 Redis 客户端。
func NewPublicService(store repository.PublicStore, redisClients *redisclient.Clients, publisher *stream.Publisher, settleService *SettleService) *PublicService {
	return &PublicService{
		store:  store,
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

// RankingItem 表示排行榜中的单个用户出价名次。
type RankingItem struct {
	Rank        int    `json:"rank"`
	UserID      uint64 `json:"userId"`
	AmountCents int64  `json:"amountCents"`
}

// GetRoom 查询直播间详情。
func (s *PublicService) ListLiveRooms(ctx context.Context) ([]model.LiveRoom, error) {
	return s.store.ListLiveRooms(ctx)
}

func (s *PublicService) GetRoom(ctx context.Context, id uint64) (*model.LiveRoom, error) {
	room, err := s.store.GetRoom(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return room, nil
}

// ListRoomAuctions 查询直播间下的竞拍列表，可按状态过滤。
func (s *PublicService) ListRoomAuctions(ctx context.Context, roomID uint64, status string) ([]model.Auction, error) {
	if roomID == 0 {
		return nil, fmt.Errorf("%w: roomId is required", ErrInvalidInput)
	}
	return s.store.ListRoomAuctions(ctx, roomID, strings.TrimSpace(status))
}

// GetAuction 查询竞拍详情。
func (s *PublicService) GetAuction(ctx context.Context, id uint64) (*model.Auction, error) {
	auction, err := s.store.GetAuction(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return auction, nil
}

// GetRanking 查询竞拍排行榜；Redis 可用时读热榜，否则用数据库出价记录兜底。
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
			return items, nil
		}
	}
	return s.rankingFromDB(ctx, auctionID, limit)
}

// PlaceBid 校验出价输入，执行 Redis Lua 原子判定，并把有效出价同步落库。
func (s *PublicService) PlaceBid(ctx context.Context, auctionID uint64, input BidInput) (*BidResult, error) {
	if err := validateBidInput(auctionID, input); err != nil {
		return nil, err
	}
	if s.redis == nil || s.redis.Master == nil {
		return nil, ErrBidEngineUnavailable
	}

	now := s.now()
	luaResult, err := runBidScript(ctx, s.redis.Master, auctionID, input, now)
	if err != nil {
		return nil, err
	}
	result := luaResult.toBidResult(auctionID, input.UserID)
	if !luaResult.accepted {
		// AUCTION_ENDED 特殊处理：竞拍在 Redis 层已过期，异步触发自动结算
		if luaResult.code == "AUCTION_ENDED" && s.settle != nil {
			go func(aID uint64) {
				sCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if _, err := s.settle.SettleAuction(sCtx, aID); err != nil {
					log.Printf("[settle] 出价触发自动结算竞拍 %d 失败: %v", aID, err)
				}
			}(auctionID)
		}
		return result, &BidRejectError{Code: luaResult.code, Message: bidRejectMessage(luaResult.code)}
	}

	if _, err := s.redis.WaitReplicas(ctx, 1, 50*time.Millisecond); err != nil {
		return nil, err
	}
	if result.IdempotentReplay {
		return result, nil
	}
	if err := s.persistAcceptedBid(ctx, auctionID, input, result, now); err != nil {
		return nil, err
	}
	return result, nil
}

// persistAcceptedBid 将 Redis 已接受的出价同步写入 bids，并更新 auctions 的出价快照。
func (s *PublicService) persistAcceptedBid(ctx context.Context, auctionID uint64, input BidInput, result *BidResult, now time.Time) error {
	auction, err := s.GetAuction(ctx, auctionID)
	if err != nil {
		return err
	}
	serverTS := now.UnixMilli()
	clientTS := input.ClientTS
	if clientTS == 0 {
		clientTS = serverTS
	}
	bid := &model.Bid{
		AuctionID:      auctionID,
		UserID:         input.UserID,
		AmountCents:    result.AmountCents,
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		ClientTS:       clientTS,
		ServerTS:       serverTS,
		Accepted:       true,
	}
	if err := s.store.CreateBid(ctx, bid); err != nil {
		return err
	}

	auction.CurrentPriceCents = result.CurrentPriceCents
	auction.WinnerUserID = &input.UserID
	auction.EndAt = time.UnixMilli(mustParseEndAt(result.EndAt))
	auction.Status = result.Status
	// 出价落库后发布事件到 Redis Stream，WebSocket 消费者从此处读取并广播。
	if s.stream != nil {
		payload, _ := json.Marshal(result)
		if pubErr := s.stream.Publish(ctx, stream.Event{
			Type:      "bid.accepted",
			RoomID:    auction.RoomID,
			AuctionID: auctionID,
			Payload:   payload,
		}); pubErr != nil {
			log.Printf("[stream] 发布事件失败: %v", pubErr)
		}
	}
	updateErr := s.store.UpdateAuctionBidState(ctx, auction)
	// 如果本次出价触发成交（sold），自动结算（必须在 UpdateAuctionBidState 之后）
	if result.Sold && s.settle != nil && updateErr == nil {
		if _, settleErr := s.settle.SettleAuction(ctx, auctionID); settleErr != nil {
			log.Printf("[settle] 出价成交自动结算竞拍 %d 失败: %v", auctionID, settleErr)
		}
	}
	return updateErr
}
// rankingFromRedis 从 Redis ZSET 读取排行榜热数据。
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

// rankingFromDB 从数据库出价记录生成兜底排行榜。
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
	return items, nil
}

// validateBidInput 校验出价请求的基础字段，复杂竞价规则交给 Redis Lua 原子脚本处理。
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

type bidLuaResult struct {
	accepted          bool
	amountCents       int64
	currentPriceCents int64
	status            string
	endAtUnixMilli    int64
	extended          bool
	sold              bool
	reserveMet        bool
	tooFrequent       bool              // 出价过于频繁，未达到最小间隔
	code              string
}

// toBidResult 将 Lua 返回的紧凑数组转换为 API 响应结构。
func (r bidLuaResult) toBidResult(auctionID uint64, userID uint64) *BidResult {
	return &BidResult{
		Accepted:          r.accepted,
		AuctionID:         auctionID,
		UserID:            userID,
		AmountCents:       r.amountCents,
		CurrentPriceCents: r.currentPriceCents,
		Status:            r.status,
		EndAt:             time.UnixMilli(r.endAtUnixMilli).Format(time.RFC3339Nano),
		Extended:          r.extended,
		Sold:              r.sold,
		ReserveMet:        r.reserveMet,
		IdempotentReplay:  r.code == "IDEMPOTENT_REPLAY",
		TooFrequent:       r.tooFrequent,
	}
}

var bidScript = goredis.NewScript(`
local stateKey = KEYS[1]
local bidsKey = KEYS[2]
local idemKey = KEYS[3]
local lastBidKey = KEYS[4]

local userId = ARGV[1]
local amount = tonumber(ARGV[2])
local nowMs = tonumber(ARGV[3])

local status = redis.call("HGET", stateKey, "status")
if not status then
  return {0, 0, 0, "", 0, 0, 0, 0, 0, "AUCTION_CACHE_MISSING"}
end

local current = tonumber(redis.call("HGET", stateKey, "currentPriceCents") or "0")
local endAt = tonumber(redis.call("HGET", stateKey, "endAtUnixMilli") or "0")
local reserve = tonumber(redis.call("HGET", stateKey, "reservePriceCents") or "0")

local minIntervalMs = 1000

-- 最小出价间隔检查：同一用户在同一个竞拍中两次出价须至少间隔 minIntervalMs
local lastTs = redis.call("GET", lastBidKey)
if lastTs and tonumber(lastTs) + minIntervalMs > nowMs then
  return {0, amount, current, status, endAt, 0, 0, 0, 1, "BID_TOO_FREQUENT"}
end

if redis.call("EXISTS", idemKey) == 1 then
  local reserveMetReplay = 0
  if reserve == 0 or current >= reserve then
    reserveMetReplay = 1
  end
  return {1, current, current, status, endAt, 0, 0, reserveMetReplay, 0, "IDEMPOTENT_REPLAY"}
end

if status ~= "running" then
  return {0, amount, current, status, endAt, 0, 0, 0, 0, "AUCTION_NOT_RUNNING"}
end
if nowMs >= endAt then
  return {0, amount, current, status, endAt, 0, 0, 0, 0, "AUCTION_ENDED"}
end

local mode = redis.call("HGET", stateKey, "mode") or "sudden_death"
local startPrice = tonumber(redis.call("HGET", stateKey, "startPriceCents") or "0")
local increment = tonumber(redis.call("HGET", stateKey, "bidIncrementCents") or "0")
local capPrice = tonumber(redis.call("HGET", stateKey, "capPriceCents") or "0")
local extendThreshold = tonumber(redis.call("HGET", stateKey, "extendThresholdSec") or "0")
local extendDuration = tonumber(redis.call("HGET", stateKey, "extendDurationSec") or "0")

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
if capPrice > 0 and finalAmount >= capPrice then
  finalAmount = capPrice
  status = "sold"
  sold = 1
end

local extended = 0
if sold == 0 and mode == "extension" and extendThreshold > 0 and extendDuration > 0 and (endAt - nowMs) <= extendThreshold * 1000 then
  endAt = endAt + extendDuration * 1000
  extended = 1
end

local reserveMet = 0
if reserve == 0 or finalAmount >= reserve then
  reserveMet = 1
end

redis.call("HSET", stateKey,
  "status", status,
  "currentPriceCents", finalAmount,
  "leaderUserId", userId,
  "endAtUnixMilli", endAt
)
redis.call("ZADD", bidsKey, finalAmount, userId)
redis.call("EXPIRE", bidsKey, 86400)
redis.call("SET", idemKey, "1", "EX", 600)
-- 记录用户本次出价时间戳用于频率检查
redis.call("SET", lastBidKey, nowMs, "EX", 86400)
redis.call("EXPIRE", lastBidKey, 86400)

return {1, finalAmount, finalAmount, status, endAt, extended, sold, reserveMet, 0, "OK"}
`)

// runBidScript 执行 Redis Lua 出价脚本，并解析脚本返回的紧凑数组。
func runBidScript(ctx context.Context, client *goredis.Client, auctionID uint64, input BidInput, now time.Time) (bidLuaResult, error) {
	stateKey := fmt.Sprintf("auction:%d:state", auctionID)
	bidsKey := fmt.Sprintf("auction:%d:bids", auctionID)
	idemKey := fmt.Sprintf("auction:%d:idem:%s", auctionID, strings.TrimSpace(input.IdempotencyKey))
	lastBidTsKey := fmt.Sprintf("auction:%d:last_bid_ts:%d", auctionID, input.UserID)
	raw, err := bidScript.Run(ctx, client, []string{stateKey, bidsKey, idemKey, lastBidTsKey},
		strconv.FormatUint(input.UserID, 10),
		strconv.FormatInt(input.AmountCents, 10),
		strconv.FormatInt(now.UnixMilli(), 10),
	).Result()
	if err != nil {
		return bidLuaResult{}, err
	}
	values, ok := raw.([]interface{})
	if !ok || len(values) != 10 {
		return bidLuaResult{}, fmt.Errorf("unexpected bid script result: %v", raw)
	}
	return bidLuaResult{
		accepted:          luaInt(values[0]) == 1,
		amountCents:       luaInt(values[1]),
		currentPriceCents: luaInt(values[2]),
		status:            luaString(values[3]),
		endAtUnixMilli:    luaInt(values[4]),
		extended:          luaInt(values[5]) == 1,
		sold:              luaInt(values[6]) == 1,
		reserveMet:        luaInt(values[7]) == 1,
		tooFrequent:       luaInt(values[8]) == 1,
		code:              luaString(values[9]),
	}, nil
}

// luaInt 将 Redis Lua 返回值转换为 int64，兼容整数和字符串两种返回形态。
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
	default:
		return 0
	}
}

// luaString 将 Redis Lua 返回值转换为字符串。
func luaString(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return fmt.Sprint(value)
	}
}

// bidRejectMessage 将稳定拒绝码转换为用户可读的中文提示。
func bidRejectMessage(code string) string {
	switch code {
	case "BID_TOO_FREQUENT":
		return "出价过于频繁，请稍后重试"
	case "AUCTION_CACHE_MISSING":
		return "竞拍尚未初始化或已过期"
	case "AUCTION_NOT_RUNNING":
		return "竞拍当前不在进行中"
	case "AUCTION_ENDED":
		return "竞拍已经结束"
	case "BID_TOO_LOW":
		return "出价低于当前最低有效出价"
	case "BID_STEP_INVALID":
		return "出价不符合加价幅度"
	case "INVALID_RULE":
		return "竞拍规则配置异常"
	default:
		return "出价未被接受"
	}
}

// mustParseEndAt 将 RFC3339Nano 字符串转回毫秒时间戳；内部只处理本服务生成的时间字符串。
func mustParseEndAt(value string) int64 {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return parsed.UnixMilli()
}

// ListBuyerOrders 查询当前用户的订单列表。
func (s *PublicService) ListBuyerOrders(ctx context.Context, buyerID uint64) ([]model.Order, error) {
	if buyerID == 0 {
		return nil, fmt.Errorf("%w: user not authenticated", ErrUnauthorized)
	}
	return s.store.ListBuyerOrders(ctx, buyerID)
}

// GetBuyerOrder 查询当前用户的某个订单详情（校验归属）。
func (s *PublicService) GetBuyerOrder(ctx context.Context, userID uint64, orderID uint64) (*model.Order, error) {
	if userID == 0 {
		return nil, fmt.Errorf("%w: user not authenticated", ErrUnauthorized)
	}
	order, err := s.store.GetOrder(ctx, orderID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	// 校验订单归属，防止越权
	if order.BuyerID != userID {
		return nil, ErrNotFound
	}
	return order, nil
}

// PayBuyerOrder 模拟支付当前用户的订单。
func (s *PublicService) PayBuyerOrder(ctx context.Context, userID uint64, orderID uint64) (*model.Order, error) {
	if userID == 0 {
		return nil, fmt.Errorf("%w: user not authenticated", ErrUnauthorized)
	}
	// 先校验归属
	order, err := s.GetBuyerOrder(ctx, userID, orderID)
	if err != nil {
		return nil, err
	}
	if s.settle == nil {
		return nil, fmt.Errorf("settle service unavailable")
	}
	return s.settle.PayOrder(ctx, order.ID)
}
