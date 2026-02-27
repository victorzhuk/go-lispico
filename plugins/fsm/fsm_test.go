package fsm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

func TestState_String(t *testing.T) {
	s := State("pending")
	assert.Equal(t, "pending", s.String())
}

func TestEvent_String(t *testing.T) {
	e := Event("confirm")
	assert.Equal(t, "confirm", e.String())
}

func TestPlugin_New(t *testing.T) {
	p := New()
	assert.NotNil(t, p)
	assert.NotNil(t, p.machines)
	assert.NotNil(t, p.events)
}

func TestPlugin_Name(t *testing.T) {
	p := New()
	assert.Equal(t, "fsm", p.Name())
}

func TestPlugin_Metadata(t *testing.T) {
	p := New()
	meta := p.Metadata()
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Equal(t, "finite state machine plugin", meta.Description)
}

func TestPlugin_Init(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)

	err := p.Init(env)
	assert.NoError(t, err)

	_, ok := env.Get("fsm/create")
	assert.True(t, ok, "fsm/create should be registered")

	_, ok = env.Get("fsm/transition")
	assert.True(t, ok, "fsm/transition should be registered")

	_, ok = env.Get("fsm/valid?")
	assert.True(t, ok, "fsm/valid? should be registered")

	_, ok = env.Get("fsm/current-state")
	assert.True(t, ok, "fsm/current-state should be registered")

	_, ok = env.Get("fsm/reset")
	assert.True(t, ok, "fsm/reset should be registered")
}

func TestStateMachine_Transition(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
				},
			},
			"confirmed": {
				On: map[Event]Transition{
					"ship": {Target: "shipped"},
				},
			},
		},
	}

	result, err := sm.Transition(Event("confirm"), nil)
	assert.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, State("confirmed"), result.State)

	result, err = sm.Transition(Event("ship"), nil)
	assert.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, State("shipped"), result.State)
}

func TestStateMachine_InvalidTransition(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
				},
			},
		},
	}

	result, err := sm.Transition(Event("ship"), nil)
	assert.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Equal(t, State("pending"), result.State)
}

func TestStateMachine_Reset(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("confirmed"),
		states: map[State]*StateConfig{
			"pending":   {On: map[Event]Transition{}},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	resetState := sm.Reset()
	assert.Equal(t, State("pending"), resetState)
	assert.Equal(t, State("pending"), sm.Current())
}

func TestStateMachine_CanTransition(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
				},
			},
		},
	}

	assert.True(t, sm.CanTransition(Event("confirm")))
	assert.False(t, sm.CanTransition(Event("ship")))
}

func TestStateMachine_Reachable(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
					"cancel":  {Target: "cancelled"},
				},
			},
			"confirmed": {
				On: map[Event]Transition{
					"ship": {Target: "shipped"},
				},
			},
		},
	}

	reachable := sm.Reachable(State("pending"))
	assert.Len(t, reachable, 2)
	assert.Contains(t, reachable, State("confirmed"))
	assert.Contains(t, reachable, State("cancelled"))
}

func TestStateMachine_Serialize_Deserialize(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("confirmed"),
		states: map[State]*StateConfig{
			"pending":   {On: map[Event]Transition{}},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	data, err := sm.Serialize()
	assert.NoError(t, err)
	assert.NotEmpty(t, data)

	sm2 := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
	}

	err = sm2.Deserialize(data)
	assert.NoError(t, err)
	assert.Equal(t, State("confirmed"), sm2.Current())
}

func TestStateMachine_TransitionWithEval_Allowed(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
				},
			},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	eval := core.NewEvaluator()
	env := core.NewEnv(nil)
	ctx := context.Background()

	result, err := sm.TransitionWithEval(ctx, Event("confirm"), nil, eval, env)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, State("confirmed"), result.State)
}

func TestStateMachine_TransitionWithEval_GuardBlocks(t *testing.T) {
	env := core.NewEnv(nil)
	env.Set("allowed", core.Bool{V: false})

	guardLambda := core.Lambda{
		Params: []core.Symbol{{V: "from"}, {V: "to"}, {V: "event"}},
		Body:   []core.Value{core.Symbol{V: "allowed"}},
		Env:    env,
	}

	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed", Guard: guardLambda},
				},
			},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	eval := core.NewEvaluator()
	ctx := context.Background()

	result, err := sm.TransitionWithEval(ctx, Event("confirm"), nil, eval, env)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Equal(t, State("pending"), result.State)
	assert.Contains(t, result.Error, "guard returned false")
}

