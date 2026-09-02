// Package app assembles and runs the concrete Glyph Host.
package app

import (
	"context"
	"fmt"
	"io"

	"github.com/n-r-w/glyph/host/internal/controller/cli"

	"github.com/n-r-w/glyph/host/internal/infra/persistence"
)

// Run initializes user data paths and performs one validated invocation.
func Run(ctx context.Context, command cli.Command, stdout, stderr io.Writer) error {
	paths, err := persistence.Initialize()
	if err != nil {
		return fmt.Errorf("initialize Glyph persistence: %w", err)
	}
	return runWithPaths(ctx, paths, command, stdout, stderr)
}

// runWithPaths selects an isolated composition path for the requested mode.
func runWithPaths(
	ctx context.Context,
	paths persistence.Paths,
	command cli.Command,
	stdout, stderr io.Writer,
) error {
	switch command.Mode {
	case cli.ModeHeadless:
		return runHeadlessWithPaths(ctx, paths, command.Headless, stdout, stderr)
	case cli.ModeRPC:
		return runProgrammaticWithPaths(ctx, paths, command, stdout)
	case cli.ModeUI:
		return runUIWithPaths(ctx, paths, command, stderr)
	}
	return fmt.Errorf("unsupported Glyph application mode %d", command.Mode)
}
