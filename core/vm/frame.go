package vm

import "github.com/victorzhuk/go-lispico/core"

// Frame is a single call frame on the VM's frame stack: the chunk currently
// executing, its instruction pointer, its base index into the value stack,
// and the lexical environment for local lookups.
type Frame struct {
	chunk *Chunk
	ip    int
	base  int
	env   *core.Env
}
