//go:build integration

package extension

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensionv1 "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// TestBashSchemaAcceptsOnlyPositiveTimeout verifies the public timeout argument before tool dispatch.
func TestBashSchemaAcceptsOnlyPositiveTimeout(t *testing.T) {
	t.Parallel()

	// Arrange: compile the public bash argument schema.
	schema, err := compileSchema(bashToolName, bashInputSchemaJSON)
	require.NoError(t, err)

	// Act: validate one positive timeout and three invalid timeout values.
	_, err = validateArguments(schema, []byte(`{"command":"printf ok","timeout":0.01}`))

	// Assert: accept only positive numeric timeout values.
	require.NoError(t, err)
	for _, arguments := range []string{
		`{"command":"printf ok","timeout":0}`,
		`{"command":"printf ok","timeout":-1}`,
		`{"command":"printf ok","timeout":"1"}`,
	} {
		_, err = validateArguments(schema, []byte(arguments))
		require.Error(t, err)
	}
}

// TestBashExecutionContextClampsSubNanosecondTimeout accepts every positive schema value.
func TestBashExecutionContextClampsSubNanosecondTimeout(t *testing.T) {
	t.Parallel()

	// Arrange: choose a positive timeout below one nanosecond.
	seconds := 1e-10
	// Act: create the bounded bash execution context.
	ctx, stop, err := bashExecutionContext(t.Context(), mo.Some(seconds))
	require.NoError(t, err)
	defer stop()
	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		require.FailNow(t, "sub-nanosecond bash timeout did not fire")
	}
	// Assert: report the configured timeout cause after the clamped duration.
	var timeoutErr bashTimeoutError
	require.ErrorAs(t, context.Cause(ctx), &timeoutErr)
}

// TestServiceExecuteBashTimeout returns a model-visible timeout instead of external cancellation.
func TestServiceExecuteBashTimeout(t *testing.T) {
	t.Parallel()

	// Arrange: make bash wait for its operation timeout and return the timeout cause.
	bashTool := NewMockBashTool(gomock.NewController(t))
	bashTool.EXPECT().Execute(gomock.Any(), "sleep 30", gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ string, _ func(BashProgress) error) (BashResult, error) {
			select {
			case <-ctx.Done():
				cause := context.Cause(ctx)
				return BashResult{
					Text: "started\n\n[" + cause.Error() + "]\n", ExitCode: -1,
					Truncation: textbudget.Truncation{
						Truncated: false, TotalBytes: 0, TotalLines: 0, FullOutputPath: "",
					},
				}, cause
			case <-time.After(250 * time.Millisecond):
				return BashResult{}, errors.New("bash timeout was not applied")
			}
		},
	)
	client := newTestClientWithBash(t, bashTool)

	// Act: prepare and run bash with a short positive timeout.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName: new(bashToolName), ArgumentsJson: []byte(`{"command":"sleep 30","timeout":0.01}`),
	}.Build())

	// Assert: expose the timeout as completed model-visible error data.
	require.NoError(t, err)
	require.True(t, result.GetIsError())
	content := result.GetContents()[0].GetText()
	require.Contains(t, content, "started")
	require.Contains(t, content, "timed out after 0.01 seconds")
}

