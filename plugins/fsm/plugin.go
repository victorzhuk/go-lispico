package fsm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/victorzhuk/go-lispico/core"
)

type Plugin struct {
	mu       sync.RWMutex
	machines map[string]*StateMachine
	events   chan StateEvent
}

func New() *Plugin {
	return &Plugin{
		machines: make(map[string]*StateMachine),
		events:   make(chan StateEvent, 100),
	}
}

func (p *Plugin) Name() string {
	return "fsm"
}

func (p *Plugin) Metadata() core.PluginMeta {
	return core.PluginMeta{
		Version:     "1.0.0",
		Description: "finite state machine plugin",
		Author:      "go-lispico team",
		Lifecycle:   "idle",
	}
}

func (p *Plugin) Init(env *core.Env) error {
	for _, fn := range []core.GoFunc{
		{Name: "fsm/create", Fn: p.create},
		{Name: "fsm/transition", Fn: p.transition},
		{Name: "fsm/valid?", Fn: p.valid},
		{Name: "fsm/current-state", Fn: p.currentState},
		{Name: "fsm/reset", Fn: p.reset},
		{Name: "fsm/on-transition", Fn: p.onTransition},
		{Name: "fsm/reachable", Fn: p.reachable},
		{Name: "fsm/state-machine", Fn: p.stateMachine},
		{Name: "fsm/list", Fn: p.list},
	} {
		if err := env.Set(fn.Name, fn); err != nil {
			return err
		}
	}
	return nil
}

func (p *Plugin) create(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("fsm/create: requires 2 arguments (id, definition)")
	}

	id, ok := args[0].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("fsm/create: first argument must be keyword")
	}

	def, ok := args[1].(*core.HashMap)
	if !ok {
		return nil, fmt.Errorf("fsm/create: second argument must be map")
	}

	sm, err := p.createMachine(id.V, def)
	if err != nil {
		return nil, fmt.Errorf("fsm/create: %w", err)
	}

	p.mu.Lock()
	p.machines[id.V] = sm
	p.mu.Unlock()

	return id, nil
}

func (p *Plugin) createMachine(id string, def *core.HashMap) (*StateMachine, error) {
	sm := &StateMachine{
		id:      id,
		states:  make(map[State]*StateConfig),
		initial: "",
		current: "",
	}

	var defKeyErr error
	def.Each(func(k, _ core.Value) {
		if _, ok := k.(core.Keyword); !ok && defKeyErr == nil {
			defKeyErr = fmt.Errorf("definition key must be a keyword")
		}
	})
	if defKeyErr != nil {
		return nil, defKeyErr
	}

	if initVal, ok := def.Get(core.Keyword{V: "initial"}); ok {
		if initKw, ok := initVal.(core.Keyword); ok {
			sm.initial = State(initKw.V)
			sm.current = State(initKw.V)
		} else {
			return nil, fmt.Errorf("initial must be keyword")
		}
	}

	if statesVal, ok := def.Get(core.Keyword{V: "states"}); ok {
		statesMap, ok := statesVal.(*core.HashMap)
		if !ok {
			return nil, fmt.Errorf("states must be a map")
		}

		var defErr error
		statesMap.Each(func(stateKey, stateVal core.Value) {
			if defErr != nil {
				return
			}
			stateKw, ok := stateKey.(core.Keyword)
			if !ok {
				defErr = fmt.Errorf("state key must be a keyword")
				return
			}

			state := State(stateKw.V)
			cfg := &StateConfig{
				On: make(map[Event]Transition),
			}

			if stateDef, ok := stateVal.(*core.HashMap); ok {
				if onVal, ok := stateDef.Get(core.Keyword{V: "on"}); ok {
					onMap, ok := onVal.(*core.HashMap)
					if !ok {
						defErr = fmt.Errorf("on must be a map")
						return
					}
					var onErr error
					onMap.Each(func(eventKey, targetVal core.Value) {
						if onErr != nil {
							return
						}
						eventKw, ok := eventKey.(core.Keyword)
						if !ok {
							onErr = fmt.Errorf("definition key must be a keyword")
							return
						}

						event := Event(eventKw.V)
						var target State
						var guard, action core.Value

						switch t := targetVal.(type) {
						case core.Keyword:
							target = State(t.V)
						case *core.HashMap:
							if tv, ok := t.Get(core.Keyword{V: "target"}); ok {
								if tkw, ok := tv.(core.Keyword); ok {
									target = State(tkw.V)
								}
							}
							if g, ok := t.Get(core.Keyword{V: "guard"}); ok {
								if _, isNil := g.(core.Nil); !isNil && g != nil {
									if _, ok := g.(core.Lambda); !ok {
										onErr = fmt.Errorf("guard must be a function")
										return
									}
									guard = g
								}
							}
							if a, ok := t.Get(core.Keyword{V: "action"}); ok {
								if _, isNil := a.(core.Nil); !isNil && a != nil {
									if _, ok := a.(core.Lambda); !ok {
										onErr = fmt.Errorf("action must be a function")
										return
									}
									action = a
								}
							}
						}

						cfg.On[event] = Transition{
							Target: target,
							Guard:  guard,
							Action: action,
						}
					})
					if onErr != nil {
						defErr = onErr
					}
				}
			}

			if defErr == nil {
				sm.states[state] = cfg
			}
		})
		if defErr != nil {
			return nil, defErr
		}
	}

	if len(sm.states) == 0 {
		return nil, fmt.Errorf("state machine %q declares no states", id)
	}
	if sm.initial == "" {
		return nil, fmt.Errorf("state machine %q declares no initial state", id)
	}
	if _, ok := sm.states[sm.initial]; !ok {
		return nil, fmt.Errorf("initial state %q is not declared", sm.initial)
	}
	for _, cfg := range sm.states {
		for _, trans := range cfg.On {
			if _, ok := sm.states[trans.Target]; !ok {
				return nil, fmt.Errorf("transition target %q is not a declared state", trans.Target)
			}
		}
	}

	return sm, nil
}

