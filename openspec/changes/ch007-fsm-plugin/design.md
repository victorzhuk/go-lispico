# Design Document: FSM Plugin

**Change ID:** 007-fsm-plugin  
**Status:** Design  
**Created:** 2026-02-23  
**Author:** AI Assistant

---

## 1. Plugin Structure

```go
package fsm

import (
    "context"
    "encoding/json"
    "fmt"
    "sync"
    "time"
    
    "github.com/victorzhuk/go-lispico/core"
)

// State represents a state machine state
type State string

func (s State) String() string { return string(s) }

// Event represents a transition event
type Event string

func (e Event) String() string { return string(e) }

type Plugin struct {
    mu       sync.RWMutex
    machines map[string]*StateMachine
    events   chan StateEvent
}

type StateMachine struct {
    mu      sync.RWMutex
    id      string
    initial State
    current State
    states  map[State]*StateConfig
    
    // Event subscribers
    callbacks []func(StateEvent)
}

type StateConfig struct {
    On map[Event]Transition
}

type Transition struct {
    Target State
    Guard  core.Value // Lambda or nil
    Action core.Value // Lambda or nil
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
        Description: "Finite state machine plugin",
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

// fsm/create registers a new state machine by keyword ID with a definition map.
// Example: (fsm/create :order {:initial :pending :states {:pending {:on {:confirm :confirmed}}}})
func (p *Plugin) create(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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

    machine, err := p.createMachine(id.V, def)
    if err != nil {
        return nil, fmt.Errorf("fsm/create: %w", err)
    }

    p.mu.Lock()
    p.machines[id.V] = machine
    p.mu.Unlock()

    return id, nil
}
```

---

## 2. Machine Creation

```go
func (p *Plugin) createMachine(id string, def *core.HashMap) (*StateMachine, error) {
    sm := &StateMachine{
        id:      id,
        states:  make(map[State]*StateConfig),
        initial: "",
        current: "",
    }
    
    // Get initial state
    if initVal, ok := def.Get(core.Keyword{V: "initial"}); ok {
        if initKw, ok := initVal.(core.Keyword); ok {
            sm.initial = State(initKw.V)
            sm.current = State(initKw.V)
        } else {
            return nil, fmt.Errorf("initial must be keyword")
        }
    }
    
    // Parse states
    if statesVal, ok := def.Get(core.Keyword{V: "states"}); ok {
        if statesMap, ok := statesVal.(*core.HashMap); ok {
            for stateKey, stateVal := range statesMap.M {
                stateKw, ok := stateKey.(core.Keyword)
                if !ok {
                    continue
                }
                
                state := State(stateKw.V)
                config := &StateConfig{
                    On: make(map[Event]Transition),
                }
                
                if stateDef, ok := stateVal.(*core.HashMap); ok {
                    if onVal, ok := stateDef.Get(core.Keyword{V: "on"}); ok {
                        if onMap, ok := onVal.(*core.HashMap); ok {
                            for eventKey, targetVal := range onMap.M {
                                eventKw, ok := eventKey.(core.Keyword)
                                if !ok {
                                    continue
                                }
                                
                                event := Event(eventKw.V)
                                
                                var target State
                                var guard, action core.Value = nil, nil
                                
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
                                
                                config.On[event] = Transition{
                                    Target: target,
                                    Guard:  guard,
                                    Action: action,
                                }
                            }
                        }
                    }
                }
                
                sm.states[state] = config
            }
        }
    }
    
    return sm, nil
}
```

---

## 3. Function Implementations

### fsm/transition