func TestStateMachine_TransitionWithEval_GuardAllows(t *testing.T) {
	env := core.NewEnv(nil)
	env.Set("allowed", core.Bool{V: true})

	guardLambda := core.Lambda{
		Params: []core.Symbol{{V: "from"}, {V: "to"}, {V: "event"}},
		Body:   []core.Value{core.Symbol{V: "allowed"}},
		Env:    env,
	}

	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed", Guard: guardLambda},
				},
			},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	eval := core.NewEvaluator()
	ctx := context.Background()

	result, err := sm.TransitionWithEval(ctx, Event("confirm"), nil, eval, env)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, State("confirmed"), result.State)
}

func TestStateMachine_TransitionWithEval_ActionExecutes(t *testing.T) {
	env := core.NewEnv(nil)
	env.Set("action-ran", core.Bool{V: false})

	actionLambda := core.Lambda{
		Params: []core.Symbol{{V: "from"}, {V: "to"}, {V: "event"}},
		Body: []core.Value{
			core.List{Items: []core.Value{
				core.Symbol{V: "set!"},
				core.Symbol{V: "action-ran"},
				core.Bool{V: true},
			}},
		},
		Env: env,
	}

	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed", Action: actionLambda},
				},
			},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	eval := core.NewEvaluator()
	ctx := context.Background()

	result, err := sm.TransitionWithEval(ctx, Event("confirm"), nil, eval, env)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, State("confirmed"), result.State)

	ran, ok := env.Get("action-ran")
	require.True(t, ok)
	assert.Equal(t, core.Bool{V: true}, ran)
}

func TestStateMachine_CallbackInvoked(t *testing.T) {
	env := core.NewEnv(nil)
	env.Set("notified", core.Bool{V: false})

	callbackLambda := core.Lambda{
		Params: []core.Symbol{{V: "machine-id"}, {V: "from"}, {V: "to"}},
		Body: []core.Value{
			core.List{Items: []core.Value{
				core.Symbol{V: "set!"},
				core.Symbol{V: "notified"},
				core.Bool{V: true},
			}},
		},
		Env: env,
	}

	sm := &StateMachine{
		id:        "test",
		initial:   State("pending"),
		current:   State("pending"),
		callbacks: []callback{{lambda: callbackLambda, eval: core.NewEvaluator(), env: env}},
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
				},
			},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	ctx := context.Background()
	sm.notify(ctx, StateEvent{
		MachineID: "test",
		From:      State("pending"),
		To:        State("confirmed"),
	})

	notified, ok := env.Get("notified")
	require.True(t, ok)
	assert.Equal(t, core.Bool{V: true}, notified)
}

