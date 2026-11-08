// Package macos contains read-only macOS diagnostic command definitions.
package macos

import (
	"context"
	"time"
)

type DiagnosticCommand struct {
	Executable string
	Args       []string
	Timeout    time.Duration
}

type DiagnosticResult struct {
	Stdout    []byte
	Stderr    []byte
	Err       error
	Truncated bool
}

type DiagnosticRunner interface {
	Run(context.Context, DiagnosticCommand) DiagnosticResult
}
