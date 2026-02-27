package fsm

import (
	"sync"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

type State string

func (s State) String() string { return string(s) }

type Event string

func (e Event) String() string { return string(e) }

type callback struct {
	lambda core.Lambda
	eval   core.Evaluator
	env    *core.Env
}

type StateMachine struct {
	mu        sync.RWMutex
	id        string
	initial   State
	current   State
	states    map[State]*StateConfig
	callbacks []callback
}

type StateConfig struct {
	On map[Event]Transition
}

type Transition struct {
	Target State
	Guard  core.Value
	Action core.Value
}

type StateEvent struct {
	MachineID string
	From      State
	To        State
	At        time.Time
	Context   map[string]any
}

type PersistentState struct {
	Version   int       `json:"version"`
	MachineID string    `json:"machine_id"`
	Current   string    `json:"current"`
	At        time.Time `json:"at"`
}

type TransitionResult struct {
	State State
	From  State
	To    State
	Valid bool
	Error string
}
