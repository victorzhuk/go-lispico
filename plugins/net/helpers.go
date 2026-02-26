package net

import (
	"github.com/victorzhuk/go-lispico/core"
)

func extractInt(m *core.HashMap, key string) (int64, bool) {
	if v, ok := m.Get(core.Keyword{V: key}); ok {
		if i, ok := v.(core.Int); ok {
			return i.V, true
		}
	}
	return 0, false
}

func extractMap(m *core.HashMap, key string) (*core.HashMap, bool) {
	if v, ok := m.Get(core.Keyword{V: key}); ok {
		if hm, ok := v.(*core.HashMap); ok {
			return hm, true
		}
	}
	return nil, false
}

func lispToGo(v core.Value) (any, error) {
	return core.ToGoValue(v)
}

func goToLisp(v any) (core.Value, error) {
	return core.FromGoValue(v)
}
