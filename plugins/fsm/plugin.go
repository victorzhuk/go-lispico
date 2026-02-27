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
	}
}

func (p *Plugin) Init(env *core.Env) error {
	env.Set("fsm/create", core.GoFunc{
		Name: "fsm/create",
		Fn:   p.create,
	})

	env.Set("fsm/transition", core.GoFunc{
		Name: "fsm/transition",
		Fn:   p.transition,
	})

	env.Set("fsm/valid?", core.GoFunc{
		Name: "fsm/valid?",
		Fn:   p.valid,
	})

	env.Set("fsm/current-state", core.GoFunc{
		Name: "fsm/current-state",
		Fn:   p.currentState,
	})

	env.Set("fsm/reset", core.GoFunc{
		Name: "fsm/reset",
		Fn:   p.reset,
	})

	env.Set("fsm/on-transition", core.GoFunc{
		Name: "fsm/on-transition",
		Fn:   p.onTransition,
	})

	env.Set("fsm/reachable", core.GoFunc{
		Name: "fsm/reachable",
		Fn:   p.reachable,
	})

	env.Set("fsm/state-machine", core.GoFunc{
		Name: "fsm/state-machine",
		Fn:   p.stateMachine,
	})

	env.Set("fsm/list", core.GoFunc{
		Name: "fsm/list",
		Fn:   p.list,
	})

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

	if initVal, ok := def.Get(core.Keyword{V: "initial"}); ok {
		if initKw, ok := initVal.(core.Keyword); ok {
			sm.initial = State(initKw.V)
			sm.current = State(initKw.V)
		} else {
			return nil, fmt.Errorf("initial must be keyword")
		}
	}

	if statesVal, ok := def.Get(core.Keyword{V: "states"}); ok {
		if statesMap, ok := statesVal.(*core.HashMap); ok {
			statesMap.Each(func(stateKey, stateVal core.Value) {
				stateKw, ok := stateKey.(core.Keyword)
				if !ok {
					return
				}

				state := State(stateKw.V)
				cfg := &StateConfig{
					On: make(map[Event]Transition),
				}

				if stateDef, ok := stateVal.(*core.HashMap); ok {
					if onVal, ok := stateDef.Get(core.Keyword{V: "on"}); ok {
						if onMap, ok := onVal.(*core.HashMap); ok {
							onMap.Each(func(eventKey, targetVal core.Value) {
								eventKw, ok := eventKey.(core.Keyword)
								if !ok {
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
										guard = g
									}
									if a, ok := t.Get(core.Keyword{V: "action"}); ok {
										action = a
									}
								}

								cfg.On[event] = Transition{
									Target: target,
									Guard:  guard,
									Action: action,
								}
							})
						}
					}
				}

				sm.states[state] = cfg
			})
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
		p.broadcast(ctx, ev)
	}

	resMap := core.NewHashMap()
	resMap, _ = resMap.Assoc(core.Keyword{V: "state"}, core.Keyword{V: string(result.State)})
	resMap, _ = resMap.Assoc(core.Keyword{V: "valid"}, core.Bool{V: result.Valid})
	if result.Error != "" {
		resMap, _ = resMap.Assoc(core.Keyword{V: "error"}, core.String{V: result.Error})
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

	return core.Keyword{V: string(sm.Reset())}, nil
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

	return core.List{Items: items}, nil
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
	m, _ = m.Assoc(core.Keyword{V: "id"}, core.String{V: sm.id})
	m, _ = m.Assoc(core.Keyword{V: "initial"}, core.Keyword{V: string(sm.initial)})
	m, _ = m.Assoc(core.Keyword{V: "current"}, core.Keyword{V: string(sm.current)})

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

	return core.List{Items: ids}, nil
}

func (p *Plugin) broadcast(ctx context.Context, ev StateEvent) {
	select {
	case p.events <- ev:
	default:
	}

	p.mu.RLock()
	sm, ok := p.machines[ev.MachineID]
	p.mu.RUnlock()

	if ok {
		sm.notify(ctx, ev)
	}
}

func (p *Plugin) Events() <-chan StateEvent {
	return p.events
}
