package vm

import "github.com/victorzhuk/go-lispico/core"

// Frame is a single call frame on the VM's frame stack: the chunk currently
// executing, its instruction pointer, its base index into the value stack,
// the environment for global lookups, and the closure's captured cells.
type Frame struct {
	chunk     *Chunk
	ip        int
	base      int
	env       *core.Env
	caps      []*cellBox
	isClosure bool
}
