package statemachine

import (
	"errors"
	"sync"
	"testing"
)

// TestStateTransitions 验证竞拍状态机允许和拒绝的状态迁移。
func TestStateTransitions(t *testing.T) {
	tests := []struct {
		name          string
		initialState  State
		event         Event
		expectedState State
		expectError   bool
	}{
		{name: "Draft to Scheduled via Publish", initialState: StateDraft, event: EventPublish, expectedState: StateScheduled, expectError: false},
		{name: "Draft to Cancelled via Cancel", initialState: StateDraft, event: EventCancel, expectedState: StateCancelled, expectError: false},
		{name: "Scheduled to Running via Start", initialState: StateScheduled, event: EventStart, expectedState: StateRunning, expectError: false},
		{name: "Scheduled to Cancelled via Cancel", initialState: StateScheduled, event: EventCancel, expectedState: StateCancelled, expectError: false},
		{name: "Running to Sold via SettleSold", initialState: StateRunning, event: EventSettleSold, expectedState: StateSold, expectError: false},
		{name: "Running to Failed via SettleFailed", initialState: StateRunning, event: EventSettleFailed, expectedState: StateFailed, expectError: false},
		{name: "Running to Cancelled via Cancel", initialState: StateRunning, event: EventCancel, expectedState: StateCancelled, expectError: false},
		{name: "Invalid: Draft to Running directly", initialState: StateDraft, event: EventStart, expectError: true},
		{name: "Invalid: Sold to Running", initialState: StateSold, event: EventStart, expectError: true},
		{name: "Invalid: Cancelled to Publish", initialState: StateCancelled, event: EventPublish, expectError: true},
		// 终态非法迁移全覆盖
		{name: "Invalid: Sold to SettleSold", initialState: StateSold, event: EventSettleSold, expectError: true},
		{name: "Invalid: Sold to SettleFailed", initialState: StateSold, event: EventSettleFailed, expectError: true},
		{name: "Invalid: Sold to Cancel", initialState: StateSold, event: EventCancel, expectError: true},
		{name: "Invalid: Sold to Publish", initialState: StateSold, event: EventPublish, expectError: true},
		{name: "Invalid: Failed to Start", initialState: StateFailed, event: EventStart, expectError: true},
		{name: "Invalid: Failed to Publish", initialState: StateFailed, event: EventPublish, expectError: true},
		{name: "Invalid: Failed to Cancel", initialState: StateFailed, event: EventCancel, expectError: true},
		{name: "Invalid: Failed to SettleSold", initialState: StateFailed, event: EventSettleSold, expectError: true},
		{name: "Invalid: Failed to SettleFailed", initialState: StateFailed, event: EventSettleFailed, expectError: true},
		{name: "Invalid: Cancelled to Cancel", initialState: StateCancelled, event: EventCancel, expectError: true},
		{name: "Invalid: Cancelled to Start", initialState: StateCancelled, event: EventStart, expectError: true},
		{name: "Invalid: Cancelled to SettleSold", initialState: StateCancelled, event: EventSettleSold, expectError: true},
		{name: "Invalid: Cancelled to SettleFailed", initialState: StateCancelled, event: EventSettleFailed, expectError: true},
		{name: "Invalid: Running to Publish", initialState: StateRunning, event: EventPublish, expectError: true},
		{name: "Invalid: Running to Start", initialState: StateRunning, event: EventStart, expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := NewMachine(tt.initialState)
			nextState, err := sm.Transition(tt.event)

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				if !errors.Is(err, ErrInvalidTransition) {
					t.Errorf("expected ErrInvalidTransition, got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if nextState != tt.expectedState {
					t.Errorf("expected state %s, got %s", tt.expectedState, nextState)
				}
				if sm.Current() != tt.expectedState {
					t.Errorf("machine current state expected %s, got %s", tt.expectedState, sm.Current())
				}
			}
		})
	}
}

// TestCanTransition 验证状态机在不改变状态的情况下判断事件是否可触发。
func TestCanTransition(t *testing.T) {
	sm := NewMachine(StateDraft)
	ok, err := sm.CanTransition(EventPublish)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !ok {
		t.Errorf("expected CanTransition(EventPublish) from Draft to be true")
	}

	ok, err = sm.CanTransition(EventStart)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if ok {
		t.Errorf("expected CanTransition(EventStart) from Draft to be false")
	}
}

// TestCanTransitionEmptyState 验证空状态时 CanTransition 返回 false 和错误。
func TestCanTransitionEmptyState(t *testing.T) {
	sm := NewMachine("")
	ok, err := sm.CanTransition(EventPublish)
	if ok {
		t.Errorf("expected false for empty state")
	}
	if err == nil {
		t.Errorf("expected error for empty state")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

// TestNewMachineRejectsEmptyState 验证 NewMachine("") 行为（当前允许，但 CanTransition 拒绝）。
func TestNewMachineEmptyState(t *testing.T) {
	sm := NewMachine("")
	_, err := sm.Transition(EventPublish)
	if err == nil {
		t.Errorf("expected error for empty state transition")
	}
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

// TestFinalStateAllEvents 验证所有终态对所有事件返回 ErrInvalidTransition。
func TestFinalStateAllEvents(t *testing.T) {
	finalStates := []State{StateSold, StateFailed, StateCancelled}
	allEvents := []Event{EventPublish, EventStart, EventCancel, EventSettleSold, EventSettleFailed}

	for _, state := range finalStates {
		sm := NewMachine(state)
		for _, event := range allEvents {
			_, err := sm.Transition(event)
			if err == nil {
				t.Errorf("expected error for %s -> %s", state, event)
			}
			if !errors.Is(err, ErrInvalidTransition) {
				t.Errorf("expected ErrInvalidTransition for %s -> %s, got: %v", state, event, err)
			}
		}
	}
}

// TestConcurrentTransition 验证并发调用 Transition 不 panic（Machine 非线程安全，但不应 panic）。
func TestConcurrentTransition(t *testing.T) {
	sm := NewMachine(StateDraft)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// 多次 Transition 可能因竞态返回错误，但不应 panic
			sm.Transition(EventPublish)
		}()
	}
	wg.Wait()
}
