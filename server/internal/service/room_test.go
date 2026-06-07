package service

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"

	"paimai/internal/model"
)

func TestRoomServiceDeletesEmptyOfflineRoom(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewRoomService(store, NewSettleService(store))

	room, err := svc.CreateRoom(ctx, 1, CreateRoomInput{Title: "输错名称"})
	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	if err := svc.DeleteRoom(ctx, room.ID, 1); err != nil {
		t.Fatalf("DeleteRoom() error = %v", err)
	}

	if _, err := store.GetRoom(ctx, room.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected deleted room to be gone, got %v", err)
	}
}

func TestRoomServiceRejectsDeletingRoomWithListings(t *testing.T) {
	ctx := context.Background()
	store := newAdminStoreStub()
	svc := NewRoomService(store, NewSettleService(store))

	room, err := svc.CreateRoom(ctx, 1, CreateRoomInput{Title: "已有上架计划"})
	if err != nil {
		t.Fatalf("CreateRoom() error = %v", err)
	}
	store.auctions[1] = &model.Auction{ID: 1, RoomID: room.ID, ProductID: 1, Status: "draft"}

	err = svc.DeleteRoom(ctx, room.ID, 1)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if _, err := store.GetRoom(ctx, room.ID); err != nil {
		t.Fatalf("expected room to remain, got %v", err)
	}
}
