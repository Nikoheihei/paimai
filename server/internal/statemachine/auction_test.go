package statemachine

import (
	"errors"
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
		{
			name:          "Draft to Scheduled via Publish",
			initialState:  StateDraft,
			event:         EventPublish,
			expectedState: StateScheduled,
			expectError:   false,
		},
		{
			name:          "Draft to Cancelled via Cancel",
			initialState:  StateDraft,
			event:         EventCancel,
			expectedState: StateCancelled,
			expectError:   false,
		},
		{
			name:          "Scheduled to Running via Start",
			initialState:  StateScheduled,
			event:         EventStart,
			expectedState: StateRunning,
			expectError:   false,
		},
		{
			name:          "Scheduled to Cancelled via Cancel",
			initialState:  StateScheduled,
			event:         EventCancel,
			expectedState: StateCancelled,
			expectError:   false,
		},
		{
			name:          "Running to Sold via SettleSold",
			initialState:  StateRunning,
			event:         EventSettleSold,
			expectedState: StateSold,
			expectError:   false,
		},
		{
			name:          "Running to Failed via SettleFailed",
			initialState:  StateRunning,
			event:         EventSettleFailed,
			expectedState: StateFailed,
			expectError:   false,
		},
		{
			name:          "Running to Cancelled via Cancel",
			initialState:  StateRunning,
			event:         EventCancel,
			expectedState: StateCancelled,
			expectError:   false,
		},
		{
			name:         "Invalid transition: Draft to Running directly",
			initialState: StateDraft,
			event:        EventStart,
			expectError:  true,
		},
		{
			name:         "Invalid transition: Sold to Running",
			initialState: StateSold,
			event:        EventStart,
			expectError:  true,
		},
		{
			name:         "Invalid transition: Cancelled to Draft",
			initialState: StateCancelled,
			event:        EventPublish,
			expectError:  true,
		},
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
	if !sm.CanTransition(EventPublish) {
		t.Errorf("expected CanTransition(EventPublish) from Draft to be true")
	}
	if sm.CanTransition(EventStart) {
		t.Errorf("expected CanTransition(EventStart) from Draft to be false")
	}
}
