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
	if !sm.inTransition.CompareAndSwap(false, true) {
		return TransitionResult{}, fmt.Errorf("fsm: transition already in progress on machine %s", sm.id)
	}
	defer sm.inTransition.Store(false)

	sm.mu.Lock()

	cfg, ok := sm.states[sm.current]
	if !ok {
		sm.mu.Unlock()
		return TransitionResult{}, fmt.Errorf("invalid current state: %s", sm.current)
	}

	trans, ok := cfg.On[event]
	if !ok {
		res := TransitionResult{
			State: sm.current,
			From:  sm.current,
			To:    sm.current,
			Valid: false,
			Error: fmt.Sprintf("event %s not valid in state %s", event, sm.current),
		}
		sm.mu.Unlock()
		return res, nil
	}

	from := sm.current
	sm.current = trans.Target
	sm.mu.Unlock()

	return TransitionResult{
		State:     trans.Target,
		From:      from,
		To:        trans.Target,
		Valid:     true,
		Committed: true,
	}, nil
}

// TransitionWithEval evaluates the guard and the action with sm.mu released, so
// a guard or action may call back into the machine (fsm/current-state, and even
// fsm/transition on other machines). Re-entrant transitions on the same machine
// are refused via the inTransition flag instead of deadlocking.
func (sm *StateMachine) TransitionWithEval(goCtx context.Context, event Event, transCtx map[string]any, eval core.Evaluator, env *core.Env) (TransitionResult, error) {
	if !sm.inTransition.CompareAndSwap(false, true) {
		return TransitionResult{}, fmt.Errorf("fsm: transition already in progress on machine %s", sm.id)
	}
	defer sm.inTransition.Store(false)

	sm.mu.Lock()
	cfg, ok := sm.states[sm.current]
	if !ok {
		sm.mu.Unlock()
		return TransitionResult{}, fmt.Errorf("invalid current state: %s", sm.current)
	}

	trans, ok := cfg.On[event]
	if !ok {
		res := TransitionResult{
			State: sm.current,
			From:  sm.current,
			To:    sm.current,
			Valid: false,
			Error: fmt.Sprintf("event %s not valid in state %s", event, sm.current),
		}
		sm.mu.Unlock()
		return res, nil
	}

	from := sm.current
	sm.mu.Unlock()

	if trans.Guard != nil {
		if guard, ok := trans.Guard.(core.Lambda); ok {
			args := []core.Value{
				core.Keyword{V: string(from)},
				core.Keyword{V: string(trans.Target)},
				core.Keyword{V: string(event)},
			}
			result, err := eval.Apply(goCtx, guard, args, env)
			if err != nil {
				return TransitionResult{
					State: from,
					From:  from,
					To:    from,
					Valid: false,
					Error: fmt.Sprintf("guard error: %s", err),
				}, nil
			}
			if !isTruthy(result) {
				return TransitionResult{
					State: from,
					From:  from,
					To:    from,
					Valid: false,
					Error: "guard returned false",
				}, nil
			}
		}
	}

	sm.mu.Lock()
	if sm.current != from {
		sm.mu.Unlock()
		return TransitionResult{Error: "state changed during guard evaluation"}, nil
	}
	sm.current = trans.Target
	sm.mu.Unlock()

	if trans.Action != nil {
		if action, ok := trans.Action.(core.Lambda); ok {
			args := []core.Value{
				core.Keyword{V: string(from)},
				core.Keyword{V: string(trans.Target)},
				core.Keyword{V: string(event)},
			}
			if _, err := eval.Apply(goCtx, action, args, env); err != nil {
				return TransitionResult{
					State:     trans.Target,
					From:      from,
					To:        trans.Target,
					Valid:     false,
					Committed: true,
					Error:     "action: " + err.Error(),
				}, nil
			}
		}
	}

	return TransitionResult{
		State:     trans.Target,
		From:      from,
		To:        trans.Target,
		Valid:     true,
		Committed: true,
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

func (sm *StateMachine) Reset() (State, error) {
	if !sm.inTransition.CompareAndSwap(false, true) {
		return State(""), fmt.Errorf("fsm: transition already in progress on machine %s", sm.id)
	}
	defer sm.inTransition.Store(false)

	sm.mu.Lock()
	sm.current = sm.initial
	current := sm.current
	sm.mu.Unlock()
	return current, nil
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

func (sm *StateMachine) notify(ctx context.Context, ev StateEvent) error {
	sm.mu.RLock()
	cbs := make([]callback, len(sm.callbacks))
	copy(cbs, sm.callbacks)
	sm.mu.RUnlock()

	args := []core.Value{
		core.String{V: ev.MachineID},
		core.Keyword{V: string(ev.From)},
		core.Keyword{V: string(ev.To)},
	}

	var firstErr error
	for _, cb := range cbs {
		if _, err := cb.eval.Apply(ctx, cb.lambda, args, cb.env); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("callback: %s", err)
		}
	}
	return firstErr
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
	if !sm.inTransition.CompareAndSwap(false, true) {
		return fmt.Errorf("fsm: transition already in progress on machine %s", sm.id)
	}
	defer sm.inTransition.Store(false)

	var state PersistentState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}

	if state.Version != 1 {
		return fmt.Errorf("unsupported state version: %d", state.Version)
	}

	if state.MachineID != sm.id {
		return fmt.Errorf("snapshot machine id %q does not match %q", state.MachineID, sm.id)
	}

	if state.Current == "" {
		return fmt.Errorf("snapshot carries no current state")
	}

	sm.mu.RLock()
	_, ok := sm.states[State(state.Current)]
	sm.mu.RUnlock()
	if !ok {
		return fmt.Errorf("unknown state %q in snapshot", state.Current)
	}

	sm.mu.Lock()
	sm.current = State(state.Current)
	sm.mu.Unlock()

	return nil
}
