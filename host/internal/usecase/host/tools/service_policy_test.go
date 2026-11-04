//go:build !integration

package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	"github.com/n-r-w/glyph/internal/errtree"
)

// acceptedReadService returns a service with one accepted tool read whose runtime is available and whose tool policy
// is the mock.
func acceptedReadService(
	t *testing.T,
	controller *gomock.Controller,
) (*Service, *MockRuntime, *MockToolPolicy) {
	t.Helper()
	runtime := NewMockRuntime(controller)
	policy := NewMockToolPolicy(controller)
	contexts := NewMockContextIssuer(controller)
	contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil).AnyTimes()
	service := New(runtime)
	service.BindContextIssuer(contexts)
	service.BindToolPolicy(policy)
	descriptors, err := service.ValidateLocal(
		startup.PendingRegistration{
			ID:       "extension",
			Path:     "/extension",
			Tools:    []startup.RawToolDescriptor{rawDescriptor("read", "Read.")},
			Handlers: nil,
		},
	)
	require.NoError(t, err)
	service.Commit(
		[]startup.AcceptedRegistration{{ID: "extension", Path: "/extension", Tools: descriptors, Handlers: nil}},
	)
	runtime.EXPECT().ToolRuntimeAvailable("extension").Return(true).AnyTimes()
	return service, runtime, policy
}

// TestServiceRunsToolWithPolicyInput verifies that the tool runs with the input that the chain returns and that the
// chain receives a validation of the tool schema for its replacements.
func TestServiceRunsToolWithPolicyInput(t *testing.T) {
	t.Parallel()
	// Arrange a chain that checks the schema validation it receives and replaces the input.
	controller := gomock.NewController(t)
	service, runtime, policy := acceptedReadService(t, controller)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{"path":"original"}`)}
	policy.EXPECT().ApplyToolPolicy(t.Context(), call, gomock.Any()).DoAndReturn(
		func(_ context.Context, current model.ToolCall, validate func([]byte) error) (model.ToolCall, mo.Option[string], error) {
			require.NoError(t, validate([]byte(`{"path":"valid"}`)))
			require.ErrorContains(t, validate([]byte(`{"path":1}`)), `invalid arguments for tool "read"`)
			current.Arguments = testToolCallArguments(`{ "path":"replaced" }`)
			return current, mo.None[string](), nil
		},
	)
	runtime.EXPECT().
		ExecuteTool(t.Context(), "extension", "read", []byte(`{ "path":"replaced" }`), gomock.Any(), gomock.Any()).
		Return(tool.Result{Terminate: false, Contents: tool.TextContents("content"), IsError: false}, nil)

	// Act through the execution.
	result, err := service.Execute(t.Context(), call, func(tool.Progress) error { return nil })

	// Assert the result of the tool that ran with the replaced input.
	require.NoError(t, err)
	assert.Equal(t, tool.TextContents("content"), result.Contents)
}

// TestServiceRejectionIsPolicyErrorResult verifies that a rejection gives an error result with the policy category
// and the complete message, and that the tool does not run.
func TestServiceRejectionIsPolicyErrorResult(t *testing.T) {
	t.Parallel()
	// Arrange a chain that rejects the call; the strict runtime mock forbids an execution.
	controller := gomock.NewController(t)
	service, _, policy := acceptedReadService(t, controller)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{"path":"secret"}`)}
	policy.EXPECT().ApplyToolPolicy(t.Context(), call, gomock.Any()).
		Return(call, mo.Some("rejected: the path is secret"), nil)

	// Act through the execution.
	result, err := service.Execute(t.Context(), call, func(tool.Progress) error { return nil })

	// Assert the error result without a Go error.
	require.NoError(t, err)
	assert.True(t, result.IsError)
	assert.Equal(t, mo.Some(agent.ToolErrorPolicyRejection), result.ErrorCategory)
	assert.Equal(t, tool.TextContents("rejected: the path is secret"), result.Contents)
	assert.Equal(t, "call", result.CallID)
	assert.Equal(t, "read", result.ToolName)
}