func TestPlugin_CreateAndTransition(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})

	states := core.NewHashMap()
	pendingState := core.NewHashMap()
	pendingOn := core.NewHashMap()
	pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "confirm"}, core.Keyword{V: "confirmed"})
	pendingState, _ = pendingState.Assoc(core.Keyword{V: "on"}, pendingOn)
	states, _ = states.Assoc(core.Keyword{V: "pending"}, pendingState)

	confirmedState := core.NewHashMap()
	confirmedState, _ = confirmedState.Assoc(core.Keyword{V: "on"}, core.NewHashMap())
	states, _ = states.Assoc(core.Keyword{V: "confirmed"}, confirmedState)

	def, _ = def.Assoc(core.Keyword{V: "states"}, states)

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "order"}, def}, env)
	require.NoError(t, err)

	_, err = p.transition(ctx, eval, []core.Value{core.Keyword{V: "order"}, core.Keyword{V: "confirm"}}, env)
	require.NoError(t, err)

	current, err := p.currentState(ctx, eval, []core.Value{core.Keyword{V: "order"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Keyword{V: "confirmed"}, current)
}

func TestPlugin_Valid(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})

	states := core.NewHashMap()
	pendingState := core.NewHashMap()
	pendingOn := core.NewHashMap()
	pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "confirm"}, core.Keyword{V: "confirmed"})
	pendingState, _ = pendingState.Assoc(core.Keyword{V: "on"}, pendingOn)
	states, _ = states.Assoc(core.Keyword{V: "pending"}, pendingState)

	def, _ = def.Assoc(core.Keyword{V: "states"}, states)

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "order"}, def}, env)
	require.NoError(t, err)

	valid, err := p.valid(ctx, eval, []core.Value{core.Keyword{V: "order"}, core.Keyword{V: "confirm"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Bool{V: true}, valid)

	valid, err = p.valid(ctx, eval, []core.Value{core.Keyword{V: "order"}, core.Keyword{V: "ship"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Bool{V: false}, valid)
}

func TestPlugin_Reset(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})

	states := core.NewHashMap()
	pendingState := core.NewHashMap()
	pendingOn := core.NewHashMap()
	pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "confirm"}, core.Keyword{V: "confirmed"})
	pendingState, _ = pendingState.Assoc(core.Keyword{V: "on"}, pendingOn)
	states, _ = states.Assoc(core.Keyword{V: "pending"}, pendingState)

	confirmedState := core.NewHashMap()
	confirmedState, _ = confirmedState.Assoc(core.Keyword{V: "on"}, core.NewHashMap())
	states, _ = states.Assoc(core.Keyword{V: "confirmed"}, confirmedState)

	def, _ = def.Assoc(core.Keyword{V: "states"}, states)

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "order"}, def}, env)
	require.NoError(t, err)

	_, err = p.transition(ctx, eval, []core.Value{core.Keyword{V: "order"}, core.Keyword{V: "confirm"}}, env)
	require.NoError(t, err)

	resetState, err := p.reset(ctx, eval, []core.Value{core.Keyword{V: "order"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Keyword{V: "pending"}, resetState)
}

func TestPlugin_Reachable(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})

	states := core.NewHashMap()
	pendingState := core.NewHashMap()
	pendingOn := core.NewHashMap()
	pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "confirm"}, core.Keyword{V: "confirmed"})
	pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "cancel"}, core.Keyword{V: "cancelled"})
	pendingState, _ = pendingState.Assoc(core.Keyword{V: "on"}, pendingOn)
	states, _ = states.Assoc(core.Keyword{V: "pending"}, pendingState)

	def, _ = def.Assoc(core.Keyword{V: "states"}, states)

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "order"}, def}, env)
	require.NoError(t, err)

	reachable, err := p.reachable(ctx, eval, []core.Value{core.Keyword{V: "order"}}, env)
	require.NoError(t, err)

	list, ok := reachable.(core.List)
	require.True(t, ok)
	assert.Len(t, list.Items, 2)
}

func TestPlugin_StateMachine(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})

	states := core.NewHashMap()
	pendingState := core.NewHashMap()
	pendingState, _ = pendingState.Assoc(core.Keyword{V: "on"}, core.NewHashMap())
	states, _ = states.Assoc(core.Keyword{V: "pending"}, pendingState)

	def, _ = def.Assoc(core.Keyword{V: "states"}, states)

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "order"}, def}, env)
	require.NoError(t, err)

	result, err := p.stateMachine(ctx, eval, []core.Value{core.Keyword{V: "order"}}, env)
	require.NoError(t, err)

	m, ok := result.(*core.HashMap)
	require.True(t, ok)

	id, ok := m.Get(core.Keyword{V: "id"})
	require.True(t, ok)
	assert.Equal(t, core.String{V: "order"}, id)

	initial, ok := m.Get(core.Keyword{V: "initial"})
	require.True(t, ok)
	assert.Equal(t, core.Keyword{V: "pending"}, initial)
}

func TestPlugin_List(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})
	def, _ = def.Assoc(core.Keyword{V: "states"}, core.NewHashMap())

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "order"}, def}, env)
	require.NoError(t, err)

	_, err = p.create(ctx, eval, []core.Value{core.Keyword{V: "user"}, def}, env)
	require.NoError(t, err)

	result, err := p.list(ctx, eval, []core.Value{}, env)
	require.NoError(t, err)

	list, ok := result.(core.List)
	require.True(t, ok)
	assert.Len(t, list.Items, 2)
}

func TestPlugin_Events(t *testing.T) {
	p := New()

	ch := p.Events()
	assert.NotNil(t, ch)
}

