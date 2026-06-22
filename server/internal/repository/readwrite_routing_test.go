package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"paimai/internal/model"
)

type routeSpyLogger struct {
	mu    sync.Mutex
	count int
}

func (l *routeSpyLogger) LogMode(logger.LogLevel) logger.Interface      { return l }
func (l *routeSpyLogger) Info(context.Context, string, ...interface{})  {}
func (l *routeSpyLogger) Warn(context.Context, string, ...interface{})  {}
func (l *routeSpyLogger) Error(context.Context, string, ...interface{}) {}
func (l *routeSpyLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count++
}

func (l *routeSpyLogger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

func (l *routeSpyLogger) bump() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count++
}

func newDryRunDB(t *testing.T, spy logger.Interface) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "gorm:gorm@tcp(127.0.0.1:9910)/gorm?charset=utf8mb4&parseTime=True&loc=Local",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               spy,
	})
	if err != nil {
		t.Fatalf("open dry-run db: %v", err)
	}
	if routeSpy, ok := spy.(*routeSpyLogger); ok {
		db.Callback().Query().Before("gorm:query").Register("route_spy:query", func(*gorm.DB) { routeSpy.bump() })
		db.Callback().Create().Before("gorm:create").Register("route_spy:create", func(*gorm.DB) { routeSpy.bump() })
		db.Callback().Update().Before("gorm:update").Register("route_spy:update", func(*gorm.DB) { routeSpy.bump() })
		db.Callback().Delete().Before("gorm:delete").Register("route_spy:delete", func(*gorm.DB) { routeSpy.bump() })
	}
	return db
}

func newRoutedDBs(t *testing.T) (*gorm.DB, *gorm.DB, *routeSpyLogger, *routeSpyLogger) {
	t.Helper()
	readSpy := &routeSpyLogger{}
	writeSpy := &routeSpyLogger{}
	return newDryRunDB(t, readSpy), newDryRunDB(t, writeSpy), readSpy, writeSpy
}

func assertOnlyRead(t *testing.T, readSpy, writeSpy *routeSpyLogger) {
	t.Helper()
	if readSpy.Count() == 0 {
		t.Fatal("expected read DB to be used")
	}
	if writeSpy.Count() != 0 {
		t.Fatalf("expected write DB to stay unused, got %d calls", writeSpy.Count())
	}
}

func assertOnlyWrite(t *testing.T, readSpy, writeSpy *routeSpyLogger) {
	t.Helper()
	if writeSpy.Count() == 0 {
		t.Fatal("expected write DB to be used")
	}
	if readSpy.Count() != 0 {
		t.Fatalf("expected read DB to stay unused, got %d calls", readSpy.Count())
	}
}

func TestPublicStoreRoutesInitialReadsAndWrites(t *testing.T) {
	ctx := context.Background()

	readDB, writeDB, readSpy, writeSpy := newRoutedDBs(t)
	public := NewGormPublicStoreWithRouter(readDB, writeDB)
	_, _ = public.ListLiveRooms(ctx)
	assertOnlyRead(t, readSpy, writeSpy)

	readDB, writeDB, readSpy, writeSpy = newRoutedDBs(t)
	public = NewGormPublicStoreWithRouter(readDB, writeDB)
	_ = public.CreateBid(ctx, &model.Bid{AuctionID: 1, UserID: 1, AmountCents: 100, Accepted: true})
	assertOnlyWrite(t, readSpy, writeSpy)
}

func TestAdminStoreRoutesListsToReadCommandsToWrite(t *testing.T) {
	ctx := context.Background()

	readDB, writeDB, readSpy, writeSpy := newRoutedDBs(t)
	admin := NewGormAdminStoreWithRouter(readDB, writeDB)
	_, _ = admin.ListProducts(ctx, nil)
	assertOnlyRead(t, readSpy, writeSpy)

	readDB, writeDB, readSpy, writeSpy = newRoutedDBs(t)
	admin = NewGormAdminStoreWithRouter(readDB, writeDB)
	_ = admin.CreateProduct(ctx, &model.Product{Name: "p", SellerID: 1})
	assertOnlyWrite(t, readSpy, writeSpy)

	readDB, writeDB, readSpy, writeSpy = newRoutedDBs(t)
	admin = NewGormAdminStoreWithRouter(readDB, writeDB)
	_, _ = admin.GetAuction(ctx, 1)
	assertOnlyWrite(t, readSpy, writeSpy)
}

func TestAuthStoreRoutesStrongConsistencyReadsAndWrites(t *testing.T) {
	ctx := context.Background()

	readDB, writeDB, readSpy, writeSpy := newRoutedDBs(t)
	auth := NewGormAuthStoreWithRouter(readDB, writeDB)
	_, _ = auth.GetUserAuthByUsername(ctx, "alice")
	assertOnlyWrite(t, readSpy, writeSpy)

	readDB, writeDB, readSpy, writeSpy = newRoutedDBs(t)
	auth = NewGormAuthStoreWithRouter(readDB, writeDB)
	_ = auth.CreateUser(ctx, &model.User{Nickname: "Alice", Role: "buyer"})
	assertOnlyWrite(t, readSpy, writeSpy)
}

func TestAddressStoreRoutesReadsAndWrites(t *testing.T) {
	ctx := context.Background()

	readDB, writeDB, readSpy, writeSpy := newRoutedDBs(t)
	address := NewGormAddressStoreWithRouter(readDB, writeDB)
	_, _ = address.ListAddresses(ctx, 1)
	assertOnlyRead(t, readSpy, writeSpy)

	readDB, writeDB, readSpy, writeSpy = newRoutedDBs(t)
	address = NewGormAddressStoreWithRouter(readDB, writeDB)
	_ = address.DeleteAddress(ctx, 1, 9)
	assertOnlyWrite(t, readSpy, writeSpy)
}
