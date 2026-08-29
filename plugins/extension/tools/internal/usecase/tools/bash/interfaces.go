package bash

import (
	"context"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=bash

// Stream identifies command output origin.
type Stream uint8

const (
	// StreamStdout carries standard output.
	StreamStdout Stream = iota
	// StreamStderr carries standard error.
	StreamStderr
)

// ProgressHandler consumes command output in delivery order.
type ProgressHandler func(stream Stream, content string) error

// ProcessResult contains bounded command output, exit status, and truncation metadata.
type ProcessResult struct {
	// Output contains bounded model-visible command output.
	Output string
	// ExitCode contains the command process exit status.
	ExitCode int
	// Truncation describes omitted complete output.
	Truncation textbudget.Truncation
}

// ProcessRunner executes one bash command.
type ProcessRunner interface {
	Run(ctx context.Context, command string, handleProgress ProgressHandler) (ProcessResult, error)
}
