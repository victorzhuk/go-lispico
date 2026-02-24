# Change Proposal: FSM Plugin

**Change ID:** 007-fsm-plugin  
**Status:** Proposed → Ready for Design  
**Created:** 2026-02-23  
**Author:** AI Assistant  
**Stakeholders:** go-lispico Core Team

---

## 1. Summary

Implement the Finite State Machine (FSM) plugin for declarative state machine definitions and execution. Supports typed state constants, validated transitions, event broadcasting, and state persistence.

**Key Characteristics:**
- Declarative state machine definition as Lisp maps
- Transition validation with explicit graph
- Event subscription (Go channels + Lisp callbacks)
- State persistence (JSON-serializable)
- Capability-gated: `fsm:subscribe`, `fsm:control` (capability enforcement deferred to future change)

---

## 2. Motivation

### Problem
Applications need state machines for:
- Order workflows (pending → paid → shipped → delivered)
- Approval processes (draft → submitted → approved → published)
- Connection state management (disconnected → connecting → connected)

Current approaches mix state logic in Go code, making them hard to modify.

### Solution
A Lisp-native FSM plugin:
- State machines defined as data (maps)
- Pure Lisp guards and actions
- Hot-reloadable state definitions
- Observable state changes

### Success Metrics
- State transitions validated correctly
- Invalid transitions blocked with clear errors
- Events delivered to subscribers in < 1ms
- State persistence round-trip works
- 1000+ transitions/second performance

---

## 3. Scope

### In Scope

**State Machine Definition**
- Map-based definition `{:initial :state :states {...}}`
- Transition table per state
- Optional guards (conditions)
- Optional actions (side effects)
- `deffsm` macro — define and register machine atomically

**Transition Operations**
- `fsm/transition` - attempt state change
- `fsm/valid?` - check if transition allowed
- `fsm/current-state` - get current state
- `fsm/reset` - reset to initial state

**Event Subscription**
- `fsm/on-transition` - register callback
- Go channel subscription for host
- Event context (from, to, timestamp, metadata)

**Introspection**
- `fsm/reachable` - get reachable states
- `fsm/state-machine` - get definition
- `fsm/list` - list all machines

**Persistence**
- Serialize state to JSON
- Deserialize and resume
- State version for migration

### Out of Scope

- Hierarchical states → Future (complex)
- Parallel regions → Future (complex)
- UML export → Future (visualization)
- State machine composition → Future

---

## 4. Functional Requirements

### State Machine Definition

| ID | Requirement | Priority |
|----|-------------|----------|
| F7.1 | Definition is a map with :initial and :states | P0 |
| F7.2 | States map contains transitions per event | P0 |
| F7.3 | Transitions specify target state | P0 |
| F7.4 | Optional :guard condition | P0 |
| F7.5 | Optional :action to execute | P1 |

### Transitions

| ID | Requirement | Priority |
|----|-------------|----------|
| F7.6 | transition validates and executes | P0 |
| F7.7 | Invalid transitions return error | P0 |
| F7.8 | valid? returns boolean without changing state | P0 |
| F7.9 | current-state returns current state keyword | P0 |
| F7.10 | reset returns to initial state | P0 |
| F7.11 | Guards evaluated before transition | P0 |
| F7.12 | Actions executed after transition | P0 |

### Events

| ID | Requirement | Priority |
|----|-------------|----------|
| F7.13 | on-transition registers callback | P0 |
| F7.14 | Callback receives event map | P0 |
| F7.15 | Go channel subscription for host | P0 |
| F7.16 | Events include from, to, timestamp | P0 |
| F7.17 | Events delivered synchronously | P0 |

### Persistence

| ID | Requirement | Priority |
|----|-------------|----------|
| F7.18 | State serializes to JSON | P0 |
| F7.19 | State deserializes from JSON | P0 |
| F7.20 | Version field for migrations | P1 |

---

## 5. Design Philosophy

### Data as Code

State machines are plain Lisp maps:

```lisp
(def order-fsm
  {:initial :pending
   :states
   {:pending   {:on {:pay    :paid
                     :cancel :cancelled}}
    :paid      {:on {:ship   :shipped
                     :refund :refunded}}
    :shipped   {:on {:deliver :delivered
                     :return  :returned}}
    :delivered {:on {:return :returned}}
    :returned  {}
    :refunded  {}
    :cancelled {}}})
```

### Explicit Transition Graph

Valid transitions are explicit, not implicit:

```lisp
; Valid: pending → paid
(fsm/transition order-fsm :pending :pay)
; => {:state :paid :valid true}

; Invalid: pending → shipped (must go through paid)
(fsm/transition order-fsm :pending :ship)
; => {:state :pending :valid false :error "invalid transition"}
```

### Event Broadcasting

Both Go and Lisp can subscribe:

```go
// Go host subscribes via channel
events := engine.FSMEvents("order-123")
for ev := range events {
    log.Info("State changed", "from", ev.From, "to", ev.To)
}
```

