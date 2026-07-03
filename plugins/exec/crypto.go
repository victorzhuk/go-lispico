package exec

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"github.com/victorzhuk/go-lispico/core"
)

func (p *Plugin) sha256(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("crypto/sha256: requires 1 argument")
	}

	data, ok := args[0].(core.String)
	if !ok {
		return nil, fmt.Errorf("crypto/sha256: requires a string argument")
	}

	hash := sha256.Sum256([]byte(data.V))
	return core.String{V: hex.EncodeToString(hash[:])}, nil
}

func (p *Plugin) uuid(ctx context.Context, eval core.Evaluator, args []core.Value, env *core.Env) (core.Value, error) {
	var b [16]byte
	_, err := io.ReadFull(rand.Reader, b[:])
	if err != nil {
		return nil, fmt.Errorf("generate uuid: %w", err)
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return core.String{V: uuid}, nil
}