```go
func (p *Plugin) transition(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) < 2 || len(args) > 3 {
        return nil, fmt.Errorf("fsm/transition: requires 2-3 arguments")
    }
    
    // Get machine ID or machine object
    var machine *StateMachine
    
    switch v := args[0].(type) {
    case core.Keyword:
        // Look up by ID
        p.mu.RLock()
        m, ok := p.machines[v.V]
        p.mu.RUnlock()
        if !ok {
            return nil, fmt.Errorf("fsm/transition: unknown machine %s", v.V)
        }
        machine = m
    case *core.HashMap:
        // Machine definition - create temporary
        // In real implementation, would cache these
        return nil, fmt.Errorf("fsm/transition: machine must be registered first")
    default:
        return nil, fmt.Errorf("fsm/transition: first argument must be keyword")
    }
    
    // Get event
    event, ok := args[1].(core.Keyword)
    if !ok {
        return nil, fmt.Errorf("fsm/transition: event must be keyword")
    }
    
    // Get optional context
    var ctx map[string]any
    if len(args) == 3 {
        if ctxMap, ok := args[2].(*core.HashMap); ok {
            ctx = make(map[string]any)
            for k, v := range ctxMap.M {
                ctx[k.String()] = v.String()
            }
        }
    }
    
    // Execute transition
    result, err := machine.Transition(Event(event.V), ctx)
    if err != nil {
        return nil, err
    }
    
    // Broadcast event
    if result.Valid {
        ev := StateEvent{
            MachineID: machine.id,
            From:      result.From,
            To:        result.To,
            At:        time.Now(),
            Context:   ctx,
        }
        p.broadcast(ev)
    }
    
    // Build result map
    resMap := core.NewHashMap()
    resMap.M[core.Keyword{V: "state"}] = core.Keyword{V: string(result.State)}
    resMap.M[core.Keyword{V: "valid"}] = core.Bool{V: result.Valid}
    if result.Error != "" {
        resMap.M[core.Keyword{V: "error"}] = core.String{V: result.Error}
    }
    
    return resMap, nil
}

type TransitionResult struct {
    State  State
    From   State
    To     State
    Valid  bool
    Error  string
}

func (sm *StateMachine) Transition(event Event, ctx map[string]any) (TransitionResult, error) {
    sm.mu.Lock()
    defer sm.mu.Unlock()
    
    // Get current state config
    config, ok := sm.states[sm.current]
    if !ok {
        return TransitionResult{}, fmt.Errorf("invalid current state: %s", sm.current)
    }
    
    // Find transition
    transition, ok := config.On[event]
    if !ok {
        return TransitionResult{
            State: sm.current,
            From:  sm.current,
            To:    sm.current,
            Valid: false,
            Error: fmt.Sprintf("event %s not valid in state %s", event, sm.current),
        }, nil
    }
    
    // Evaluate guard (simplified - would call lambda in real impl)
    // if transition.Guard != nil { ... }
    
    from := sm.current
    sm.current = transition.Target
    
    // Execute action (simplified)
    // if transition.Action != nil { ... }
    
    return TransitionResult{
        State: sm.current,
        From:  from,
        To:    sm.current,
        Valid: true,
    }, nil
}
```

### fsm/valid?

```go
func (p *Plugin) valid(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("fsm/valid?: requires 2 arguments")
    }
    
    // Get machine
    var machine *StateMachine
    
    if kw, ok := args[0].(core.Keyword); ok {
        p.mu.RLock()
        m, ok := p.machines[kw.V]
        p.mu.RUnlock()
        if !ok {
            return core.Bool{V: false}, nil
        }
        machine = m
    } else {
        return nil, fmt.Errorf("fsm/valid?: first argument must be keyword")
    }
    
    // Get event
    event, ok := args[1].(core.Keyword)
    if !ok {
        return nil, fmt.Errorf("fsm/valid?: event must be keyword")
    }
    
    // Check if valid
    machine.mu.RLock()
    defer machine.mu.RUnlock()
    
    config, ok := machine.states[machine.current]
    if !ok {
        return core.Bool{V: false}, nil
    }
    
    _, ok = config.On[Event(event.V)]
    return core.Bool{V: ok}, nil
}
```

### fsm/current-state

```go
func (p *Plugin) currentState(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("fsm/current-state: requires 1 argument")
    }
    
    kw, ok := args[0].(core.Keyword)
    if !ok {
        return nil, fmt.Errorf("fsm/current-state: argument must be keyword")
    }
    
    p.mu.RLock()
    machine, ok := p.machines[kw.V]
    p.mu.RUnlock()
    
    if !ok {
        return nil, fmt.Errorf("fsm/current-state: unknown machine %s", kw.V)
    }
    
    machine.mu.RLock()
    current := machine.current
    machine.mu.RUnlock()
    
    return core.Keyword{V: string(current)}, nil
}
```

### fsm/reset

```go
func (p *Plugin) reset(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("fsm/reset: requires 1 argument")
    }
    
    kw, ok := args[0].(core.Keyword)
    if !ok {
        return nil, fmt.Errorf("fsm/reset: argument must be keyword")
    }
    
    p.mu.RLock()
    machine, ok := p.machines[kw.V]
    p.mu.RUnlock()
    
    if !ok {
        return nil, fmt.Errorf("fsm/reset: unknown machine %s", kw.V)
    }
    
    machine.mu.Lock()
    machine.current = machine.initial
    current := machine.current
    machine.mu.Unlock()
    
    return core.Keyword{V: string(current)}, nil
}
```