```lisp
; Lisp callback
(fsm/on-transition order-fsm
  (fn [ev]
    (println "Transitioned from" (:from ev) "to" (:to ev))))
```

---

## 6. State Machine Format

### Basic Structure

```lisp
{:initial :start-state
 :states
 {:state-name
  {:on {:event :target-state}}
  
  :another-state
  {:on {:event :target
        :another-event :another-target}}}}
```

### With Guards and Actions

```lisp
{:initial :pending
 :states
 {:pending
  {:on {:submit {:target :submitted
                 :guard (fn [ctx] (>= (:amount ctx) 100))
                 :action (fn [ctx] (println "Submitted!"))}}}}
```

### Complex Example

```lisp
(def approval-fsm
  {:initial :draft
   :states
   {:draft
    {:on {:submit {:target :pending
                   :guard (fn [ctx] (not (empty? (:content ctx))))}}}
    
    :pending
    {:on {:approve {:target :approved
                    :guard (fn [ctx] (>= (:approver-level ctx) 2))}
          :reject  {:target :rejected}
          :revise  {:target :draft}}}
    
    :approved
    {:on {:publish {:target :published
                    :action (fn [ctx] (publish-document (:id ctx)))}}}
    
    :rejected {}
    :published {}}})
```

---

## 7. Lisp API Reference

### fsm/transition

```lisp
(fsm/transition machine event opts?) → map

; Basic
(fsm/transition order-fsm :pay)
; => {:state :paid :valid true}

; With context for guards
(fsm/transition approval-fsm :submit {:content "Hello"})
; => {:state :submitted :valid true}

; Guard fails
(fsm/transition approval-fsm :submit {:content ""})
; => {:state :draft :valid false :error "guard rejected"}

; Invalid event
(fsm/transition order-fsm :invalid-event)
; => {:state :current-state :valid false :error "event not valid in current state"}
```

### fsm/valid?

```lisp
(fsm/valid? machine event) → bool

(fsm/valid? order-fsm :pay)
; => true

(fsm/valid? order-fsm :ship)  ; Can't ship from pending
; => false
```

### fsm/current-state

```lisp
(fsm/current-state machine) → keyword

(fsm/current-state order-fsm)
; => :pending
```

### fsm/reset

```lisp
(fsm/reset machine) → keyword

(fsm/reset order-fsm)
; => :pending (initial state)
```

### fsm/on-transition

```lisp
(fsm/on-transition machine callback) → nil

(fsm/on-transition order-fsm
  (fn [ev]
    (println "Transition:" (:from ev) "→" (:to ev))))
```

### fsm/reachable

```lisp
(fsm/reachable machine from-state?) → [keyword]

; From current state
(fsm/reachable order-fsm)
; => [:paid :cancelled]  ; Events: :pay, :cancel

; From specific state
(fsm/reachable order-fsm :shipped)
; => [:delivered :returned]
```

### fsm/state-machine

```lisp
(fsm/state-machine machine) → map

; Returns the original definition
(fsm/state-machine order-fsm)
; => {:initial :pending :states {...}}
```

### fsm/register

```lisp
; Register a state machine by name (required for fsm/list and named access)
(fsm/register name machine) → nil

; Example
(def order-fsm {:initial :pending :states {...}})
(fsm/register "order-fsm" order-fsm)  ; explicit name registration

; Or use deffsm macro (preferred)
(deffsm order-fsm {:initial :pending :states {...}})
; deffsm = (def order-fsm ...) + (fsm/register "order-fsm" order-fsm)
```

`fsm/transition` works with both named machines and direct map values:
```lisp
(fsm/transition order-fsm :pay)           ; direct value
(fsm/transition "order-fsm" :pay)         ; by registered name
```

### fsm/list

```lisp
(fsm/list) → [string]

; Returns registered machine names
(fsm/list)
; => ["order-fsm" "approval-fsm"]
```

### fsm/serialize

```lisp
(fsm/serialize machine) → string

; Returns JSON string of current state
(fsm/serialize order-fsm)
; => "{\"version\":1,\"machineId\":\"order-fsm\",\"current\":\"pending\",\"at\":\"2026-02-24T...\"}"
```

### fsm/deserialize

```lisp
(fsm/deserialize machine json-str) → keyword

; Restores state from JSON string, returns restored state
(fsm/deserialize order-fsm snapshot)
; => :pending
```

---

## 8. Implementation Notes

### Go Type Design

```go
type StateMachine struct {
    mu        sync.RWMutex
    id        string
    initial   core.Keyword
    current   core.Keyword
    states    map[core.Keyword]*StateConfig
    eventCh   chan StateEvent
    callbacks []func(StateEvent)
}

type StateConfig struct {
    Transitions map[core.Keyword]Transition
}

type Transition struct {
    Target core.Keyword
    Guard  func(context.Context, map[string]core.Value) bool
    Action func(context.Context, map[string]core.Value)
}

type StateEvent struct {
    MachineID string
    From      core.Keyword
    To        core.Keyword
    At        time.Time
    Context   map[string]any
}
```

