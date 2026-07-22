package runtime

import (
	"context"
	"fmt"
	"sync"

	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/core/compiler"
	"github.com/victorzhuk/go-lispico/core/vm"
)

const maxStdlibBootstrapArtifacts = 64

type stdlibBootstrapKey struct {
	dialectFP  string
	sourceHash sourceHash
}

type stdlibBootstrapArtifact struct {
	chunks []*vm.Chunk
}

type stdlibBootstrapCacheStats struct {
	Entries  int
	Hits     int
	Misses   int
	Compiles int
}

type stdlibBootstrapArtifactCache struct {
	mu        sync.Mutex
	artifacts map[stdlibBootstrapKey]stdlibBootstrapArtifact
	disabled  bool
	hits      int
	misses    int
	compiles  int
}

var stdlibBootstrapArtifacts = &stdlibBootstrapArtifactCache{
	artifacts: make(map[stdlibBootstrapKey]stdlibBootstrapArtifact),
}

// EvalStdlibBootstrap evaluates a reusable stdlib bootstrap source through the
// bytecode VM. Compilation artifacts are process-scoped; each engine runs a
// chunk-tree copy with fresh global-read site tables.
func (be *bytecodeEvaluator) EvalStdlibBootstrap(ctx context.Context, source string, env *core.Env) (result core.Value, err error) {
	ctx = be.evalResourceContext(ctx)
	if core.HasEvalMeter(ctx) {
		var top bool
		top, err = core.StartEval(ctx)
		if err != nil {
			return nil, err
		}
		defer func() {
			if ferr := core.FinishEval(ctx, top); ferr != nil && (err == nil || core.IsTerminalEvalError(ferr)) {
				result = nil
				err = ferr
			}
		}()
	}
	if err := core.PollEvalState(ctx); err != nil {
		return nil, err
	}

	key := stdlibBootstrapKey{dialectFP: be.dialectFP, sourceHash: sha256Hash(source)}
	if artifact, ok := stdlibBootstrapArtifacts.get(key); ok {
		return be.runStdlibBootstrapArtifact(ctx, artifact, env)
	}

	artifact, err := be.compileStdlibBootstrapArtifact(ctx, source, env)
	if err != nil {
		return nil, err
	}
	stdlibBootstrapArtifacts.countCompile()

	result, err = be.runStdlibBootstrapArtifact(ctx, artifact, env)
	if err != nil {
		return nil, err
	}
	stdlibBootstrapArtifacts.put(key, artifact)
	return result, nil
}

func (be *bytecodeEvaluator) compileStdlibBootstrapArtifact(ctx context.Context, source string, env *core.Env) (stdlibBootstrapArtifact, error) {
	forms, err := core.Read(source)
	if err != nil {
		return stdlibBootstrapArtifact{}, fmt.Errorf("stdlib bootstrap read: %w", err)
	}

	macro := core.NewEvaluator()
	macro.SetFallbackEvalMeter(be.engineMeter)
	chunks := make([]*vm.Chunk, 0, len(forms))
	for _, form := range forms {
		expanded, err := macro.MacroExpand(ctx, form, env)
		if err != nil {
			return stdlibBootstrapArtifact{}, fmt.Errorf("stdlib bootstrap macro expand: %w", err)
		}
		comp := compiler.NewCompiler("<stdlib-bootstrap>")
		comp.SetEvalMeter(core.EvalMeterFrom(ctx))
		if err := comp.Compile(expanded); err != nil {
			return stdlibBootstrapArtifact{}, fmt.Errorf("stdlib bootstrap compile: %w", err)
		}
		comp.Chunk().Emit(vm.OpReturn, 0)
		comp.MarkCaptures()
		if err := chargeCompiledChunk(ctx, comp.Chunk()); err != nil {
			return stdlibBootstrapArtifact{}, fmt.Errorf("stdlib bootstrap charge: %w", err)
		}
		chunk := comp.Chunk()
		if err := chunk.Validate(); err != nil {
			return stdlibBootstrapArtifact{}, fmt.Errorf("stdlib bootstrap validate: %w", err)
		}
		chunks = append(chunks, chunk)
	}
	return stdlibBootstrapArtifact{chunks: chunks}, nil
}

func (be *bytecodeEvaluator) runStdlibBootstrapArtifact(ctx context.Context, artifact stdlibBootstrapArtifact, env *core.Env) (core.Value, error) {
	var result core.Value = core.Nil{}
	for _, chunk := range artifact.chunks {
		var err error
		result, err = be.runVM(ctx, chunk.CopyTreeFreshSites(), env)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (c *stdlibBootstrapArtifactCache) get(key stdlibBootstrapKey) (stdlibBootstrapArtifact, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disabled {
		return stdlibBootstrapArtifact{}, false
	}
	artifact, ok := c.artifacts[key]
	if ok {
		c.hits++
	} else {
		c.misses++
	}
	return artifact, ok
}

func (c *stdlibBootstrapArtifactCache) put(key stdlibBootstrapKey, artifact stdlibBootstrapArtifact) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.disabled {
		return
	}
	if _, ok := c.artifacts[key]; !ok {
		c.artifacts[key] = artifact
	}
	for len(c.artifacts) > maxStdlibBootstrapArtifacts {
		for k := range c.artifacts {
			delete(c.artifacts, k)
			break
		}
	}
}

func (c *stdlibBootstrapArtifactCache) countCompile() {
	c.mu.Lock()
	c.compiles++
	c.mu.Unlock()
}

func clearStdlibBootstrapCacheForTest() {
	stdlibBootstrapArtifacts.mu.Lock()
	stdlibBootstrapArtifacts.artifacts = make(map[stdlibBootstrapKey]stdlibBootstrapArtifact)
	stdlibBootstrapArtifacts.hits = 0
	stdlibBootstrapArtifacts.misses = 0
	stdlibBootstrapArtifacts.compiles = 0
	stdlibBootstrapArtifacts.mu.Unlock()
}

func setStdlibBootstrapCacheDisabledForTest(disabled bool) func() {
	stdlibBootstrapArtifacts.mu.Lock()
	prev := stdlibBootstrapArtifacts.disabled
	stdlibBootstrapArtifacts.disabled = disabled
	stdlibBootstrapArtifacts.mu.Unlock()
	return func() {
		stdlibBootstrapArtifacts.mu.Lock()
		stdlibBootstrapArtifacts.disabled = prev
		stdlibBootstrapArtifacts.mu.Unlock()
	}
}

func stdlibBootstrapCacheStatsForTest() stdlibBootstrapCacheStats {
	stdlibBootstrapArtifacts.mu.Lock()
	defer stdlibBootstrapArtifacts.mu.Unlock()
	return stdlibBootstrapCacheStats{
		Entries:  len(stdlibBootstrapArtifacts.artifacts),
		Hits:     stdlibBootstrapArtifacts.hits,
		Misses:   stdlibBootstrapArtifacts.misses,
		Compiles: stdlibBootstrapArtifacts.compiles,
	}
}