### fsm/on-transition

```go
func (p *Plugin) onTransition(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 2 {
        return nil, fmt.Errorf("fsm/on-transition: requires 2 arguments")
    }
    
    // Get machine
    var machine *StateMachine
    
    if kw, ok := args[0].(core.Keyword); ok {
        p.mu.RLock()
        m, ok := p.machines[kw.V]
        p.mu.RUnlock()
        if !ok {
            return nil, fmt.Errorf("fsm/on-transition: unknown machine %s", kw.V)
        }
        machine = m
    } else {
        return nil, fmt.Errorf("fsm/on-transition: first argument must be keyword")
    }
    
    // Get callback
    callback, ok := args[1].(core.Lambda)
    if !ok {
        return nil, fmt.Errorf("fsm/on-transition: callback must be function")
    }
    
    // Register callback
    machine.mu.Lock()
    machine.callbacks = append(machine.callbacks, func(ev StateEvent) {
        // In real implementation, would call the lambda
        _ = callback
        _ = ev
    })
    machine.mu.Unlock()
    
    return core.Nil{}, nil
}
```

### fsm/reachable

```go
func (p *Plugin) reachable(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) < 1 || len(args) > 2 {
        return nil, fmt.Errorf("fsm/reachable: requires 1-2 arguments")
    }
    
    // Get machine
    var machine *StateMachine
    
    if kw, ok := args[0].(core.Keyword); ok {
        p.mu.RLock()
        m, ok := p.machines[kw.V]
        p.mu.RUnlock()
        if !ok {
            return nil, fmt.Errorf("fsm/reachable: unknown machine %s", kw.V)
        }
        machine = m
    } else {
        return nil, fmt.Errorf("fsm/reachable: first argument must be keyword")
    }
    
    // Get state (default to current)
    fromState := machine.current
    if len(args) == 2 {
        if kw, ok := args[1].(core.Keyword); ok {
            fromState = State(kw.V)
        }
    }
    
    machine.mu.RLock()
    config, ok := machine.states[fromState]
    machine.mu.RUnlock()
    
    if !ok {
        return core.List{}, nil
    }
    
    // Collect reachable states
    var states []core.Value
    seen := make(map[State]bool)
    
    for _, trans := range config.On {
        if !seen[trans.Target] {
            seen[trans.Target] = true
            states = append(states, core.Keyword{V: string(trans.Target)})
        }
    }
    
    return core.List{Items: states}, nil
}
```

### fsm/state-machine

```go
func (p *Plugin) stateMachine(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
    if len(args) != 1 {
        return nil, fmt.Errorf("fsm/state-machine: requires 1 argument")
    }
    
    // This function would return the machine definition
    // For now, return nil
    return core.Nil{}, nil
}
```

### fsm/list

```go
func (p *Plugin) list(ctx context.Context, eval *core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
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
```

---

## 4. Event Broadcasting

```go
func (p *Plugin) broadcast(ev StateEvent) {
    // Send to global channel
    select {
    case p.events <- ev:
    default:
        // Channel full, drop
    }
    
    // Find machine and notify callbacks
    p.mu.RLock()
    machine, ok := p.machines[ev.MachineID]
    p.mu.RUnlock()
    
    if ok {
        machine.mu.RLock()
        callbacks := make([]func(StateEvent), len(machine.callbacks))
        copy(callbacks, machine.callbacks)
        machine.mu.RUnlock()
        
        for _, cb := range callbacks {
            cb(ev)
        }
    }
}

// Events returns the event channel for Go subscriptions
func (p *Plugin) Events() <-chan StateEvent {
    return p.events
}
```

---

## 5. Persistence

```go
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
    defer sm.mu.Unlock()
    
    sm.current = State(state.Current)
    return nil
}
```

---

## 6. File Organization

```
plugins/fsm/
├── plugin.go         # Main plugin
├── machine.go        # State machine implementation
├── events.go         # Event broadcasting
├── persistence.go    # Serialization
└── fsm_test.go       # Test suite
```

---

**Next Step:** Create tasks document (03-tasks.md).
