package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"github.com/samber/mo"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// executeBash decodes and executes bash while mapping progress channels.
func (s *Service) executeBash(arguments []byte, stream extensionv1.ExtensionService_ExecuteServer) error {
	var input bashArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return sendResult(stream, fmt.Sprintf("decode bash arguments: %v", err), true)
	}
	executionContext, stopTimeout, err := bashExecutionContext(stream.Context(), input.Timeout)
	if err != nil {
		return sendResult(stream, err.Error(), true)
	}
	defer stopTimeout()
	result, err := s.bashTool.Execute(executionContext, input.Command, func(progress BashProgress) error {
		return sendProgress(stream, progress)
	})
	if err != nil {
		if result.Text != "" && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			// Retain partial command output and expose the execution cause within the standard-tool budget.
			result.Text = boundedBashResultText(result.Text + "\n\n" + err.Error())
			return sendBashResult(stream, result, true)
		}
		return operationResult(stream, "", err)
	}
	return sendBashResult(stream, result, result.ExitCode != 0)
}

// boundedBashResultText keeps the newest complete error context within the standard-tool limits.
func boundedBashResultText(content string) string {
	bounded := []byte(content)
	if len(bounded) > textbudget.MaximumBytes {
		bounded = bounded[len(bounded)-textbudget.MaximumBytes:]
	}
	bounded = bytes.ToValidUTF8(bounded, []byte("?"))
	for len(bounded) > textbudget.MaximumBytes {
		bounded = bytes.ToValidUTF8(bounded[1:], []byte("?"))
	}
	for bytes.Count(bounded, []byte{'\n'}) > textbudget.MaximumLines {
		newline := bytes.IndexByte(bounded, '\n')
		if newline < 0 {
			return ""
		}
		bounded = bounded[newline+1:]
	}
	return string(bounded)
}

// sendBashResult retains complete output only when its path reaches the caller.
func sendBashResult(sender ResultSender, result BashResult, isError bool) error {
	if err := sendResult(sender, result.Text, isError); err != nil {
		return errors.Join(err, removeBashOutput(result.Truncation.FullOutputPath))
	}
	return nil
}

// removeBashOutput removes an undelivered complete-output file.
func removeBashOutput(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove undelivered bash output: %w", err)
	}
	return nil
}

// bashExecutionContext cancels one command with a timeout-specific cause.
func bashExecutionContext(parent context.Context, timeout mo.Option[float64]) (context.Context, func(), error) {
	seconds, ok := timeout.Get()
	if !ok {
		return parent, func() {}, nil
	}
	if seconds <= 0 {
		return nil, nil, errors.New("bash timeout must be positive")
	}
	maximumSeconds := float64(math.MaxInt64) / float64(time.Second)
	if seconds > maximumSeconds {
		return nil, nil, errors.New("bash timeout exceeds supported duration")
	}
	duration := time.Duration(seconds * float64(time.Second))
	if duration == 0 {
		duration = time.Nanosecond
	}
	ctx, cancel := context.WithCancelCause(parent)
	timer := time.AfterFunc(duration, func() {
		cancel(bashTimeoutError{
			seconds: seconds,
		})
	})
	stop := func() {
		timer.Stop()
		cancel(nil)
	}
	return ctx, stop, nil
}
