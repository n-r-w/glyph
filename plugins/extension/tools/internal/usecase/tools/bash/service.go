// Package bash executes one command through the project bash process adapter.
package bash

import (
	"context"
	"fmt"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// Service coordinates one bash command.
type Service struct{ runner ProcessRunner }

var _ extensioncontroller.BashTool = (*Service)(nil)

// New creates a bash service.
func New(runner ProcessRunner) *Service { return &Service{runner: runner} }

// Execute streams status and command output and returns complete output.
func (s *Service) Execute(
	ctx context.Context,
	command string,
	handleProgress func(extensioncontroller.BashProgress) error,
) (extensioncontroller.BashResult, error) {
	status := extensioncontroller.BashProgress{
		Channel: extensioncontroller.BashProgressStatus,
		Content: "running",
	}
	if err := handleProgress(status); err != nil {
		return extensioncontroller.BashResult{}, fmt.Errorf("deliver bash status: %w", err)
	}
	result, err := s.runner.Run(ctx, command, func(stream Stream, content string) error {
		channel := extensioncontroller.BashProgressStatus
		switch stream {
		case StreamStdout:
			channel = extensioncontroller.BashProgressStdout
		case StreamStderr:
			channel = extensioncontroller.BashProgressStderr
		}
		return handleProgress(extensioncontroller.BashProgress{Channel: channel, Content: content})
	})
	if err != nil {
		return extensioncontroller.BashResult{}, fmt.Errorf("run bash command: %w", err)
	}
	return extensioncontroller.BashResult{Stdout: result.Stdout, Stderr: result.Stderr, ExitCode: result.ExitCode}, nil
}
