package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
)

type publicStoreStub struct {
	rooms    map[uint64]*model.LiveRoom
	auctions map[uint64]*model.Auction
	bids     []model.Bid
}

// newPublicStoreStub 创建用户侧内存仓储，供 PublicService 单元测试隔离数据库使用。
func newPublicStoreStub() *publicStoreStub {
	return &publicStoreStub{
		rooms:    make(map[uint64]*model.LiveRoom),
		auctions: make(map[uint64]*model.Auction),
	}
}

// GetRoom 从内存仓储中查询直播间，不存在时模拟 GORM 未找到错误。
func (s *publicStoreStub) GetRoom(_ context.Context, id uint64) (*model.LiveRoom, error) {
	room, ok := s.rooms[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *room
	return &cp, nil
}

// GetAuction 从内存仓储中查询竞拍，不存在时模拟 GORM 未找到错误。
func (s *publicStoreStub) GetAuction(_ context.Context, id uint64) (*model.Auction, error) {
	auction, ok := s.auctions[id]
	if !ok {
		return nil, gorm.ErrRecordNotFound
	}
	cp := *auction
	return &cp, nil
}

// ListRoomAuctions 从内存仓储中查询指定直播间竞拍，并支持状态过滤。
func (s *publicStoreStub) ListRoomAuctions(_ context.Context, roomID uint64, status string) ([]model.Auction, error) {
	auctions := make([]model.Auction, 0, len(s.auctions))
	for _, auction := range s.auctions {
		if auction.RoomID != roomID {
			continue
		}
		if status != "" && auction.Status != status {
			continue
		}
		auctions = append(auctions, *auction)
	}
	return auctions, nil
}

// CreateBid 在内存仓储中追加一条出价记录。
func (s *publicStoreStub) CreateBid(_ context.Context, bid *model.Bid) error {
	s.bids = append(s.bids, *bid)
	return nil
}

// UpdateAuctionBidState 在内存仓储中更新竞拍出价快照。
func (s *publicStoreStub) UpdateAuctionBidState(_ context.Context, auction *model.Auction) error {
	cp := *auction
	s.auctions[auction.ID] = &cp
	return nil
}

// ListAuctionBids 从内存仓储中返回指定竞拍的有效出价记录。
func (s *publicStoreStub) ListLiveRooms(_ context.Context) ([]model.LiveRoom, error) {
	rooms := make([]model.LiveRoom, 0, len(s.rooms))
	for _, r := range s.rooms {
		if r.Status == "live" {
			rooms = append(rooms, *r)
		}
	}
	return rooms, nil
}

func (s *publicStoreStub) ListBuyerOrders(_ context.Context, buyerID uint64) ([]model.Order, error) {
	return []model.Order{}, nil
}

func (s *publicStoreStub) GetOrder(_ context.Context, id uint64) (*model.Order, error) {
	return nil, gorm.ErrRecordNotFound
}

func (s *publicStoreStub) ListAuctionBids(_ context.Context, auctionID uint64, limit int) ([]model.Bid, error) {
	bids := make([]model.Bid, 0, len(s.bids))
	for _, bid := range s.bids {
		if bid.AuctionID == auctionID && bid.Accepted {
			bids = append(bids, bid)
		}
	}
	if limit > 0 && len(bids) > limit {
		return bids[:limit], nil
	}
	return bids, nil
}

// TestPublicServiceGetRoomNotFound 验证直播间不存在时转换为服务层 ErrNotFound。
func TestPublicServiceGetRoomNotFound(t *testing.T) {
	svc := NewPublicService(newPublicStoreStub(), nil, nil, nil, nil)

	_, err := svc.GetRoom(context.Background(), 404)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// TestPublicServiceListRoomAuctionsFiltersStatus 验证直播间竞拍列表能按状态过滤。
func TestPublicServiceListRoomAuctionsFiltersStatus(t *testing.T) {
	store := newPublicStoreStub()
	store.auctions[1] = &model.Auction{ID: 1, RoomID: 10, Status: "running"}
	store.auctions[2] = &model.Auction{ID: 2, RoomID: 10, Status: "scheduled"}
	store.auctions[3] = &model.Auction{ID: 3, RoomID: 20, Status: "running"}
	svc := NewPublicService(store, nil, nil, nil, nil)

	auctions, err := svc.ListRoomAuctions(context.Background(), 10, "running")
	if err != nil {
		t.Fatalf("ListRoomAuctions() error = %v", err)
	}
	if len(auctions) != 1 || auctions[0].ID != 1 {
		t.Fatalf("unexpected auctions: %+v", auctions)
	}
}

// TestPublicServiceRankingFallsBackToDB 验证 Redis 不可用时排行榜使用数据库出价记录兜底。
func TestPublicServiceRankingFallsBackToDB(t *testing.T) {
	store := newPublicStoreStub()
	store.bids = []model.Bid{
		{AuctionID: 1, UserID: 7, AmountCents: 300, Accepted: true},
		{AuctionID: 1, UserID: 8, AmountCents: 200, Accepted: true},
		{AuctionID: 2, UserID: 9, AmountCents: 999, Accepted: true},
	}
	svc := NewPublicService(store, nil, nil, nil, nil)

	ranking, err := svc.GetRanking(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("GetRanking() error = %v", err)
	}
	if len(ranking) != 2 || ranking[0].UserID != 7 || ranking[0].AmountCents != 300 {
		t.Fatalf("unexpected ranking: %+v", ranking)
	}
}

// TestPublicServicePlaceBidRequiresRedis 验证没有 Redis 时出价走 MySQL 完整路径。
// 由于 stub 中没有对应竞拍，应该返回竞拍不存在。
func TestPublicServicePlaceBidRequiresRedis(t *testing.T) {
	store := newPublicStoreStub()
	svc := NewPublicService(store, newAdminStoreStub(), nil, nil, nil)

	_, err := svc.PlaceBid(context.Background(), 1, BidInput{
		UserID:         10,
		AmountCents:    100,
		IdempotencyKey: "idem-1",
	})
	if err == nil {
		t.Fatal("expected error for non-existent auction, got nil")
	}
}

// TestValidateBidInput 验证出价输入的基础字段校验。
func TestValidateBidInput(t *testing.T) {
	tests := []struct {
		name      string
		auctionID uint64
		input     BidInput
		wantErr   bool
	}{
		{
			name:      "valid input",
			auctionID: 1,
			input:     BidInput{UserID: 1, AmountCents: 100, IdempotencyKey: "ok"},
		},
		{
			name:      "missing user",
			auctionID: 1,
			input:     BidInput{AmountCents: 100, IdempotencyKey: "ok"},
			wantErr:   true,
		},
		{
			name:      "non-positive amount",
			auctionID: 1,
			input:     BidInput{UserID: 1, AmountCents: 0, IdempotencyKey: "ok"},
			wantErr:   true,
		},
		{
			name:      "missing idempotency key",
			auctionID: 1,
			input:     BidInput{UserID: 1, AmountCents: 100},
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBidInput(tt.auctionID, tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateBidInput() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestBidLuaResultToBidResult 验证 Lua 结果结构能转换为稳定 API 响应。
func TestBidLuaResultToBidResult(t *testing.T) {
	endAt := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC).UnixMilli()
	result := bidLuaResult{
		accepted:          true,
		amountCents:       500,
		currentPriceCents: 500,
		status:            "running",
		endAtUnixMilli:    endAt,
		extended:          true,
		reserveMet:        true,
		code:              "IDEMPOTENT_REPLAY",
	}.toBidResult(1, 9)

	if !result.Accepted || !result.Extended || !result.ReserveMet {
		t.Fatalf("unexpected bid result flags: %+v", result)
	}
	if result.AuctionID != 1 || result.UserID != 9 || result.CurrentPriceCents != 500 {
		t.Fatalf("unexpected bid result values: %+v", result)
	}
}

// TestBidTooFrequentRejectMessage 验证出价过于频繁时的中文拒绝提示。
func TestBidTooFrequentRejectMessage(t *testing.T) {
	msg := bidRejectMessage("BID_TOO_FREQUENT")
	if msg == "" || msg == "出价被拒绝" {
		t.Fatalf("expected specific reject message for BID_TOO_FREQUENT, got: %q", msg)
	}
}

// TestBidLuaResultTooFrequent 验证 tooFrequent 标记能在 toBidResult 中正确传递。
func TestBidLuaResultTooFrequent(t *testing.T) {
	result := bidLuaResult{
		accepted:          false,
		amountCents:       100,
		currentPriceCents: 50,
		status:            "running",
		endAtUnixMilli:    0,
		tooFrequent:       true,
		code:              "BID_TOO_FREQUENT",
	}.toBidResult(1, 9)

	if result.Accepted {
		t.Fatal("expected rejected bid")
	}
	if !result.TooFrequent {
		t.Fatal("expected tooFrequent flag to be true")
	}
}

// TestBidLuaResultIdempotentReplayFlags 验证幂等重放时各种标记正确。
// 注意：在新架构中 toBidResult 不再设置 IdempotentReplay（由 MySQL 唯一索引负责幂等）。
func TestBidLuaResultIdempotentReplayFlags(t *testing.T) {
	result := bidLuaResult{
		accepted:          true,
		amountCents:       500,
		currentPriceCents: 500,
		status:            "running",
		endAtUnixMilli:    time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC).UnixMilli(),
		extended:          false,
		sold:              false,
		reserveMet:        true,
		code:              "IDEMPOTENT_REPLAY",
	}.toBidResult(1, 9)

	if !result.ReserveMet {
		t.Fatal("expected reserveMet to be preserved")
	}
	if result.Extended {
		t.Fatal("expected extended to be false on idempotent replay")
	}
}

// TestBidLuaResultCapPriceSold 验证封顶价成交时 sold 标记正确。
func TestBidLuaResultCapPriceSold(t *testing.T) {
	result := bidLuaResult{
		accepted:          true,
		amountCents:       1000,
		currentPriceCents: 1000,
		status:            "sold",
		endAtUnixMilli:    time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC).UnixMilli(),
		sold:              true,
		code:              "OK",
	}.toBidResult(1, 9)

	if !result.Sold {
		t.Fatal("expected sold flag to be true")
	}
	if result.Status != "sold" {
		t.Fatalf("expected status 'sold', got %q", result.Status)
	}
}

// TestBidRejectMessageFallback 验证未识别的拒绝码返回通用提示。
func TestBidRejectMessageFallback(t *testing.T) {
	msg := bidRejectMessage("UNKNOWN_REJECT_CODE_XYZ")
	if msg != "出价被拒绝" {
		t.Fatalf("expected fallback message, got: %q", msg)
	}
}

// TestBidRejectMessageKnownCodes 验证所有已知拒绝码都有对应的中文提示，而不是 fallback。
func TestBidRejectMessageKnownCodes(t *testing.T) {
	knownCodes := []string{
		"AUCTION_CACHE_MISSING",
		"AUCTION_NOT_RUNNING",
		"AUCTION_ENDED",
		"BID_TOO_LOW",
		"BID_STEP_INVALID",
		"INVALID_RULE",
		"BID_TOO_FREQUENT",
	}
	for _, code := range knownCodes {
		msg := bidRejectMessage(code)
		if msg == "出价被拒绝" {
			t.Errorf("code %q returned fallback message, expected specific one", code)
		}
	}
}

// TestValidateBidInputIdempotencyKeyTooLong 验证幂等键超长时拒绝。
func TestValidateBidInputIdempotencyKeyTooLong(t *testing.T) {
	longKey := ""
	for i := 0; i < 150; i++ {
		longKey += "a"
	}
	err := validateBidInput(1, BidInput{UserID: 1, AmountCents: 100, IdempotencyKey: longKey})
	if err == nil {
		t.Fatal("expected error for idempotency key exceeding 128 chars")
	}
}

// TestLuaIntParsing 验证 luaInt 能正确解析不同 Redis 返回类型。
func TestLuaIntParsing(t *testing.T) {
	tests := []struct {
		input interface{}
		want  int64
	}{
		{int64(42), 42},
		{string("42"), 42},
		{[]byte("42"), 42},
		{int64(0), 0},
		{string(""), 0},
		{nil, 0},
		{float64(3.14), 0},
	}
	for _, tt := range tests {
		got := luaInt(tt.input)
		if got != tt.want {
			t.Errorf("luaInt(%v) = %d, want %d", tt.input, got, tt.want)
		}
	}
}
