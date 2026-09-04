package core

import "context"

func ValueStringContext(context.Context, Value) (string, error) { panic("not implemented") }
func ValueDeepBytesContext(context.Context, Value) (int64, error) { panic("not implemented") }
func ValueNodeCountContext(context.Context, Value) (int, error) { panic("not implemented") }
func CheckConstructionDepthContext(context.Context, Value, Evaluator) error { panic("not implemented") }
func CheckNestedElementDepthContext(context.Context, Value, Evaluator) error { panic("not implemented") }
