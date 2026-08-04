package bash

import "context"

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

// ProcessResult contains complete command output and exit status.
type ProcessResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ProcessRunner executes one bash command.
//
//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=bash
type ProcessRunner interface {
	Run(ctx context.Context, command string, handleProgress ProgressHandler) (ProcessResult, error)
}
