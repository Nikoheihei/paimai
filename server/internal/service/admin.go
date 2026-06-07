package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
	"paimai/internal/statemachine"
	redisclient "paimai/pkg/redis"
)

const (
	AuctionModeSuddenDeath = "sudden_death"
	AuctionModeExtension   = "extension"
	AuctionModeReserve     = "reserve"
)

var (
	ErrInvalidInput       = errors.New("invalid input")
	ErrNotFound           = errors.New("not found")
	ErrInvalidTransition  = errors.New("invalid auction transition")
	ErrAuctionNotEditable = errors.New("auction is not editable")
	ErrUnauthorized       = errors.New("unauthorized")
)

// AdminService 聚合后台管理侧的业务能力。
//
// 这里刻意只依赖 repository.AdminStore 接口，而不是直接依赖 GORM：
// 1. 服务层只关心“商品/竞拍如何流转”，不关心数据具体存到哪里，保持高内聚。
// 2. 单元测试可以注入内存版 store，避免测试依赖 MySQL，降低耦合。
// 3. 后续如果切换为 sqlc 或拆分读写库，只需要新增 repository 实现。
type AdminService struct {
	store repository.AdminStore
	redis *redisclient.Clients
	now   func() time.Time
}

// NewAdminService 创建后台管理服务，并注入数据访问对象和可选 Redis 客户端。
func NewAdminService(store repository.AdminStore, redisClients *redisclient.Clients) *AdminService {
	return &AdminService{
		store: store,
		redis: redisClients,
		now:   time.Now,
	}
}

// ProductInput 是创建商品的输入参数。
type ProductInput struct {
	SellerID    uint64 `json:"sellerId"`
	Name        string `json:"name"`
	ImageURL    string `json:"imageUrl"`
	Description string `json:"description"`
}

// AuctionInput 是创建竞拍的输入参数。
//
// 金额统一使用“分”为单位，避免浮点数比较带来的精度问题。
// StartPriceCents / BidIncrementCents / CapPriceCents 会在服务层统一校验，
// handler 只负责 JSON 绑定，不承载业务规则。
type AuctionInput struct {
	RoomID             uint64     `json:"roomId"`
	ProductID          uint64     `json:"productId"`
	Mode               string     `json:"mode"`
	StartPriceCents    int64      `json:"startPriceCents"`
	BidIncrementCents  int64      `json:"bidIncrementCents"`
	CapPriceCents      int64      `json:"capPriceCents"`
	ReservePriceCents  *int64     `json:"reservePriceCents"`
	ExtendThresholdSec int        `json:"extendThresholdSec"`
	ExtendDurationSec  int        `json:"extendDurationSec"`
	StartAt            *time.Time `json:"startAt"`
	EndAt              *time.Time `json:"endAt"`
}

// AuctionPatchInput 是修改未开始竞拍规则的输入参数。
//
// 指针字段用于区分“调用方没有传这个字段”和“调用方显式传了 0”。
// 例如 CapPriceCents 传 0 表示清除封顶价，而 nil 表示不修改封顶价。
type AuctionPatchInput struct {
	Mode               *string    `json:"mode"`
	StartPriceCents    *int64     `json:"startPriceCents"`
	BidIncrementCents  *int64     `json:"bidIncrementCents"`
	CapPriceCents      *int64     `json:"capPriceCents"`
	ReservePriceCents  *int64     `json:"reservePriceCents"`
	ClearReservePrice  bool       `json:"clearReservePrice"`
	ExtendThresholdSec *int       `json:"extendThresholdSec"`
	ExtendDurationSec  *int       `json:"extendDurationSec"`
	StartAt            *time.Time `json:"startAt"`
	EndAt              *time.Time `json:"endAt"`
}

// StartAuctionInput 是开始竞拍时的可选参数。
type StartAuctionInput struct {
	DurationSec int `json:"durationSec"`
}

// CancelAuctionInput 是主播/商家取消竞拍时的输入参数。
type CancelAuctionInput struct {
	Reason string `json:"reason"`
}