func TestPlugin_Broadcast(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	env.Set("transition-fired", core.Bool{V: false})

	callbackLambda := core.Lambda{
		Params: []core.Symbol{{V: "mid"}, {V: "from"}, {V: "to"}},
		Body: []core.Value{
			core.List{Items: []core.Value{
				core.Symbol{V: "set!"},
				core.Symbol{V: "transition-fired"},
				core.Bool{V: true},
			}},
		},
		Env: env,
	}

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})

	states := core.NewHashMap()
	pendingState := core.NewHashMap()
	pendingOn := core.NewHashMap()
	pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "confirm"}, core.Keyword{V: "confirmed"})
	pendingState, _ = pendingState.Assoc(core.Keyword{V: "on"}, pendingOn)
	states, _ = states.Assoc(core.Keyword{V: "pending"}, pendingState)

	confirmedState := core.NewHashMap()
	confirmedState, _ = confirmedState.Assoc(core.Keyword{V: "on"}, core.NewHashMap())
	states, _ = states.Assoc(core.Keyword{V: "confirmed"}, confirmedState)

	def, _ = def.Assoc(core.Keyword{V: "states"}, states)

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "order"}, def}, env)
	require.NoError(t, err)

	_, err = p.onTransition(ctx, eval, []core.Value{core.Keyword{V: "order"}, callbackLambda}, env)
	require.NoError(t, err)

	_, err = p.transition(ctx, eval, []core.Value{core.Keyword{V: "order"}, core.Keyword{V: "confirm"}}, env)
	require.NoError(t, err)

	fired, ok := env.Get("transition-fired")
	require.True(t, ok)
	assert.Equal(t, core.Bool{V: true}, fired)
}

func TestPlugin_Transition_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.transition(ctx, eval, []core.Value{}, env)
	assert.Error(t, err)

	_, err = p.transition(ctx, eval, []core.Value{core.Keyword{V: "unknown"}}, env)
	assert.Error(t, err)

	_, err = p.transition(ctx, eval, []core.Value{core.String{V: "not-keyword"}, core.Keyword{V: "event"}}, env)
	assert.Error(t, err)

	_, err = p.transition(ctx, eval, []core.Value{core.Keyword{V: "unknown"}, core.String{V: "not-keyword"}}, env)
	assert.Error(t, err)
}

func TestPlugin_Create_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.create(ctx, eval, []core.Value{}, env)
	assert.Error(t, err)

	_, err = p.create(ctx, eval, []core.Value{core.String{V: "not-keyword"}, core.NewHashMap()}, env)
	assert.Error(t, err)

	_, err = p.create(ctx, eval, []core.Value{core.Keyword{V: "test"}, core.String{V: "not-map"}}, env)
	assert.Error(t, err)
}

func TestPlugin_Valid_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.valid(ctx, eval, []core.Value{}, env)
	assert.Error(t, err)

	_, err = p.valid(ctx, eval, []core.Value{core.String{V: "not-keyword"}, core.Keyword{V: "event"}}, env)
	assert.Error(t, err)
}

func TestPlugin_CurrentState_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.currentState(ctx, eval, []core.Value{}, env)
	assert.Error(t, err)

	_, err = p.currentState(ctx, eval, []core.Value{core.String{V: "not-keyword"}}, env)
	assert.Error(t, err)

	_, err = p.currentState(ctx, eval, []core.Value{core.Keyword{V: "unknown"}}, env)
	assert.Error(t, err)
}

func TestPlugin_Reset_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.reset(ctx, eval, []core.Value{}, env)
	assert.Error(t, err)

	_, err = p.reset(ctx, eval, []core.Value{core.String{V: "not-keyword"}}, env)
	assert.Error(t, err)

	_, err = p.reset(ctx, eval, []core.Value{core.Keyword{V: "unknown"}}, env)
	assert.Error(t, err)
}

func TestPlugin_OnTransition_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.onTransition(ctx, eval, []core.Value{}, env)
	assert.Error(t, err)

	_, err = p.onTransition(ctx, eval, []core.Value{core.Keyword{V: "unknown"}, core.Lambda{}}, env)
	assert.Error(t, err)

	_, err = p.onTransition(ctx, eval, []core.Value{core.Keyword{V: "test"}, core.String{V: "not-lambda"}}, env)
	assert.Error(t, err)
}

func TestPlugin_Reachable_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.reachable(ctx, eval, []core.Value{}, env)
	assert.Error(t, err)

	_, err = p.reachable(ctx, eval, []core.Value{core.Keyword{V: "unknown"}}, env)
	assert.Error(t, err)
}

