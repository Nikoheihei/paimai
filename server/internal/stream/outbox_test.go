package stream

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"errors"
	"paimai/internal/model"
	"paimai/internal/repository"
)

// mockAdminStore 实现 repository.AdminStore 的最小 mock，只覆盖 outbox 相关方法。
type mockAdminStore struct {
	mu     sync.Mutex
	events map[uint64]*model.OutboxEvent // id -> event
	nextID uint64
	// 控制行为
	pickErr     error
	doneErr     error
	doneCallErr bool // MarkOutboxEventDone 调用失败一次后恢复
}

func newMockAdminStore() *mockAdminStore {
	return &mockAdminStore{
		events: make(map[uint64]*model.OutboxEvent),
		nextID: 1,
	}
}

func (s *mockAdminStore) addPendingEvent(eventType, payload string) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	s.events[id] = &model.OutboxEvent{
		ID:        id,
		EventType: eventType,
		Payload:   payload,
		Status:    "pending",
	}
	return id
}

func (s *mockAdminStore) PickPendingOutboxEvents(ctx context.Context, limit int) ([]model.OutboxEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pickErr != nil {
		return nil, s.pickErr
	}
	var result []model.OutboxEvent
	for _, evt := range s.events {
		if evt.Status == "pending" {
			result = append(result, *evt)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (s *mockAdminStore) MarkOutboxEventDone(ctx context.Context, id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.doneCallErr {
		s.doneCallErr = false // 只失败一次
		return errors.New("mock error")
	}
	if s.doneErr != nil {
		return s.doneErr
	}
	if evt, ok := s.events[id]; ok {
		evt.Status = "done"
	}
	return nil
}

func (s *mockAdminStore) MarkOutboxEventFailed(ctx context.Context, id uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if evt, ok := s.events[id]; ok {
		evt.Status = "failed"
	}
	return nil
}

func (s *mockAdminStore) UpdateProduct(_ context.Context, product *model.Product) error { return nil }
func (s *mockAdminStore) UpdateProductStatus(_ context.Context, id uint64, status string) error {
	return nil
}
func (s *mockAdminStore) HasActiveAuctionByProduct(_ context.Context, productID uint64) (bool, error) {
	return false, nil
}
func (s *mockAdminStore) UpdateProductStock(_ context.Context, productID uint64, delta int) error {
	return nil
}
func (s *mockAdminStore) HasPendingPaymentOrder(_ context.Context, productID uint64) (bool, error) {
	return false, nil
}
func (s *mockAdminStore) ListAuctionBids(_ context.Context, auctionID uint64, limit int) ([]model.Bid, error) {
	return nil, nil
}

// 实现 AdminStore 其他必须方法（最小 stub）
func (s *mockAdminStore) CreateProduct(ctx context.Context, product *model.Product) error { return nil }
func (s *mockAdminStore) ListProducts(ctx context.Context, sellerID *uint64) ([]model.Product, error) {
	return nil, nil
}
func (s *mockAdminStore) GetProduct(ctx context.Context, id uint64) (*model.Product, error) {
	return nil, nil
}
func (s *mockAdminStore) DeleteProduct(ctx context.Context, id uint64) error              { return nil }
func (s *mockAdminStore) CreateAuction(ctx context.Context, auction *model.Auction) error { return nil }
func (s *mockAdminStore) GetAuction(ctx context.Context, id uint64) (*model.Auction, error) {
	return nil, nil
}
func (s *mockAdminStore) UpdateAuction(ctx context.Context, auction *model.Auction) error { return nil }
func (s *mockAdminStore) ListAuctions(ctx context.Context, filter repository.AuctionFilter) ([]model.Auction, error) {
	return nil, nil
}
func (s *mockAdminStore) CreateRoom(ctx context.Context, room *model.LiveRoom) error { return nil }
func (s *mockAdminStore) GetRoom(ctx context.Context, id uint64) (*model.LiveRoom, error) {
	return nil, nil
}
func (s *mockAdminStore) UpdateRoom(ctx context.Context, room *model.LiveRoom) error { return nil }
func (s *mockAdminStore) DeleteRoom(ctx context.Context, id uint64) error            { return nil }
func (s *mockAdminStore) ListRoomsBySeller(ctx context.Context, sellerID uint64) ([]model.LiveRoom, error) {
	return nil, nil
}
func (s *mockAdminStore) GetUser(ctx context.Context, id uint64) (*model.User, error) {
	return nil, nil
}
func (s *mockAdminStore) GetUsernameByUserID(ctx context.Context, id uint64) (string, error) {
	return "", nil
}
func (s *mockAdminStore) CreateOrder(ctx context.Context, order *model.Order) error { return nil }
func (s *mockAdminStore) GetOrder(ctx context.Context, id uint64) (*model.Order, error) {
	return nil, nil
}
func (s *mockAdminStore) GetOrderByAuction(ctx context.Context, auctionID uint64) (*model.Order, error) {
	return nil, nil
}
func (s *mockAdminStore) UpdateOrder(ctx context.Context, order *model.Order) error { return nil }
func (s *mockAdminStore) ListOrders(ctx context.Context) ([]model.Order, error)     { return nil, nil }
func (s *mockAdminStore) ListOrdersBySeller(ctx context.Context, sellerID uint64) ([]model.Order, error) {
	return nil, nil
}
func (s *mockAdminStore) ListOrdersByBuyer(ctx context.Context, buyerID uint64) ([]model.Order, error) {
	return nil, nil
}
func (s *mockAdminStore) UpdateOrderStatus(ctx context.Context, id uint64, status string, paidAt *time.Time, addressID *uint64, addressSnapshot string) error {
	return nil
}
func (s *mockAdminStore) ListExpiredPendingOrders(ctx context.Context, before time.Time, limit int) ([]model.Order, error) {
	return nil, nil
}
func (s *mockAdminStore) ListRunningExpiredAuctions(ctx context.Context) ([]model.Auction, error) {
	return nil, nil
}
func (s *mockAdminStore) WithTx(ctx context.Context, fn func(repository.AdminStore) error) error {
	return fn(s)
}
func (s *mockAdminStore) CreateOutboxEvent(ctx context.Context, evt *model.OutboxEvent) error {
	return nil
}
func (s *mockAdminStore) CreateBid(ctx context.Context, bid *model.Bid) error { return nil }
func (s *mockAdminStore) UpdateAuctionBidState(ctx context.Context, auction *model.Auction) error {
	return nil
}

// mockRedisClientOutbox 是 OutboxPoller 用的 Redis mock（只需要 XAdd）。
type mockRedisClientOutbox struct {
	xaddErr  error
	xaddCall int
}

func newMockRedisOutbox() *mockRedisClientOutbox {
	return &mockRedisClientOutbox{}
}

func (r *mockRedisClientOutbox) XAdd(ctx context.Context, a *goredis.XAddArgs) *goredis.StringCmd {
	cmd := goredis.NewStringCmd(ctx)
	r.xaddCall++
	if r.xaddErr != nil {
		cmd.SetErr(r.xaddErr)
	}
	cmd.SetVal("mock-stream-id-12345")
	return cmd
}

// TestOutboxPollOnce_Success 验证 pollOnce 正常流程：XAdd → MarkDone。
func TestOutboxPollOnce_Success(t *testing.T) {
	store := newMockAdminStore()
	redis := newMockRedisOutbox()

	payload, _ := json.Marshal(map[string]interface{}{"roomId": 1, "auctionId": 1})
	store.addPendingEvent("bid.accepted", string(payload))

	p := &OutboxPoller{
		adminStore: store,
		redis:      redis,
		streamKey:  "auction:events",
		batchSize:  50,
	}
	p.pollOnce(context.Background())

	if redis.xaddCall != 1 {
		t.Errorf("expected 1 XAdd call, got %d", redis.xaddCall)
	}
	if store.events[1].Status != "done" {
		t.Errorf("expected event status 'done', got '%s'", store.events[1].Status)
	}
}

// TestOutboxPollOnce_XAddFail 验证 XAdd 失败时事件标记为 failed，不移除。
func TestOutboxPollOnce_XAddFail(t *testing.T) {
	store := newMockAdminStore()
	redis := newMockRedisOutbox()
	redis.xaddErr = errors.New("mock error")

	payload, _ := json.Marshal(map[string]interface{}{"roomId": 1})
	store.addPendingEvent("bid.accepted", string(payload))

	p := &OutboxPoller{
		adminStore: store,
		redis:      redis,
		streamKey:  "auction:events",
		batchSize:  50,
	}
	p.pollOnce(context.Background())

	if store.events[1].Status != "failed" {
		t.Errorf("expected event status 'failed' after XAdd error, got '%s'", store.events[1].Status)
	}
}

// TestOutboxPollOnce_MarkDoneFail 验证 XAdd 成功但 MarkDone 失败时，事件保持 pending 以便重试。
func TestOutboxPollOnce_MarkDoneFail(t *testing.T) {
	store := newMockAdminStore()
	redis := newMockRedisOutbox()
	store.doneErr = errors.New("mock error")

	payload, _ := json.Marshal(map[string]interface{}{"roomId": 1})
	store.addPendingEvent("bid.accepted", string(payload))

	p := &OutboxPoller{
		adminStore: store,
		redis:      redis,
		streamKey:  "auction:events",
		batchSize:  50,
	}
	p.pollOnce(context.Background())

	// XAdd 已成功，但 MarkDone 失败，事件保持 pending 以在下轮重试
	if redis.xaddCall != 1 {
		t.Errorf("expected 1 XAdd call, got %d", redis.xaddCall)
	}
	if store.events[1].Status != "pending" {
		t.Errorf("expected event status 'pending' (retry next cycle), got '%s'", store.events[1].Status)
	}
}

// TestOutboxPollOnce_MultipleEvents 验证多事件批量处理。
func TestOutboxPollOnce_MultipleEvents(t *testing.T) {
	store := newMockAdminStore()
	redis := newMockRedisOutbox()

	for i := 0; i < 3; i++ {
		payload, _ := json.Marshal(map[string]interface{}{"roomId": 1, "idx": i})
		store.addPendingEvent("bid.accepted", string(payload))
	}

	p := &OutboxPoller{
		adminStore: store,
		redis:      redis,
		streamKey:  "auction:events",
		batchSize:  50,
	}
	p.pollOnce(context.Background())

	if redis.xaddCall != 3 {
		t.Errorf("expected 3 XAdd calls, got %d", redis.xaddCall)
	}
	for id := uint64(1); id <= 3; id++ {
		if store.events[id].Status != "done" {
			t.Errorf("event %d: expected 'done', got '%s'", id, store.events[id].Status)
		}
	}
}

// TestOutboxPollOnce_PickError 验证 PickPendingOutboxEvents 返回错误时不 panic。
func TestOutboxPollOnce_PickError(t *testing.T) {
	store := newMockAdminStore()
	store.pickErr = errors.New("mock error")
	redis := newMockRedisOutbox()

	p := &OutboxPoller{
		adminStore: store,
		redis:      redis,
	}
	p.pollOnce(context.Background())

	if redis.xaddCall != 0 {
		t.Errorf("expected 0 XAdd calls after pick error, got %d", redis.xaddCall)
	}
}

// TestOutboxPollOnce_NoEvents 验证无 pending 事件时不执行任何操作。
func TestOutboxPollOnce_NoEvents(t *testing.T) {
	store := newMockAdminStore()
	redis := newMockRedisOutbox()

	p := &OutboxPoller{
		adminStore: store,
		redis:      redis,
	}
	p.pollOnce(context.Background())

	if redis.xaddCall != 0 {
		t.Errorf("expected 0 XAdd calls, got %d", redis.xaddCall)
	}
}

// TestOutboxPollOnce_Concurrent 验证并发调用 pollOnce 不 panic。
func TestOutboxPollOnce_Concurrent(t *testing.T) {
	store := newMockAdminStore()
	redis := newMockRedisOutbox()

	for i := 0; i < 10; i++ {
		payload, _ := json.Marshal(map[string]interface{}{"roomId": 1})
		store.addPendingEvent("bid.accepted", string(payload))
	}

	p := &OutboxPoller{
		adminStore: store,
		redis:      redis,
		batchSize:  50,
	}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.pollOnce(context.Background())
		}()
	}
	wg.Wait()
}