// CreateProduct 校验商品创建输入，并委托 repository 写入商品数据。
// 商品创建成功后通过 Outbox 事件通知所有在线客户端刷新商品列表。
func (s *AdminService) CreateProduct(ctx context.Context, sellerID uint64, input ProductInput) (*model.Product, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, fmt.Errorf("%w: product name is required", ErrInvalidInput)
	}

	product := &model.Product{
		SellerID:    sellerID,
		Name:        input.Name,
		ImageURL:    strings.TrimSpace(input.ImageURL),
		Description: strings.TrimSpace(input.Description),
	}

	// 使用事务：创建商品 + 写入 Outbox 事件
	var created *model.Product
	if err := s.store.WithTx(ctx, func(tx repository.AdminStore) error {
		if err := tx.CreateProduct(ctx, product); err != nil {
			return err
		}
		created = product

		// 写入 Outbox 事件，通知前端刷新商品列表
		eventPayload, _ := json.Marshal(map[string]interface{}{
			"type":        "product.created",
			"productId":   product.ID,
			"sellerId":    product.SellerID,
			"name":        product.Name,
			"imageUrl":    product.ImageURL,
			"description": product.Description,
		})
		return tx.CreateOutboxEvent(ctx, &model.OutboxEvent{
			EventType: "product.created",
			Payload:   string(eventPayload),
			Status:    "pending",
			EventUUID: fmt.Sprintf("product-%d", product.ID),
		})
	}); err != nil {
		return nil, err
	}
	return created, nil
}

// ListProducts 查询后台商品列表，可按卖家 ID 做过滤。
func (s *AdminService) ListProducts(ctx context.Context, sellerID *uint64) ([]model.Product, error) {
	return s.store.ListProducts(ctx, sellerID)
}