Using `core.Keyword` directly avoids a redundant integer mapping layer — state identity is already provided by keyword equality.

### Transition Algorithm

```go
func (sm *StateMachine) Transition(ctx context.Context, event Event, context map[string]Value) (*TransitionResult, error) {
    sm.mu.Lock()

    stateConfig := sm.states[sm.current]
    if stateConfig == nil {
        sm.mu.Unlock()
        return nil, fmt.Errorf("fsm transition: invalid current state %v", sm.current)
    }

    transition, ok := stateConfig.Transitions[event]
    if !ok {
        current := sm.current
        sm.mu.Unlock()
        return &TransitionResult{State: current, Valid: false,
            Error: fmt.Sprintf("event %v not valid in state %v", event, sm.current)}, nil
    }

    if transition.Guard != nil && !transition.Guard(ctx, context) {
        current := sm.current
        sm.mu.Unlock()
        return &TransitionResult{State: current, Valid: false, Error: "guard rejected transition"}, nil
    }

    from := sm.current
    sm.current = transition.Target
    action := transition.Action
    sm.mu.Unlock() // release BEFORE calling action and broadcast

    if action != nil {
        action(ctx, context)
    }

    ev := StateEvent{MachineID: sm.id, From: from, To: sm.current, At: time.Now(), Context: toGoMap(context)}
    sm.broadcast(ev)

    return &TransitionResult{State: sm.current, Valid: true}, nil
}
```

**Critical**: The mutex is released before calling `action` and `broadcast` to prevent deadlock if either calls `fsm/transition` again (reentrant transitions).

### Event Broadcasting

```go
func (sm *StateMachine) broadcast(ev StateEvent) {
    select {
    case sm.eventCh <- ev:
    default:
        // Channel full — log the drop; callers must not rely on channel delivery for critical state tracking
        // For reliable delivery, use on-transition callbacks instead
    }

    for _, cb := range sm.callbacks {
        cb(ev)
    }
}
```

**Reliability**: The Go channel is a best-effort delivery mechanism for host integration. For reliable event delivery (e.g., persisting transitions), use `fsm/on-transition` callbacks which are called synchronously and always delivered.

### Persistence

```go
func (sm *StateMachine) Serialize() ([]byte, error) {
    state := PersistentState{
        Version:   1,
        MachineID: sm.id,
        Current:   sm.current.String(),
        At:        time.Now(),
    }
    return json.Marshal(state)
}

func (sm *StateMachine) Deserialize(data []byte) error {
    var state PersistentState
    if err := json.Unmarshal(data, &state); err != nil {
        return fmt.Errorf("fsm deserialize: %w", err)
    }

    if state.Version != 1 {
        return fmt.Errorf("fsm deserialize: unsupported state version: %d", state.Version)
    }
    
    // Restore state
    sm.current = ParseState(state.Current)
    return nil
}
```

---

## 9. Error Handling

```
InvalidStateError: state :invalid not defined in machine
  → State not in machine definition

InvalidTransitionError: event :invalid not valid in state :pending
  → Event not defined for current state

GuardError: guard rejected transition from :draft to :submitted
  → Guard condition returned false

StateMachineError: machine "order-fsm" not found
  → Machine ID not registered
```

---

## 10. Performance Requirements

| Metric | Target | Notes |
|--------|--------|-------|
| Transition | < 10µs | Without guards/actions |
| Guard evaluation | < 1ms | Lisp function call |
| Event delivery | < 1ms | Channel + callbacks |
| Serialization | < 1ms | JSON encode |
| Concurrent transitions | 1000+/sec | Lock contention minimal |

---

## 11. Dependencies

### External Dependencies

- `sync` - RWMutex for state
- `context` - Cancellation propagation
- `time` - Timestamps
- `encoding/json` - Persistence

### Internal Dependencies

- **Change 1** (core-engine): Required
- **Change 2** (stdlib-plugin): Required (map operations)
- **Change 3** (runtime-api): Required (context)
- **Change 6a** (io-plugin): Optional (for persistence to file)

### Dependent Changes

None (terminal plugin in stack).

---

## 12. Risks & Mitigations

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Guard function errors | Medium | Medium | Recover panics, return error |
| Action side effects | Medium | Medium | Document, transactions not guaranteed |
| State inconsistency | Low | High | Persistence, recovery procedures |
| Event callback loops | Low | High | Document, depth limits |

---

## 13. Acceptance Criteria

- [ ] State machine definition and transitions
- [ ] Transition validation works
- [ ] Guards and actions executed
- [ ] Event subscription (Go + Lisp)
- [ ] Reachability analysis
- [ ] Persistence round-trip
- [ ] Invalid transitions blocked
- [ ] Concurrent access safe
- [ ] Documentation with examples
- [ ] Test coverage ≥ 85%

---

**Next Step:** Create detailed design document (02-design.md) with State type system, transition graph validation, and event broadcasting.
