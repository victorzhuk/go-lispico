package vm

type Frame struct {
	chunk *Chunk
	ip    int
	base  int
}
