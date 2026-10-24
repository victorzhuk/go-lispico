package runtime

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/victorzhuk/go-lispico/core"
)

// sourceChunkBytes caps each read of a source file so the reader decides
// admission in bounded steps instead of pre-allocating the whole ceiling.
const sourceChunkBytes = 64 << 10

// readSourceFile reads path as engine source, refusing input larger than the
// resolved MaxAllocationBytes before buffering more than that ceiling. Once
// the content fills the ceiling, one extra byte is probed so an oversize file
// is proven oversize without holding its full content. All arithmetic stays
// below the ceiling, so large positive limits cannot overflow. The ceiling is
// a source-size admission check, not a
// claim that Go heap use equals the deterministic allocation ledger; strings
// callers hand to Eval directly stay borrowed and uncharged.
func (e *engineImpl) readSourceFile(ctx context.Context, path string) ([]byte, error) {
	ceiling := e.config.limits.MaxAllocationBytes

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var content []byte
	for len(content) < ceiling {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		chunk := make([]byte, min(sourceChunkBytes, ceiling-len(content)))
		n, rerr := io.ReadFull(f, chunk)
		content = append(content, chunk[:n]...)
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			return content, nil
		}
		if rerr != nil {
			return nil, rerr
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var probe [1]byte
	n, rerr := f.Read(probe[:])
	if n > 0 {
		return nil, core.NewResourceLimitError(fmt.Sprintf(
			"read file %s: source size exceeds MaxAllocationBytes %d", path, ceiling))
	}
	if rerr == io.EOF {
		return content, nil
	}
	return nil, rerr
}