// CreateAuction 校验竞拍规则并创建 draft 状态的竞拍草稿。
func (s *AdminService) CreateAuction(ctx context.Context, input AuctionInput) (*model.Auction, error) {
	if input.RoomID == 0 || input.ProductID == 0 {
		return nil, fmt.Errorf("%w: roomId and productId are required", ErrInvalidInput)
	}
	if _, err := s.store.GetProduct(ctx, input.ProductID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if err := validateAuctionRules(input.Mode, input.StartPriceCents, input.BidIncrementCents, input.CapPriceCents, input.ReservePriceCents, input.ExtendThresholdSec, input.ExtendDurationSec); err != nil {
		return nil, err
	}

	mode := normalizedMode(input.Mode)
	now := time.Now()
	var startAt, endAt time.Time
	if input.StartAt != nil && !input.StartAt.IsZero() {
		startAt = *input.StartAt
	} else {
		startAt = now
	}
	if input.EndAt != nil && !input.EndAt.IsZero() && !input.EndAt.Before(startAt) {
		endAt = *input.EndAt
	} else {
		endAt = startAt.Add(5 * time.Minute)
	}
	auction := &model.Auction{
		RoomID:             input.RoomID,
		ProductID:          input.ProductID,
		Mode:               mode,
		StartPriceCents:    input.StartPriceCents,
		CurrentPriceCents:  input.StartPriceCents,
		BidIncrementCents:  input.BidIncrementCents,
		CapPriceCents:      input.CapPriceCents,
		ReservePriceCents:  input.ReservePriceCents,
		ExtendThresholdSec: input.ExtendThresholdSec,
		ExtendDurationSec:  input.ExtendDurationSec,
		Status:             string(statemachine.StateDraft),
		StartAt:            startAt,
		EndAt:              endAt,
	}
	if err := s.store.CreateAuction(ctx, auction); err != nil {
		return nil, err
	}
	s.enqueueAuctionRefresh(ctx, "auction.created", auction)
	return auction, nil
}

// UpdateAuction 修改 draft 或 scheduled 状态下的竞拍规则。
func (s *AdminService) UpdateAuction(ctx context.Context, id uint64, input AuctionPatchInput) (*model.Auction, error) {
	auction, err := s.getAuction(ctx, id)
	if err != nil {
		return nil, err
	}
	if auction.Status != string(statemachine.StateDraft) && auction.Status != string(statemachine.StateScheduled) {
		return nil, ErrAuctionNotEditable
	}

	if input.Mode != nil {
		auction.Mode = normalizedMode(*input.Mode)
	}
	if input.StartPriceCents != nil {
		auction.StartPriceCents = *input.StartPriceCents
		auction.CurrentPriceCents = *input.StartPriceCents
	}
	if input.BidIncrementCents != nil {
		auction.BidIncrementCents = *input.BidIncrementCents
	}
	if input.CapPriceCents != nil {
		auction.CapPriceCents = *input.CapPriceCents
	}
	if input.ClearReservePrice {
		auction.ReservePriceCents = nil
	} else if input.ReservePriceCents != nil {
		auction.ReservePriceCents = input.ReservePriceCents
	}
	if input.ExtendThresholdSec != nil {
		auction.ExtendThresholdSec = *input.ExtendThresholdSec
	}
	if input.ExtendDurationSec != nil {
		auction.ExtendDurationSec = *input.ExtendDurationSec
	}
	if input.StartAt != nil && !input.StartAt.IsZero() {
		auction.StartAt = *input.StartAt
	}
	if input.EndAt != nil && !input.EndAt.IsZero() {
		if !input.EndAt.After(auction.StartAt) {
			return nil, fmt.Errorf("%w: endAt must be after startAt", ErrInvalidInput)
		}
		auction.EndAt = *input.EndAt
	}

	if err := validateAuctionRules(auction.Mode, auction.StartPriceCents, auction.BidIncrementCents, auction.CapPriceCents, auction.ReservePriceCents, auction.ExtendThresholdSec, auction.ExtendDurationSec); err != nil {
		return nil, err
	}
	if err := s.store.UpdateAuction(ctx, auction); err != nil {
		return nil, err
	}
	s.enqueueAuctionRefresh(ctx, "auction.updated", auction)
	return auction, nil
}

// PublishAuction 将竞拍从 draft 状态发布为 scheduled 状态。
func (s *AdminService) PublishAuction(ctx context.Context, id uint64) (*model.Auction, error) {
	return s.transitionAuction(ctx, id, statemachine.EventPublish, "")
}

// StartAuction 将 scheduled 状态竞拍启动为 running，并初始化开始/结束时间。
func (s *AdminService) StartAuction(ctx context.Context, id uint64, input StartAuctionInput) (*model.Auction, error) {
	auction, err := s.getAuction(ctx, id)
	if err != nil {
		return nil, err
	}
	next, err := transition(auction.Status, statemachine.EventStart)
	if err != nil {
		return nil, err
	}

	duration := time.Duration(input.DurationSec) * time.Second
	if duration <= 0 {
		duration = 60 * time.Second
	}
	now := s.now()
	auction.Status = string(next)
	auction.StartAt = now
	auction.EndAt = now.Add(duration)
	auction.CurrentPriceCents = auction.StartPriceCents
	auction.WinnerUserID = nil
	auction.CancelReason = ""

	if err := s.store.UpdateAuction(ctx, auction); err != nil {
		return nil, err
	}
	s.initAuctionCache(ctx, auction)
	s.enqueueAuctionRefresh(ctx, "auction.updated", auction)
	return auction, nil
}

// CancelAuction 根据状态机约束取消竞拍，并记录取消原因。
func (s *AdminService) CancelAuction(ctx context.Context, id uint64, input CancelAuctionInput) (*model.Auction, error) {
	return s.transitionAuction(ctx, id, statemachine.EventCancel, strings.TrimSpace(input.Reason))
}

// ListAuctions 查询后台竞拍列表，可按直播间和状态过滤。
func (s *AdminService) ListAuctions(ctx context.Context, filter repository.AuctionFilter) ([]model.Auction, error) {
	return s.store.ListAuctions(ctx, filter)
}

// StartDueScheduledAuctions 将已到预约时间的上架计划自动启动为 running。
func (s *AdminService) StartDueScheduledAuctions(ctx context.Context) (int, error) {
	auctions, err := s.store.ListAuctions(ctx, repository.AuctionFilter{Status: string(statemachine.StateScheduled)})
	if err != nil {
		return 0, err
	}

	now := s.now()
	started := 0
	for _, auction := range auctions {
		if auction.StartAt.After(now) {
			continue
		}
		durationSec := int(auction.EndAt.Sub(auction.StartAt).Seconds())
		if durationSec <= 0 {
			durationSec = 300
		}
		if _, err := s.StartAuction(ctx, auction.ID, StartAuctionInput{DurationSec: durationSec}); err != nil {
			if errors.Is(err, ErrInvalidTransition) {
				continue
			}
			return started, err
		}
		started++
	}
	return started, nil
}

// ListAuctionBids 查询指定竞拍的出价历史（按金额降序）。
func (s *AdminService) ListAuctionBids(ctx context.Context, auctionID uint64) ([]model.Bid, error) {
	_, err := s.store.GetAuction(ctx, auctionID)
	if err != nil {
		return nil, err
	}
	return s.store.ListAuctionBids(ctx, auctionID, 50)
}

// transitionAuction 封装竞拍状态流转、取消原因写入和持久化更新。
func (s *AdminService) transitionAuction(ctx context.Context, id uint64, event statemachine.Event, reason string) (*model.Auction, error) {
	auction, err := s.getAuction(ctx, id)
	if err != nil {
		return nil, err
	}
	next, err := transition(auction.Status, event)
	if err != nil {
		return nil, err
	}
	auction.Status = string(next)
	if event == statemachine.EventCancel {
		auction.CancelReason = reason
	}
	if err := s.store.UpdateAuction(ctx, auction); err != nil {
		return nil, err
	}
	s.enqueueAuctionRefresh(ctx, "auction.updated", auction)
	return auction, nil
}

// getAuction 查询竞拍记录，并把 GORM 的未找到错误转换成服务层稳定错误。
func (s *AdminService) getAuction(ctx context.Context, id uint64) (*model.Auction, error) {
	auction, err := s.store.GetAuction(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return auction, nil
}

func (s *AdminService) enqueueAuctionRefresh(ctx context.Context, eventType string, auction *model.Auction) {
	eventID := fmt.Sprintf("%s-%d-%d", eventType, auction.ID, time.Now().UnixNano())
	eventPayload, _ := json.Marshal(map[string]interface{}{
		"type":      eventType,
		"eventId":   eventID,
		"auctionId": auction.ID,
		"roomId":    auction.RoomID,
		"productId": auction.ProductID,
		"status":    auction.Status,
	})
	if err := s.store.CreateOutboxEvent(ctx, &model.OutboxEvent{
		EventType: eventType,
		Payload:   string(eventPayload),
		Status:    "pending",
		EventUUID: eventID,
	}); err != nil {
		log.Printf("[admin] enqueue auction refresh failed (auction=%d type=%s): %v", auction.ID, eventType, err)
	}
}

// initAuctionCache 在竞拍启动时把权威快照写入 Redis 热数据。
func (s *AdminService) initAuctionCache(ctx context.Context, auction *model.Auction) error {
	if s.redis == nil || s.redis.Master == nil {
		return nil
	}
	reservePrice := int64(0)
	if auction.ReservePriceCents != nil {
		reservePrice = *auction.ReservePriceCents
	}
	key := fmt.Sprintf("auction:%d:state", auction.ID)
	if err := s.redis.Master.HSet(ctx, key, map[string]interface{}{
		"status":             auction.Status,
		"mode":               auction.Mode,
		"currentPriceCents":  auction.CurrentPriceCents,
		"leaderUserId":       "",
		"startPriceCents":    auction.StartPriceCents,
		"bidIncrementCents":  auction.BidIncrementCents,
		"capPriceCents":      auction.CapPriceCents,
		"reservePriceCents":  reservePrice,
		"endAtUnixMilli":     auction.EndAt.UnixMilli(),
		"extendThresholdSec": auction.ExtendThresholdSec,
		"extendDurationSec":  auction.ExtendDurationSec,
	}).Err(); err != nil {
		log.Printf("[admin] initAuctionCache 失败 (auction=%d): %v — 不影响竞拍启动", auction.ID, err)
		return nil
	}
	if err := s.redis.Master.Expire(ctx, key, 24*time.Hour).Err(); err != nil {
		log.Printf("[admin] initAuctionCache expire 失败 (auction=%d): %v", auction.ID, err)
	}
	return nil
}

// transition 是状态机的服务层适配函数。
//
// 业务代码不直接写 auction.Status = "running" 之类的状态变更，
// 而是统一通过 statemachine.Machine 校验迁移是否合法。
// 这样可以把状态流转约束集中在一个地方，避免接口越多越容易出现“绕过规则”的状态写入。
func transition(status string, event statemachine.Event) (statemachine.State, error) {
	machine := statemachine.NewMachine(statemachine.State(status))
	next, err := machine.Transition(event)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidTransition, err)
	}
	return next, nil
}

