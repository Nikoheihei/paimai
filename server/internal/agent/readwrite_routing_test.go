package agent

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

type agentRouteSpyLogger struct {
	mu    sync.Mutex
	count int
}

func (l *agentRouteSpyLogger) LogMode(logger.LogLevel) logger.Interface      { return l }
func (l *agentRouteSpyLogger) Info(context.Context, string, ...interface{})  {}
func (l *agentRouteSpyLogger) Warn(context.Context, string, ...interface{})  {}
func (l *agentRouteSpyLogger) Error(context.Context, string, ...interface{}) {}
func (l *agentRouteSpyLogger) Trace(context.Context, time.Time, func() (string, int64), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count++
}

func (l *agentRouteSpyLogger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.count
}

func (l *agentRouteSpyLogger) bump() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.count++
}

func newAgentDryRunDB(t *testing.T, spy logger.Interface) *gorm.DB {
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
	if routeSpy, ok := spy.(*agentRouteSpyLogger); ok {
		db.Callback().Query().Before("gorm:query").Register("route_spy:query", func(*gorm.DB) { routeSpy.bump() })
		db.Callback().Create().Before("gorm:create").Register("route_spy:create", func(*gorm.DB) { routeSpy.bump() })
		db.Callback().Update().Before("gorm:update").Register("route_spy:update", func(*gorm.DB) { routeSpy.bump() })
		db.Callback().Delete().Before("gorm:delete").Register("route_spy:delete", func(*gorm.DB) { routeSpy.bump() })
	}
	return db
}

func newAgentRoutedDBs(t *testing.T) (*gorm.DB, *gorm.DB, *agentRouteSpyLogger, *agentRouteSpyLogger) {
	t.Helper()
	readSpy := &agentRouteSpyLogger{}
	writeSpy := &agentRouteSpyLogger{}
	return newAgentDryRunDB(t, readSpy), newAgentDryRunDB(t, writeSpy), readSpy, writeSpy
}

func assertAgentOnlyRead(t *testing.T, readSpy, writeSpy *agentRouteSpyLogger) {
	t.Helper()
	if readSpy.Count() == 0 {
		t.Fatal("expected read DB to be used")
	}
	if writeSpy.Count() != 0 {
		t.Fatalf("expected write DB to stay unused, got %d calls", writeSpy.Count())
	}
}

func assertAgentOnlyWrite(t *testing.T, readSpy, writeSpy *agentRouteSpyLogger) {
	t.Helper()
	if writeSpy.Count() == 0 {
		t.Fatal("expected write DB to be used")
	}
	if readSpy.Count() != 0 {
		t.Fatalf("expected read DB to stay unused, got %d calls", readSpy.Count())
	}
}

func TestAgentStoreRoutesUIReadsAndGuardWrites(t *testing.T) {
	ctx := context.Background()

	readDB, writeDB, readSpy, writeSpy := newAgentRoutedDBs(t)
	store := NewGormStoreWithRouter(readDB, writeDB)
	_, _ = store.ListAgentsByOwner(ctx, 1, AgentTypeBuyer)
	assertAgentOnlyRead(t, readSpy, writeSpy)

	readDB, writeDB, readSpy, writeSpy = newAgentRoutedDBs(t)
	store = NewGormStoreWithRouter(readDB, writeDB)
	_, _ = store.ListActiveBuyerAgents(ctx)
	assertAgentOnlyWrite(t, readSpy, writeSpy)

	readDB, writeDB, readSpy, writeSpy = newAgentRoutedDBs(t)
	store = NewGormStoreWithRouter(readDB, writeDB)
	_ = store.CreateAgent(ctx, &model.AgentProfile{OwnerUserID: 1, AgentType: AgentTypeBuyer, Status: AgentStatusDraft})
	assertAgentOnlyWrite(t, readSpy, writeSpy)

	readDB, writeDB, readSpy, writeSpy = newAgentRoutedDBs(t)
	store = NewGormStoreWithRouter(readDB, writeDB)
	_, _ = store.ListAuditLogs(ctx, 1, 20)
	assertAgentOnlyRead(t, readSpy, writeSpy)
}