func (p *Plugin) transition(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 2 || len(args) > 3 {
		return nil, fmt.Errorf("fsm/transition: requires 2-3 arguments")
	}

	var sm *StateMachine

	switch v := args[0].(type) {
	case core.Keyword:
		p.mu.RLock()
		m, ok := p.machines[v.V]
		p.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("fsm/transition: unknown machine %s", v.V)
		}
		sm = m
	default:
		return nil, fmt.Errorf("fsm/transition: first argument must be keyword")
	}

	event, ok := args[1].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("fsm/transition: event must be keyword")
	}

	var transCtx map[string]any
	if len(args) == 3 {
		if ctxMap, ok := args[2].(*core.HashMap); ok {
			transCtx = make(map[string]any)
			ctxMap.Each(func(k, v core.Value) {
				transCtx[k.String()] = v.String()
			})
		}
	}

	result, err := sm.TransitionWithEval(ctx, Event(event.V), transCtx, eval, env)
	if err != nil {
		return nil, err
	}

	if result.Valid {
		ev := StateEvent{
			MachineID: sm.id,
			From:      result.From,
			To:        result.To,
			At:        time.Now(),
			Context:   transCtx,
		}
		if err := p.broadcast(ctx, ev); err != nil {
			if result.Error != "" {
				result.Error = result.Error + "; " + err.Error()
			} else {
				result.Error = err.Error()
			}
		}
	}

	resMap := core.NewHashMap()
	resMap, _, _ = resMap.Assoc(core.Keyword{V: "state"}, core.Keyword{V: string(result.State)})
	resMap, _, _ = resMap.Assoc(core.Keyword{V: "valid"}, core.Bool{V: result.Valid})
	if result.Error != "" {
		resMap, _, _ = resMap.Assoc(core.Keyword{V: "error"}, core.String{V: result.Error})
	}

	return resMap, nil
}

func (p *Plugin) valid(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("fsm/valid?: requires 2 arguments")
	}

	var sm *StateMachine

	if kw, ok := args[0].(core.Keyword); ok {
		p.mu.RLock()
		m, ok := p.machines[kw.V]
		p.mu.RUnlock()
		if !ok {
			return core.Bool{V: false}, nil
		}
		sm = m
	} else {
		return nil, fmt.Errorf("fsm/valid?: first argument must be keyword")
	}

	event, ok := args[1].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("fsm/valid?: event must be keyword")
	}

	return core.Bool{V: sm.CanTransition(Event(event.V))}, nil
}

