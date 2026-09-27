package cl

import "github.com/victorzhuk/go-lispico/core"

// CLSpec exposes the unexported stock dialect spec to in-package tests.
func CLSpec() core.DialectSpec { return clSpec }