func TestPlugin_StateMachine_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.stateMachine(ctx, eval, []core.Value{}, env)
	assert.Error(t, err)

	_, err = p.stateMachine(ctx, eval, []core.Value{core.String{V: "not-keyword"}}, env)
	assert.Error(t, err)

	_, err = p.stateMachine(ctx, eval, []core.Value{core.Keyword{V: "unknown"}}, env)
	assert.Error(t, err)
}

func TestPlugin_List_Errors(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	_, err := p.list(ctx, eval, []core.Value{core.Keyword{V: "extra"}}, env)
	assert.Error(t, err)
}

func TestPlugin_Transition_WithGuardError(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	guardLambda := core.Lambda{
		Params: []core.Symbol{{V: "from"}},
		Body:   []core.Value{core.Symbol{V: "undefined-var"}},
		Env:    env,
	}

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})

	states := core.NewHashMap()
	pendingState := core.NewHashMap()
	pendingOn := core.NewHashMap()
	pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "confirm"}, core.NewHashMap())
	transitionMap, _ := pendingOn.Get(core.Keyword{V: "confirm"})
	if tm, ok := transitionMap.(*core.HashMap); ok {
		tm, _ = tm.Assoc(core.Keyword{V: "target"}, core.Keyword{V: "confirmed"})
		tm, _ = tm.Assoc(core.Keyword{V: "guard"}, guardLambda)
		pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "confirm"}, tm)
	}
	pendingState, _ = pendingState.Assoc(core.Keyword{V: "on"}, pendingOn)
	states, _ = states.Assoc(core.Keyword{V: "pending"}, pendingState)

	def, _ = def.Assoc(core.Keyword{V: "states"}, states)

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "order"}, def}, env)
	require.NoError(t, err)
}

func TestStateMachine_TransitionWithEval_GuardError(t *testing.T) {
	env := core.NewEnv(nil)

	guardLambda := core.Lambda{
		Params: []core.Symbol{{V: "from"}},
		Body:   []core.Value{core.Symbol{V: "undefined-var"}},
		Env:    env,
	}

	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed", Guard: guardLambda},
				},
			},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	eval := core.NewEvaluator()
	ctx := context.Background()

	result, err := sm.TransitionWithEval(ctx, Event("confirm"), nil, eval, env)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Contains(t, result.Error, "guard error")
}

func TestStateMachine_Serialize_Version(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("confirmed"),
		states: map[State]*StateConfig{
			"pending":   {On: map[Event]Transition{}},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	data, err := sm.Serialize()
	require.NoError(t, err)

	var state PersistentState
	err = json.Unmarshal(data, &state)
	require.NoError(t, err)
	assert.Equal(t, 1, state.Version)
	assert.Equal(t, "test", state.MachineID)
	assert.Equal(t, "confirmed", state.Current)
}

func TestStateMachine_Deserialize_InvalidVersion(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
	}

	invalidData := `{"version": 2, "machine_id": "test", "current": "confirmed", "at": "2024-01-01T00:00:00Z"}`
	err := sm.Deserialize([]byte(invalidData))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported state version")
}

func TestStateMachine_Deserialize_InvalidJSON(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
	}

	err := sm.Deserialize([]byte("invalid json"))
	assert.Error(t, err)
}

func TestStateMachine_Persistence_RoundTrip(t *testing.T) {
	sm := &StateMachine{
		id:      "order-123",
		initial: State("pending"),
		current: State("shipped"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
					"cancel":  {Target: "cancelled"},
				},
			},
			"confirmed": {
				On: map[Event]Transition{
					"ship": {Target: "shipped"},
				},
			},
			"shipped":   {On: map[Event]Transition{}},
			"cancelled": {On: map[Event]Transition{}},
		},
	}

	data, err := sm.Serialize()
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	sm2 := &StateMachine{
		id:      "order-123",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending":   {On: map[Event]Transition{}},
			"confirmed": {On: map[Event]Transition{}},
			"shipped":   {On: map[Event]Transition{}},
			"cancelled": {On: map[Event]Transition{}},
		},
	}

	err = sm2.Deserialize(data)
	require.NoError(t, err)
	assert.Equal(t, State("shipped"), sm2.Current())

	valid := sm2.CanTransition(Event("ship"))
	assert.False(t, valid)
}