// TestServiceChainFailureIsFatalOperationFailure verifies that a chain failure returns the complete cause as a fatal
// extension failure and that the tool does not run.
func TestServiceChainFailureIsFatalOperationFailure(t *testing.T) {
	t.Parallel()
	// Arrange a chain that fails; the strict runtime mock forbids an execution.
	controller := gomock.NewController(t)
	service, _, policy := acceptedReadService(t, controller)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{"path":"a"}`)}
	cause := errors.New(`middleware handler "policy": policy source: unexpected EOF`)
	policy.EXPECT().ApplyToolPolicy(t.Context(), call, gomock.Any()).Return(model.ToolCall{}, mo.None[string](), cause)

	// Act through the execution.
	_, err := service.Execute(t.Context(), call, func(tool.Progress) error { return nil })

	// Assert the fatal marker, the category, and the complete cause.
	require.ErrorIs(t, err, cause)
	assert.EqualError(t, err, cause.Error())
	_, fatal := errors.AsType[run.FatalOperationFailure](err)
	assert.True(t, fatal)
	coded, isCoded := errors.AsType[errtree.CodedError](err)
	require.True(t, isCoded)
	assert.Equal(t, "EXTENSION_FAILED", coded.Code())
}

// TestServiceChainCancellationIsOrdinaryError verifies that a chain canceled by the owning context returns the
// cancellation without the fatal marker, so Agent Core ends the run as aborted.
func TestServiceChainCancellationIsOrdinaryError(t *testing.T) {
	t.Parallel()
	// Arrange a canceled context and a chain that returns its cancellation.
	controller := gomock.NewController(t)
	service, _, policy := acceptedReadService(t, controller)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{"path":"a"}`)}
	policy.EXPECT().ApplyToolPolicy(ctx, call, gomock.Any()).Return(model.ToolCall{}, mo.None[string](), ctx.Err())

	// Act through the execution.
	_, err := service.Execute(ctx, call, func(tool.Progress) error { return nil })

	// Assert the plain cancellation.
	require.ErrorIs(t, err, context.Canceled)
	_, fatal := errors.AsType[run.FatalOperationFailure](err)
	assert.False(t, fatal)
}

// TestServiceValidatesOriginalArgumentsAfterChain verifies that arguments no handler replaced are validated after the
// chain and give the invalid-arguments result without running the tool.
func TestServiceValidatesOriginalArgumentsAfterChain(t *testing.T) {
	t.Parallel()
	// Arrange a chain that allows a call with schema-invalid arguments; the strict runtime mock forbids an execution.
	controller := gomock.NewController(t)
	service, _, policy := acceptedReadService(t, controller)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{}`)}
	policy.EXPECT().ApplyToolPolicy(t.Context(), call, gomock.Any()).Return(call, mo.None[string](), nil)

	// Act through the execution.
	result, err := service.Execute(t.Context(), call, func(tool.Progress) error { return nil })

	// Assert the invalid-arguments result.
	require.NoError(t, err)
	assert.Equal(t, mo.Some(agent.ToolErrorInvalidArguments), result.ErrorCategory)
}

// TestServiceSkipsPolicyForUnavailableTool verifies that a call of an unavailable tool gets the unavailable result
// before the chain runs.
func TestServiceSkipsPolicyForUnavailableTool(t *testing.T) {
	t.Parallel()
	// Arrange a service without the called tool; the strict policy mock forbids the chain.
	controller := gomock.NewController(t)
	service, _, _ := acceptedReadService(t, controller)
	call := model.ToolCall{ID: "call", Name: "missing", Arguments: testToolCallArguments(`{}`)}

	// Act through the execution.
	result, err := service.Execute(t.Context(), call, func(tool.Progress) error { return nil })

	// Assert the unavailable result.
	require.NoError(t, err)
	assert.Equal(t, mo.Some(agent.ToolErrorUnavailableTool), result.ErrorCategory)
}

// TestServiceRequiresToolPolicy verifies that an execution without a bound tool policy fails instead of running the
// tool unchecked.
func TestServiceRequiresToolPolicy(t *testing.T) {
	t.Parallel()
	// Arrange a service whose tool policy is not bound; the strict runtime mock forbids an execution.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	service := New(runtime)
	descriptors, err := service.ValidateLocal(
		startup.PendingRegistration{
			ID: "extension", Path: "/extension",
			Tools:    []startup.RawToolDescriptor{rawDescriptor("read", "Read.")},
			Handlers: nil,
		},
	)
	require.NoError(t, err)
	service.Commit(
		[]startup.AcceptedRegistration{{ID: "extension", Path: "/extension", Tools: descriptors, Handlers: nil}},
	)
	runtime.EXPECT().ToolRuntimeAvailable("extension").Return(true)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{"path":"a"}`)}

	// Act through the execution.
	_, executeErr := service.Execute(t.Context(), call, func(tool.Progress) error { return nil })

	// Assert the explicit failure; the strict runtime mock shows that the tool did not run.
	require.Error(t, executeErr)
}