type auctionRuleInput struct {
	mode            string
	startPrice      int64
	increment       int64
	capPrice        int64
	reservePrice    *int64
	extendThreshold int
	extendDuration  int
}

type auctionModeRule struct {
	validate func(auctionRuleInput) error
}

// auctionModeRules 是竞拍模式的扩展点。
//
// 公共价格规则放在 validateCommonAuctionRules 中；各模式只声明自己的额外约束。
// 后续新增模式时，优先新增一个 validateXXXRules 函数并注册到这个 map，
// 避免不断修改一大段 if/else，符合“对扩展开放、对修改关闭”的目标。
var auctionModeRules = map[string]auctionModeRule{
	AuctionModeSuddenDeath: {validate: validateNoExtensionRules},
	AuctionModeExtension:   {validate: validateExtensionRules},
	AuctionModeReserve:     {validate: validateReserveRules},
}

// validateAuctionRules 统一校验竞拍规则。
//
// 这个函数只负责组织校验顺序：
// 1. 先处理默认模式与公共价格合法性。
// 2. 再根据 mode 分发到具体模式规则。
// 3. 返回带 ErrInvalidInput 包装的错误，方便 handler 映射为 HTTP 400。
func validateAuctionRules(mode string, startPrice, increment, capPrice int64, reservePrice *int64, extendThreshold, extendDuration int) error {
	mode = normalizedMode(mode)
	input := auctionRuleInput{
		mode:            mode,
		startPrice:      startPrice,
		increment:       increment,
		capPrice:        capPrice,
		reservePrice:    reservePrice,
		extendThreshold: extendThreshold,
		extendDuration:  extendDuration,
	}

	rule, ok := auctionModeRules[mode]
	if !ok {
		return fmt.Errorf("%w: unsupported auction mode", ErrInvalidInput)
	}
	if err := validateCommonAuctionRules(input); err != nil {
		return err
	}
	return rule.validate(input)
}

