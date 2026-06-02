package statemachine

import (
	"errors"
	"fmt"
)

// State 表示拍卖的当前状态。
type State string

const (
	StateDraft     State = "draft"
	StateScheduled State = "scheduled"
	StateRunning   State = "running"
	StateSold      State = "sold"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Event 表示状态转移的触发事件。
type Event string

const (
	EventPublish      Event = "publish"
	EventStart        Event = "start"
	EventCancel       Event = "cancel"
	EventSettleSold   Event = "settle_sold"
	EventSettleFailed Event = "settle_failed"
)

// ErrInvalidTransition 当不允许进行状态转移时返回。
var ErrInvalidTransition = errors.New("invalid state transition")

// TransitionTable 将当前状态和触发事件映射到允许的目标状态。
// 此设计符合开闭原则（OCP）—— 可以通过扩展该静态 Map 来添加新的状态和转移，而无需修改核心状态机流转逻辑。
var TransitionTable = map[State]map[Event]State{
	StateDraft: {
		EventPublish: StateScheduled,
		EventCancel:  StateCancelled,
	},
	StateScheduled: {
		EventStart:  StateRunning,
		EventCancel: StateCancelled,
	},
	StateRunning: {
		EventSettleSold:   StateSold,
		EventSettleFailed: StateFailed,
		EventCancel:       StateCancelled,
	},
}

// Machine 处理状态转移并验证其正确性。
type Machine struct {
	currentState State
}

// NewMachine 初始化带有起始状态的竞拍状态机。
func NewMachine(initialState State) *Machine {
	return &Machine{currentState: initialState}
}

// Current 返回状态机当前状态。
func (m *Machine) Current() State {
	return m.currentState
}

// CanTransition 检查给定事件是否允许从当前状态触发。
func (m *Machine) CanTransition(event Event) bool {
	if m.currentState == "" {
		return false
	}
	transitions, ok := TransitionTable[m.currentState]
	if !ok {
		return false
	}
	_, allowed := transitions[event]
	return allowed
}

// Transition 尝试使用给定事件执行状态迁移；迁移非法时返回 ErrInvalidTransition。
func (m *Machine) Transition(event Event) (State, error) {
	transitions, ok := TransitionTable[m.currentState]
	if !ok {
		return m.currentState, fmt.Errorf("%w: state '%s' has no defined transitions", ErrInvalidTransition, m.currentState)
	}

	nextState, allowed := transitions[event]
	if !allowed {
		return m.currentState, fmt.Errorf("%w: cannot trigger event '%s' from state '%s'", ErrInvalidTransition, event, m.currentState)
	}

	m.currentState = nextState
	return m.currentState, nil
}