// TestServiceExecuteBashBoundsRetainedOutputAndRunnerError keeps the complete error result within standard-tool limits.
func TestServiceExecuteBashBoundsRetainedOutputAndRunnerError(t *testing.T) {
	t.Parallel()

	// Arrange: retained output at both standard-tool limits with a complete-output reference.
	fullOutputPath := t.TempDir() + "/complete.log"
	fileReference := "[Output truncated. Full output: " + fullOutputPath + "]\n"
	linePrefix := strings.Repeat("x\n", textbudget.MaximumLines-1)
	paddingBytes := textbudget.MaximumBytes - len(linePrefix) - len(fileReference)
	require.Positive(t, paddingBytes)
	boundedOutput := linePrefix + strings.Repeat("x", paddingBytes) + fileReference
	require.Len(t, boundedOutput, textbudget.MaximumBytes)
	require.Equal(t, textbudget.MaximumLines, strings.Count(boundedOutput, "\n"))

	bashTool := NewMockBashTool(gomock.NewController(t))
	bashTool.EXPECT().Execute(gomock.Any(), "partial", gomock.Any()).Return(
		BashResult{
			Text:     boundedOutput,
			ExitCode: -1,
			Truncation: textbudget.Truncation{
				Truncated:      true,
				TotalBytes:     int64(textbudget.MaximumBytes + 1),
				TotalLines:     int64(textbudget.MaximumLines + 1),
				FullOutputPath: fullOutputPath,
			},
		},
		fmt.Errorf("run bash command: %w", errors.New("unique runner failure")),
	)
	client := newTestClientWithBash(t, bashTool)

	// Act: execute bash through the extension controller.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName: new(bashToolName), ArgumentsJson: []byte(`{"command":"partial"}`),
	}.Build())

	// Assert: the bounded terminal error keeps the full cause and complete-output reference.
	require.NoError(t, err)
	require.True(t, result.GetIsError())
	content := result.GetContents()[0].GetText()
	require.Contains(t, content, "run bash command: unique runner failure")
	require.Contains(t, content, fileReference)
	assert.LessOrEqual(t, len(content), textbudget.MaximumBytes)
	assert.LessOrEqual(t, strings.Count(content, "\n"), textbudget.MaximumLines)
}

// TestServiceExecuteBashRetainedOutputCancellationPreservesCancellation preserves operation cancellation behavior.
func TestServiceExecuteBashRetainedOutputCancellationPreservesCancellation(t *testing.T) {
	t.Parallel()

	// Arrange: a runner result with retained output and wrapped cancellation.
	bashTool := NewMockBashTool(gomock.NewController(t))
	bashTool.EXPECT().Execute(gomock.Any(), "cancel", gomock.Any()).Return(
		BashResult{
			Text: "partial output", ExitCode: -1,
			Truncation: textbudget.Truncation{
				Truncated: false, TotalBytes: 0, TotalLines: 0, FullOutputPath: "",
			},
		},
		fmt.Errorf("run bash command: %w", context.Canceled),
	)
	client := newTestClientWithBash(t, bashTool)

	// Act: execute bash through the extension controller.
	_, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName: new(bashToolName), ArgumentsJson: []byte(`{"command":"cancel"}`),
	}.Build())

	// Assert: cancellation remains identifiable instead of becoming completed tool data.
	require.ErrorIs(t, err, context.Canceled)
}

// TestServiceExecuteBashReturnsBoundedText forwards the prepared terminal result without JSON expansion.
func TestServiceExecuteBashReturnsBoundedText(t *testing.T) {
	t.Parallel()

	// Arrange: make bash return one bounded successful output.
	bashTool := NewMockBashTool(gomock.NewController(t))
	bashTool.EXPECT().Execute(gomock.Any(), "printf ok", gomock.Any()).Return(
		BashResult{
			Text: "ok\n\n[Exit code: 0]\n", ExitCode: 0,
			Truncation: textbudget.Truncation{
				Truncated: false, TotalBytes: 0, TotalLines: 0, FullOutputPath: "",
			},
		},
		nil,
	)
	client := newTestClientWithBash(t, bashTool)

	// Act: prepare and run the public bash operation.
	result, err := runExecution(t, client, extensionv1.ExecuteRequest_builder{
		ToolName: new(bashToolName), ArgumentsJson: []byte(`{"command":"printf ok"}`),
	}.Build())

	// Assert: preserve the bounded text as a successful ToolResult.
	require.NoError(t, err)
	require.False(t, result.GetIsError())
	require.Equal(t, "ok\n\n[Exit code: 0]\n", result.GetContents()[0].GetText())
}

// newTestClientWithBash serves one selected bash mock with inert companion tools.
func newTestClientWithBash(t *testing.T, bashTool BashTool) *Service {
	t.Helper()
	return newTestClientWithAllTools(
		t,
		NewMockReadTool(gomock.NewController(t)),
		NewMockWriteTool(gomock.NewController(t)),
		NewMockEditTool(gomock.NewController(t)),
		bashTool,
		NewMockSearchTool(gomock.NewController(t)),
	)
}
