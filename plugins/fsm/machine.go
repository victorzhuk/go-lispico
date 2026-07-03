package fsm

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

func (sm *StateMachine) ID() string {
	return sm.id
}

func (sm *StateMachine) Current() State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.current
}

func (sm *StateMachine) Initial() State {
	return sm.initial
}

func (sm *StateMachine) Transition(event Event, ctx map[string]any) (TransitionResult, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cfg, ok := sm.states[sm.current]
	if !ok {
		return TransitionResult{}, fmt.Errorf("invalid current state: %s", sm.current)
	}

	trans, ok := cfg.On[event]
	if !ok {
		return TransitionResult{
			State: sm.current,
			From:  sm.current,
			To:    sm.current,
			Valid: false,
			Error: fmt.Sprintf("event %s not valid in state %s", event, sm.current),
		}, nil
	}

	from := sm.current
	sm.current = trans.Target

	return TransitionResult{
		State: sm.current,
		From:  from,
		To:    sm.current,
		Valid: true,
	}, nil
}

// TransitionWithEval holds sm.mu across the guard/action Lambda invocations, so
// a guard or action must not call back into this machine (e.g. fsm/transition on
// the same id) — the mutex is not reentrant and would deadlock.
func (sm *StateMachine) TransitionWithEval(goCtx context.Context, event Event, transCtx map[string]any, eval core.Evaluator, env *core.Env) (TransitionResult, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	cfg, ok := sm.states[sm.current]
	if !ok {
		return TransitionResult{}, fmt.Errorf("invalid current state: %s", sm.current)
	}

	trans, ok := cfg.On[event]
	if !ok {
		return TransitionResult{
			State: sm.current,
			From:  sm.current,
			To:    sm.current,
			Valid: false,
			Error: fmt.Sprintf("event %s not valid in state %s", event, sm.current),
		}, nil
	}

	if trans.Guard != nil {
		if guard, ok := trans.Guard.(core.Lambda); ok {
			args := []core.Value{
				core.Keyword{V: string(sm.current)},
				core.Keyword{V: string(trans.Target)},
				core.Keyword{V: string(event)},
			}
			result, err := eval.Apply(goCtx, guard, args, env)
			if err != nil {
				return TransitionResult{
					State: sm.current,
					From:  sm.current,
					To:    sm.current,
					Valid: false,
					Error: fmt.Sprintf("guard error: %s", err),
				}, nil
			}
			if !isTruthy(result) {
				return TransitionResult{
					State: sm.current,
					From:  sm.current,
					To:    sm.current,
					Valid: false,
					Error: "guard returned false",
				}, nil
			}
		}
	}

	from := sm.current
	sm.current = trans.Target

	if trans.Action != nil {
		if action, ok := trans.Action.(core.Lambda); ok {
			args := []core.Value{
				core.Keyword{V: string(from)},
				core.Keyword{V: string(trans.Target)},
				core.Keyword{V: string(event)},
			}
			_, _ = eval.Apply(goCtx, action, args, env)
		}
	}

	return TransitionResult{
		State: sm.current,
		From:  from,
		To:    sm.current,
		Valid: true,
	}, nil
}

func isTruthy(v core.Value) bool {
	if _, ok := v.(core.Nil); ok {
		return false
	}
	if b, ok := v.(core.Bool); ok {
		return b.V
	}
	return true
}

func (sm *StateMachine) CanTransition(event Event) bool {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	cfg, ok := sm.states[sm.current]
	if !ok {
		return false
	}

	_, ok = cfg.On[event]
	return ok
}

func (sm *StateMachine) Reset() State {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.current = sm.initial
	return sm.current
}

func (sm *StateMachine) Reachable(fromState State) []State {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	cfg, ok := sm.states[fromState]
	if !ok {
		return nil
	}

	seen := make(map[State]bool)
	var result []State
	for _, trans := range cfg.On {
		if !seen[trans.Target] {
			seen[trans.Target] = true
			result = append(result, trans.Target)
		}
	}
	return result
}

func (sm *StateMachine) addCallback(l core.Lambda, eval core.Evaluator, env *core.Env) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.callbacks = append(sm.callbacks, callback{lambda: l, eval: eval, env: env})
}

func (sm *StateMachine) notify(ctx context.Context, ev StateEvent) {
	sm.mu.RLock()
	cbs := make([]callback, len(sm.callbacks))
	copy(cbs, sm.callbacks)
	sm.mu.RUnlock()

	args := []core.Value{
		core.String{V: ev.MachineID},
		core.Keyword{V: string(ev.From)},
		core.Keyword{V: string(ev.To)},
	}

	for _, cb := range cbs {
		_, _ = cb.eval.Apply(ctx, cb.lambda, args, cb.env)
	}
}

func (sm *StateMachine) Serialize() ([]byte, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	state := PersistentState{
		Version:   1,
		MachineID: sm.id,
		Current:   string(sm.current),
		At:        time.Now(),
	}

	return json.Marshal(state)
}

func (sm *StateMachine) Deserialize(data []byte) error {
	var state PersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	if state.Version != 1 {
		return fmt.Errorf("unsupported state version: %d", state.Version)
	}

	sm.mu.Lock()
	sm.current = State(state.Current)
	sm.mu.Unlock()

	return nil
}
