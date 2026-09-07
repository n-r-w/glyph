package extension

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/samber/mo"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// executeBash decodes and executes bash while mapping progress channels.
func (s *Service) executeBash(
	ctx context.Context,
	arguments []byte,
	report func(context.Context, *extensionv1.ToolProgress) error,
) (*extensionv1.ToolResult, error) {
	var input bashArguments
	if err := json.Unmarshal(arguments, &input); err != nil {
		return textResult(fmt.Sprintf("decode bash arguments: %v", err), true), nil
	}
	if err := validateBashTimeout(input.Timeout); err != nil {
		return textResult(err.Error(), true), nil
	}
	command := BashCommand{Text: input.Command, Timeout: input.Timeout}
	result, err := s.bashTool.Execute(ctx, command, func(progress BashProgress) error {
		return reportProgress(ctx, report, progress)
	})
	if err != nil {
		if result.Text != "" && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			// Retain partial command output and expose the execution cause within the standard-tool budget.
			result.Text = boundedBashResultText(result.Text + "\n\n" + err.Error())
			return bashResult(result, true)
		}
		return operationResult("", err)
	}
	return bashResult(result, result.ExitCode != 0)
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

// bashResult returns bounded output and retains its complete-output file for the caller.
func bashResult(result BashResult, isError bool) (*extensionv1.ToolResult, error) {
	return textResult(result.Text, isError), nil
}

// validateBashTimeout checks the public duration range before dispatch.
func validateBashTimeout(timeout mo.Option[float64]) error {
	seconds, ok := timeout.Get()
	if !ok {
		return nil
	}
	if seconds <= 0 {
		return errors.New("bash timeout must be positive")
	}
	maximumSeconds := float64(math.MaxInt64) / float64(time.Second)
	if seconds > maximumSeconds {
		return errors.New("bash timeout exceeds supported duration")
	}
	return nil
}
