// Package main provides the Glyph Host executable.
package main

import (
	"context"
	"io"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
)

const (
	exitSuccess = 0
	exitFailure = 1
	exitUsage   = 2
)

// applicationRun is the concrete composition entry point used by command execution.
type applicationRun func(context.Context, cli.Command, io.Writer, io.Writer) error

// execute validates arguments, runs one request, and maps terminal outcomes to process status.
func execute(
	ctx context.Context,
	arguments []string,
	stdout, stderr io.Writer,
	run applicationRun,
) int {
	renderer := headless.NewRenderer(stdout, stderr)
	command, err := cli.Parse(arguments)
	if err != nil {
		_ = renderer.WriteError(err)
		return exitUsage
	}
	runErr := run(ctx, command, stdout, stderr)
	if runErr != nil {
		_ = renderer.WriteError(runErr)
		return exitFailure
	}
	return exitSuccess
}