// validateCommonAuctionRules 校验所有竞拍模式共享的价格规则。
func validateCommonAuctionRules(input auctionRuleInput) error {
	if input.startPrice < 0 || input.increment <= 0 || input.capPrice < 0 {
		return fmt.Errorf("%w: price fields are invalid", ErrInvalidInput)
	}
	if input.capPrice > 0 && input.capPrice < input.startPrice+input.increment {
		return fmt.Errorf("%w: capPriceCents must be at least one increment above startPriceCents", ErrInvalidInput)
	}
	if input.reservePrice != nil && *input.reservePrice < 0 {
		return fmt.Errorf("%w: reservePriceCents cannot be negative", ErrInvalidInput)
	}
	return nil
}

// validateNoExtensionRules 校验不支持自动延时模式下的延时参数。
func validateNoExtensionRules(input auctionRuleInput) error {
	if input.extendThreshold < 0 || input.extendDuration < 0 {
		return fmt.Errorf("%w: extension settings cannot be negative", ErrInvalidInput)
	}
	return nil
}

// validateExtensionRules 校验自动延时模式必须提供正数延时窗口和延时时长。
func validateExtensionRules(input auctionRuleInput) error {
	if input.extendThreshold <= 0 || input.extendDuration <= 0 {
		return fmt.Errorf("%w: extension mode requires positive extendThresholdSec and extendDurationSec", ErrInvalidInput)
	}
	return nil
}

// ptrIntToValue 将 *int 转为 int，nil 时返回 0。
func ptrIntToValue(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// validateReserveRules 校验保留价模式必须配置非空保留价。
func validateReserveRules(input auctionRuleInput) error {
	if input.reservePrice == nil {
		return fmt.Errorf("%w: reserve mode requires reservePriceCents", ErrInvalidInput)
	}
	return validateNoExtensionRules(input)
}

// normalizedMode 规范化竞拍模式，空值按默认绝杀模式处理。
func normalizedMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return AuctionModeSuddenDeath
	}
	return mode
}

// GetProduct 查询商品详情。
func (s *AdminService) GetProduct(ctx context.Context, id uint64) (*model.Product, error) {
	return s.store.GetProduct(ctx, id)
}

// UpdateProductInput 是编辑商品的输入参数。
type UpdateProductInput struct {
	Name        string `json:"name"`
	ImageURL    string `json:"imageUrl"`
	Description string `json:"description"`
}

// UpdateProduct 编辑商品信息。
func (s *AdminService) UpdateProduct(ctx context.Context, id uint64, input UpdateProductInput) (*model.Product, error) {
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		return nil, fmt.Errorf("%w: product name is required", ErrInvalidInput)
	}
	product, err := s.store.GetProduct(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	product.Name = input.Name
	product.ImageURL = strings.TrimSpace(input.ImageURL)
	product.Description = strings.TrimSpace(input.Description)
	if err := s.store.UpdateProduct(ctx, product); err != nil {
		return nil, err
	}
	return product, nil
}

// DeleteProduct 删除商品。有关联活跃竞拍（draft/scheduled/running）时拒绝。
func (s *AdminService) DeleteProduct(ctx context.Context, id uint64) error {
	// 查询该商品的所有竞拍
	auctions, err := s.store.ListAuctions(ctx, repository.AuctionFilter{})
	if err != nil {
		return err
	}
	for _, a := range auctions {
		if a.ProductID == id && (a.Status == "draft" || a.Status == "scheduled" || a.Status == "running") {
			return fmt.Errorf("%w: product has active auctions", ErrInvalidInput)
		}
	}
	return s.store.DeleteProduct(ctx, id)
}

// GetOrder 查询订单详情。
func (s *AdminService) GetOrder(ctx context.Context, id uint64) (*model.Order, error) {
	order, err := s.store.GetOrder(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return order, nil
}

// ListOrdersBySeller 查询商家所有订单。
func (s *AdminService) ListOrdersBySeller(ctx context.Context, sellerID uint64) ([]model.Order, error) {
	return s.store.ListOrdersBySeller(ctx, sellerID)
}
