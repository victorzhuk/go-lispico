package vm

import "github.com/victorzhuk/go-lispico/core"

type Frame struct {
	chunk *Chunk
	ip    int
	base  int
	env   *core.Env
}