func (p *Plugin) currentState(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("fsm/current-state: requires 1 argument")
	}

	kw, ok := args[0].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("fsm/current-state: argument must be keyword")
	}

	p.mu.RLock()
	sm, ok := p.machines[kw.V]
	p.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("fsm/current-state: unknown machine %s", kw.V)
	}

	return core.Keyword{V: string(sm.Current())}, nil
}

func (p *Plugin) reset(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("fsm/reset: requires 1 argument")
	}

	kw, ok := args[0].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("fsm/reset: argument must be keyword")
	}

	p.mu.RLock()
	sm, ok := p.machines[kw.V]
	p.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("fsm/reset: unknown machine %s", kw.V)
	}

	state, err := sm.Reset()
	if err != nil {
		return nil, fmt.Errorf("fsm/reset: %w", err)
	}
	return core.Keyword{V: string(state)}, nil
}

func (p *Plugin) onTransition(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("fsm/on-transition: requires 2 arguments")
	}

	var sm *StateMachine

	if kw, ok := args[0].(core.Keyword); ok {
		p.mu.RLock()
		m, ok := p.machines[kw.V]
		p.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("fsm/on-transition: unknown machine %s", kw.V)
		}
		sm = m
	} else {
		return nil, fmt.Errorf("fsm/on-transition: first argument must be keyword")
	}

	lambda, ok := args[1].(core.Lambda)
	if !ok {
		return nil, fmt.Errorf("fsm/on-transition: callback must be function")
	}

	sm.addCallback(lambda, eval, env)

	return core.Nil{}, nil
}

func (p *Plugin) reachable(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 || len(args) > 2 {
		return nil, fmt.Errorf("fsm/reachable: requires 1-2 arguments")
	}

	var sm *StateMachine

	if kw, ok := args[0].(core.Keyword); ok {
		p.mu.RLock()
		m, ok := p.machines[kw.V]
		p.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("fsm/reachable: unknown machine %s", kw.V)
		}
		sm = m
	} else {
		return nil, fmt.Errorf("fsm/reachable: first argument must be keyword")
	}

	fromState := sm.Current()
	if len(args) == 2 {
		if kw, ok := args[1].(core.Keyword); ok {
			fromState = State(kw.V)
		}
	}

	states := sm.Reachable(fromState)
	items := make([]core.Value, len(states))
	for i, s := range states {
		items[i] = core.Keyword{V: string(s)}
	}

	return core.NewList(items), nil
}

func (p *Plugin) stateMachine(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 1 {
		return nil, fmt.Errorf("fsm/state-machine: requires 1 argument")
	}

	kw, ok := args[0].(core.Keyword)
	if !ok {
		return nil, fmt.Errorf("fsm/state-machine: argument must be keyword")
	}

	p.mu.RLock()
	sm, ok := p.machines[kw.V]
	p.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("fsm/state-machine: unknown machine %s", kw.V)
	}

	m := core.NewHashMap()
	m, _, _ = m.Assoc(core.Keyword{V: "id"}, core.String{V: sm.ID()})
	m, _, _ = m.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: string(sm.Initial())})
	m, _, _ = m.Assoc(core.Keyword{V: "current"}, core.Keyword{V: string(sm.Current())})

	return m, nil
}

func (p *Plugin) list(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) != 0 {
		return nil, fmt.Errorf("fsm/list: takes no arguments")
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	var ids []core.Value
	for id := range p.machines {
		ids = append(ids, core.String{V: id})
	}

	return core.NewList(ids), nil
}

func (p *Plugin) broadcast(ctx context.Context, ev StateEvent) error {
	select {
	case p.events <- ev:
	default:
	}

	p.mu.RLock()
	sm, ok := p.machines[ev.MachineID]
	p.mu.RUnlock()

	if ok {
		return sm.notify(ctx, ev)
	}
	return nil
}

func (p *Plugin) Events() <-chan StateEvent {
	return p.events
}
