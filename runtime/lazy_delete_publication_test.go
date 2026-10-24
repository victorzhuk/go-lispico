package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
)

// A deferred bootstrap definition staged outside the deletion critical
// section must not publish past an UnloadPlugin or an explicit Delete that
// completes after admission: the name stays absent in both cells and a host
// write racing the window survives untouched.

func lmStagedArrow(t *testing.T, eng Engine, impl *engineImpl, p *lfProbe) chan struct{} {
	t.Helper()
	gDone := make(chan struct{})
	go func() {
		defer close(gDone)
		_, _ = eng.RootEnv().Get("->")
	}()
	lfWait(t, p.entered, "staged bootstrap execution of ->")
	t.Cleanup(func() {
		p.unblock()
		<-gDone
	})
	_ = impl
	return gDone
}

func assertArrowAbsentHostWriteSurvives(t *testing.T, eng Engine, impl *engineImpl, marker string) {
	t.Helper()
	state := impl.lazyMaterializer.state
	state.mu.Lock()
	_, installed := state.installed["->"]
	state.mu.Unlock()
	assert.False(t, installed, "-> must not be recorded installed after %s", marker)

	root := eng.RootEnv()
	assert.False(t, root.HasLiveFunc("->"), "-> must not be live in the func cell after %s", marker)

	host := core.Int{V: 42}
	require.NoError(t, root.Set("->", host), "the host write racing the %s window must be accepted", marker)
	got, ok := root.Get("->")
	require.True(t, ok, "the host write must survive %s", marker)
	assert.True(t, host.Equals(got), "the host write must survive %s: got %v", marker, got)

	_, err := eng.Eval(context.Background(), "after-"+marker, "(-> 1)")
	require.Error(t, err, "-> must not resolve to the staged macro after %s", marker)
	// The head position resolves to the surviving host write (an Int, not a
	// callable) — proof the staged macro definition never published.
	assert.Contains(t, err.Error(), "expected callable")
}

// The unload completes while an admitted materialization is still executing
// its bootstrap definition; the publication check must discard the staged
// definition.
func TestLazyMaterialize_UnloadDuringStagedBootstrapDoesNotPublish(t *testing.T) {
	t.Parallel()

	eng, impl, p := lfArmed(t)
	gDone := lmStagedArrow(t, eng, impl, p)

	// No name or state lock is held across bootstrap execution, so the
	// unload must complete while the definition is in flight.
	require.NoError(t, eng.UnloadPlugin(""), "UnloadPlugin must not block on staged bootstrap execution")

	p.unblock()
	<-gDone
	assertArrowAbsentHostWriteSurvives(t, eng, impl, "unload")
}

// The explicit Delete completes while an admitted materialization is still
// executing its bootstrap definition; the deletion critical section taken by
// BeginDelete must win over the later publication.
func TestLazyMaterialize_DeleteDuringStagedBootstrapDoesNotPublish(t *testing.T) {
	t.Parallel()

	eng, impl, p := lfArmed(t)
	gDone := lmStagedArrow(t, eng, impl, p)

	eng.RootEnv().Delete("->")

	p.unblock()
	<-gDone
	assertArrowAbsentHostWriteSurvives(t, eng, impl, "delete")
}

// A host binding installed while a staged bootstrap definition executes must
// survive publication: publishStaged fills only destination cells that are
// still unoccupied, in each namespace independently.
func TestLazyMaterialize_HostBindingDuringStagedBootstrapWins(t *testing.T) {
	t.Parallel()

	startStaged := func(t *testing.T) (Engine, *lfProbe, chan struct{}) {
		t.Helper()
		eng, _, p := lfArmed(t)
		gDone := make(chan struct{})
		t.Cleanup(func() {
			p.unblock()
			<-gDone
		})
		go func() {
			defer close(gDone)
			_, _ = eng.RootEnv().Get("->")
		}()
		lfWait(t, p.entered, "staged bootstrap execution of ->")
		return eng, p, gDone
	}

	t.Run("func cell", func(t *testing.T) {
		eng, p, gDone := startStaged(t)
		root := eng.RootEnv()

		hostFn, err := eng.Eval(context.Background(), "host", "(fn [x] [:host x])")
		require.NoError(t, err)
		require.NoError(t, root.SetFunc("->", hostFn),
			"the host write into the func cell during staging must be accepted")

		p.unblock()
		lfWait(t, gDone, "materialization of -> after release")

		got, ok := root.GetFunc("->")
		require.True(t, ok, "the host func binding of -> must survive publication")
		assert.True(t, hostFn.Equals(got),
			"the func cell must hold the host binding, not the staged macro: got %v", got)
	})

	t.Run("value cell", func(t *testing.T) {
		eng, p, gDone := startStaged(t)
		root := eng.RootEnv()

		host := core.Int{V: 7}
		require.NoError(t, root.Set("->", host),
			"the host write into the value cell during staging must be accepted")

		p.unblock()
		lfWait(t, gDone, "materialization of -> after release")

		got, ok := root.Get("->")
		require.True(t, ok, "the host value binding of -> must survive publication")
		assert.True(t, host.Equals(got),
			"the value cell must hold the host binding: got %v", got)
	})
}