func TestStateMachine_Concurrent_Transitions(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("idle"),
		current: State("idle"),
		states: map[State]*StateConfig{
			"idle": {
				On: map[Event]Transition{
					"start": {Target: "running"},
				},
			},
			"running": {
				On: map[Event]Transition{
					"stop": {Target: "idle"},
				},
			},
		},
	}

	var wg sync.WaitGroup
	errCount := int32(0)
	successCount := int32(0)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				result, err := sm.Transition(Event("start"), nil)
				if err != nil {
					atomic.AddInt32(&errCount, 1)
				} else if result.Valid {
					atomic.AddInt32(&successCount, 1)
				}
				sm.Reset()
			}
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(0), errCount)
	assert.Greater(t, successCount, int32(0))
}

func TestStateMachine_Concurrent_Serialize(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
				},
			},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	var wg sync.WaitGroup
	errors := make([]error, 0)
	var mu sync.Mutex

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_, err := sm.Transition(Event("confirm"), nil)
				if err != nil {
					mu.Lock()
					errors = append(errors, err)
					mu.Unlock()
				}
			} else {
				_, err := sm.Serialize()
				if err != nil {
					mu.Lock()
					errors = append(errors, err)
					mu.Unlock()
				}
			}
			sm.Reset()
		}(i)
	}

	wg.Wait()
	assert.Empty(t, errors)
}

func TestPlugin_Concurrent_MultipleMachines(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		def := core.NewHashMap()
		def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "pending"})

		states := core.NewHashMap()
		pendingState := core.NewHashMap()
		pendingOn := core.NewHashMap()
		pendingOn, _ = pendingOn.Assoc(core.Keyword{V: "confirm"}, core.Keyword{V: "confirmed"})
		pendingState, _ = pendingState.Assoc(core.Keyword{V: "on"}, pendingOn)
		states, _ = states.Assoc(core.Keyword{V: "pending"}, pendingState)

		confirmedState := core.NewHashMap()
		confirmedState, _ = confirmedState.Assoc(core.Keyword{V: "on"}, core.NewHashMap())
		states, _ = states.Assoc(core.Keyword{V: "confirmed"}, confirmedState)

		def, _ = def.Assoc(core.Keyword{V: "states"}, states)

		_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: fmt.Sprintf("machine-%d", i)}, def}, env)
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	errCount := int32(0)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			machineID := fmt.Sprintf("machine-%d", idx%5)
			_, err := p.transition(ctx, eval, []core.Value{core.Keyword{V: machineID}, core.Keyword{V: "confirm"}}, env)
			if err != nil {
				atomic.AddInt32(&errCount, 1)
			}
			p.reset(ctx, eval, []core.Value{core.Keyword{V: machineID}}, env)
		}(i)
	}

	wg.Wait()
	assert.Equal(t, int32(0), errCount)
}

func TestStateMachine_FullLifecycle(t *testing.T) {
	sm := &StateMachine{
		id:      "order",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
					"cancel":  {Target: "cancelled"},
				},
			},
			"confirmed": {
				On: map[Event]Transition{
					"ship":   {Target: "shipped"},
					"cancel": {Target: "cancelled"},
				},
			},
			"shipped": {
				On: map[Event]Transition{
					"deliver": {Target: "delivered"},
				},
			},
			"delivered": {On: map[Event]Transition{}},
			"cancelled": {On: map[Event]Transition{}},
		},
	}

	assert.Equal(t, State("pending"), sm.Current())
	assert.Equal(t, State("pending"), sm.Initial())
	assert.True(t, sm.CanTransition(Event("confirm")))
	assert.False(t, sm.CanTransition(Event("ship")))

	reachable := sm.Reachable(State("pending"))
	assert.Len(t, reachable, 2)
	assert.Contains(t, reachable, State("confirmed"))
	assert.Contains(t, reachable, State("cancelled"))

	result, err := sm.Transition(Event("confirm"), nil)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, State("confirmed"), sm.Current())

	assert.False(t, sm.CanTransition(Event("confirm")))
	assert.True(t, sm.CanTransition(Event("ship")))

	result, err = sm.Transition(Event("ship"), nil)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, State("shipped"), sm.Current())

	result, err = sm.Transition(Event("deliver"), nil)
	require.NoError(t, err)
	assert.True(t, result.Valid)
	assert.Equal(t, State("delivered"), sm.Current())

	assert.False(t, sm.CanTransition(Event("ship")))

	resetState := sm.Reset()
	assert.Equal(t, State("pending"), resetState)
	assert.Equal(t, State("pending"), sm.Current())
}

