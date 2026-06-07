package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"paimai/internal/model"
	"paimai/internal/repository"
)

// RoomService 负责直播间管理（CRUD、开播、关播自动结算）。
type RoomService struct {
	adminStore    repository.AdminStore
	settleService *SettleService
	now           func() time.Time
}

// NewRoomService 创建直播间管理服务。
func NewRoomService(adminStore repository.AdminStore, settleService *SettleService) *RoomService {
	return &RoomService{
		adminStore:    adminStore,
		settleService: settleService,
		now:           time.Now,
	}
}

// CreateRoomInput 是创建直播间的输入参数。
type CreateRoomInput struct {
	Title    string `json:"title"`
	CoverURL string `json:"coverUrl"`
}

// RoomResult 是直播间信息的返回结构。
type RoomResult struct {
	ID        uint64 `json:"id"`
	SellerID  uint64 `json:"sellerId"`
	Title     string `json:"title"`
	CoverURL  string `json:"coverUrl"`
	Status    string `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// CloseRoomResult 是关播操作的返回结构。
type CloseRoomResult struct {
	RoomID  uint64 `json:"roomId"`
	Status  string `json:"status"`
	Settled int    `json:"settled"` // 本次关播时结算的竞拍数量
}

// CreateRoom 创建直播间。
func (s *RoomService) CreateRoom(ctx context.Context, sellerID uint64, input CreateRoomInput) (*RoomResult, error) {
	input.Title = strings.TrimSpace(input.Title)
	input.CoverURL = strings.TrimSpace(input.CoverURL)
	if input.Title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidInput)
	}

	room := &model.LiveRoom{
		SellerID: sellerID,
		Title:    input.Title,
		CoverURL: input.CoverURL,
		Status:   "offline",
	}
	if err := s.adminStore.CreateRoom(ctx, room); err != nil {
		return nil, err
	}
	return toRoomResult(room), nil
}

// ListRooms 返回当前商家所有直播间。
func (s *RoomService) ListRooms(ctx context.Context, sellerID uint64) ([]RoomResult, error) {
	rooms, err := s.adminStore.ListRoomsBySeller(ctx, sellerID)
	if err != nil {
		return nil, err
	}
	results := make([]RoomResult, 0, len(rooms))
	for _, r := range rooms {
		results = append(results, *toRoomResult(&r))
	}
	return results, nil
}

// GetRoom 返回直播间详情。
func (s *RoomService) GetRoom(ctx context.Context, id uint64) (*RoomResult, error) {
	room, err := s.adminStore.GetRoom(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return toRoomResult(room), nil
}

// UpdateRoom 更新当前商家的直播间信息。
func (s *RoomService) UpdateRoom(ctx context.Context, id uint64, sellerID uint64, input CreateRoomInput) (*RoomResult, error) {
	room, err := s.adminStore.GetRoom(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if room.SellerID != sellerID {
		return nil, ErrUnauthorized
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Title == "" {
		return nil, fmt.Errorf("%w: title is required", ErrInvalidInput)
	}
	room.Title = input.Title
	room.CoverURL = strings.TrimSpace(input.CoverURL)
	if err := s.adminStore.UpdateRoom(ctx, room); err != nil {
		return nil, err
	}
	return toRoomResult(room), nil
}

// DeleteRoom 删除当前商家的空直播间。已有上架计划或竞拍历史时拒绝，避免断开历史数据。
func (s *RoomService) DeleteRoom(ctx context.Context, id uint64, sellerID uint64) error {
	room, err := s.adminStore.GetRoom(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		return err
	}
	if room.SellerID != sellerID {
		return ErrUnauthorized
	}
	if room.Status == "live" {
		return fmt.Errorf("%w: cannot delete a live room", ErrInvalidTransition)
	}

	auctions, err := s.adminStore.ListAuctions(ctx, repository.AuctionFilter{RoomID: &id})
	if err != nil {
		return err
	}
	if len(auctions) > 0 {
		return fmt.Errorf("%w: room has listing history, edit the room instead", ErrInvalidInput)
	}
	return s.adminStore.DeleteRoom(ctx, id)
}

// GoLive 将直播间从 offline 切换到 live。
func (s *RoomService) GoLive(ctx context.Context, id uint64) (*RoomResult, error) {
	room, err := s.adminStore.GetRoom(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if room.Status == "live" {
		return toRoomResult(room), nil
	}
	if room.Status != "offline" {
		return nil, fmt.Errorf("%w: cannot go live from status %s", ErrInvalidTransition, room.Status)
	}
	room.Status = "live"
	if err := s.adminStore.UpdateRoom(ctx, room); err != nil {
		return nil, err
	}
	return toRoomResult(room), nil
}

// CloseRoom 关播，自动结算该房间所有 running 竞拍。
func (s *RoomService) CloseRoom(ctx context.Context, id uint64) (*CloseRoomResult, error) {
	room, err := s.adminStore.GetRoom(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if room.Status == "closed" {
		return nil, fmt.Errorf("%w: room already closed", ErrInvalidTransition)
	}

	// 结算该房间所有未进入终态的竞拍（running 的倒计时未结束、sold 的尚未结算）
	// 使用空 filter 查全部竞拍，再过滤
	allAuctions, err := s.adminStore.ListAuctions(ctx, repository.AuctionFilter{RoomID: &id})
	if err != nil {
		return nil, err
	}
	settled := 0
	for _, auction := range allAuctions {
		if auction.Status != "running" && auction.Status != "sold" {
			continue
		}
		if _, err := s.settleService.SettleAuction(ctx, auction.ID); err != nil {
			log.Printf("[room] 关播结算竞拍 %d 失败: %v", auction.ID, err)
			continue
		}
		settled++
	}

	room.Status = "closed"
	if err := s.adminStore.UpdateRoom(ctx, room); err != nil {
		return nil, err
	}
	return &CloseRoomResult{
		RoomID:  id,
		Status:  "closed",
		Settled: settled,
	}, nil
}

// toRoomResult 将 model.LiveRoom 转换为 API 响应结构。
func toRoomResult(room *model.LiveRoom) *RoomResult {
	return &RoomResult{
		ID:        room.ID,
		SellerID:  room.SellerID,
		Title:     room.Title,
		CoverURL:  room.CoverURL,
		Status:    room.Status,
		CreatedAt: room.CreatedAt.Format(time.RFC3339),
	}
}