func TestStateMachine_InvalidTransition_ReturnsError(t *testing.T) {
	sm := &StateMachine{
		id:      "test",
		initial: State("pending"),
		current: State("pending"),
		states: map[State]*StateConfig{
			"pending": {
				On: map[Event]Transition{
					"confirm": {Target: "confirmed"},
				},
			},
			"confirmed": {On: map[Event]Transition{}},
		},
	}

	result, err := sm.Transition(Event("invalid-event"), nil)
	require.NoError(t, err)
	assert.False(t, result.Valid)
	assert.Equal(t, State("pending"), result.State)
	assert.Contains(t, result.Error, "not valid in state")
}

func TestStateMachine_ID(t *testing.T) {
	sm := &StateMachine{
		id:      "my-machine",
		initial: State("pending"),
		current: State("pending"),
	}
	assert.Equal(t, "my-machine", sm.ID())
}

func TestPlugin_Acceptance_FullWorkflow(t *testing.T) {
	p := New()
	env := core.NewEnv(nil)
	require.NoError(t, p.Init(env))

	eval := core.NewEvaluator()
	ctx := context.Background()

	def := core.NewHashMap()
	def, _ = def.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: "draft"})

	states := core.NewHashMap()

	draftState := core.NewHashMap()
	draftOn := core.NewHashMap()
	draftOn, _ = draftOn.Assoc(core.Keyword{V: "submit"}, core.Keyword{V: "submitted"})
	draftState, _ = draftState.Assoc(core.Keyword{V: "on"}, draftOn)
	states, _ = states.Assoc(core.Keyword{V: "draft"}, draftState)

	submittedState := core.NewHashMap()
	submittedOn := core.NewHashMap()
	submittedOn, _ = submittedOn.Assoc(core.Keyword{V: "approve"}, core.Keyword{V: "approved"})
	submittedOn, _ = submittedOn.Assoc(core.Keyword{V: "reject"}, core.Keyword{V: "rejected"})
	submittedState, _ = submittedState.Assoc(core.Keyword{V: "on"}, submittedOn)
	states, _ = states.Assoc(core.Keyword{V: "submitted"}, submittedState)

	approvedState := core.NewHashMap()
	approvedState, _ = approvedState.Assoc(core.Keyword{V: "on"}, core.NewHashMap())
	states, _ = states.Assoc(core.Keyword{V: "approved"}, approvedState)

	rejectedState := core.NewHashMap()
	rejectedState, _ = rejectedState.Assoc(core.Keyword{V: "on"}, core.NewHashMap())
	states, _ = states.Assoc(core.Keyword{V: "rejected"}, rejectedState)

	def, _ = def.Assoc(core.Keyword{V: "states"}, states)

	_, err := p.create(ctx, eval, []core.Value{core.Keyword{V: "document"}, def}, env)
	require.NoError(t, err)

	current, err := p.currentState(ctx, eval, []core.Value{core.Keyword{V: "document"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Keyword{V: "draft"}, current)

	valid, err := p.valid(ctx, eval, []core.Value{core.Keyword{V: "document"}, core.Keyword{V: "submit"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Bool{V: true}, valid)

	valid, err = p.valid(ctx, eval, []core.Value{core.Keyword{V: "document"}, core.Keyword{V: "approve"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Bool{V: false}, valid)

	_, err = p.transition(ctx, eval, []core.Value{core.Keyword{V: "document"}, core.Keyword{V: "submit"}}, env)
	require.NoError(t, err)

	current, err = p.currentState(ctx, eval, []core.Value{core.Keyword{V: "document"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Keyword{V: "submitted"}, current)

	reachable, err := p.reachable(ctx, eval, []core.Value{core.Keyword{V: "document"}}, env)
	require.NoError(t, err)
	list, ok := reachable.(core.List)
	require.True(t, ok)
	assert.Len(t, list.Items, 2)

	_, err = p.transition(ctx, eval, []core.Value{core.Keyword{V: "document"}, core.Keyword{V: "approve"}}, env)
	require.NoError(t, err)

	current, err = p.currentState(ctx, eval, []core.Value{core.Keyword{V: "document"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Keyword{V: "approved"}, current)

	_, err = p.reset(ctx, eval, []core.Value{core.Keyword{V: "document"}}, env)
	require.NoError(t, err)

	current, err = p.currentState(ctx, eval, []core.Value{core.Keyword{V: "document"}}, env)
	require.NoError(t, err)
	assert.Equal(t, core.Keyword{V: "draft"}, current)
}
