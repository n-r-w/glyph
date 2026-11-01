//go:build !integration

package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// commitToolPolicyHandlers registers tool-policy handlers of one extension in the given order.
func commitToolPolicyHandlers(service *Service, extensionID string, handlerIDs ...string) {
	handlers := make([]startup.AcceptedHandler, len(handlerIDs))
	for index, id := range handlerIDs {
		handlers[index] = startup.AcceptedHandler{ID: id, Kind: startup.RawHandlerKindToolPolicy}
	}
	service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{
		{ID: extensionID, Path: "", Tools: nil, Handlers: handlers},
	})
}

// policyCall creates a model tool call with the given JSON arguments.
func policyCall(t *testing.T, arguments string) model.ToolCall {
	t.Helper()
	parsed, err := model.NewToolCallArguments([]byte(arguments))
	require.NoError(t, err)
	return model.ToolCall{ID: "call", Name: "read", Arguments: parsed}
}

// acceptAnyArguments is a schema validation that accepts every arguments value.
func acceptAnyArguments([]byte) error { return nil }

// TestApplyToolPolicyComposesHandlers verifies registration order, the immutable original call, the call left by
// preceding handlers, the validation of each replacement, and skipped handlers of other kinds.
func TestApplyToolPolicyComposesHandlers(t *testing.T) {
	t.Parallel()
	// Arrange two extensions whose handlers replace the input, allow, and replace from the received current call.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, nil)
	commitToolPolicyHandlers(service, "first", "policy-a", "policy-keep")
	commitInputHandlers(service, "first", "input")
	commitToolPolicyHandlers(service, "second", "policy-b")
	call := policyCall(t, `{"path":"original"}`)
	contexts.EXPECT().IssueContext("first").Return(extension.Context{}, nil).Times(2)
	contexts.EXPECT().IssueContext("second").Return(extension.Context{}, nil)
	gomock.InOrder(
		runtime.EXPECT().HandleToolPolicy(gomock.Any(), "first", "policy-a", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Transformation[model.ToolCall]) (ToolPolicyAction, error) {
				assert.Equal(t, call, input.Original)
				assert.Equal(t, call, input.Current)
				return ToolPolicyAction{Kind: Replace, Arguments: []byte(`{"path":"A"}`), Reason: ""}, nil
			}),
		runtime.EXPECT().HandleToolPolicy(gomock.Any(), "first", "policy-keep", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Transformation[model.ToolCall]) (ToolPolicyAction, error) {
				assert.Equal(t, `{"path":"A"}`, input.Current.Arguments.String())
				return ToolPolicyAction{Kind: Preserve, Arguments: nil, Reason: ""}, nil
			}),
		runtime.EXPECT().HandleToolPolicy(gomock.Any(), "second", "policy-b", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Transformation[model.ToolCall]) (ToolPolicyAction, error) {
				assert.Equal(t, call, input.Original)
				assert.Equal(t, `{"path":"A"}`, input.Current.Arguments.String())
				return ToolPolicyAction{Kind: Replace, Arguments: []byte(`{"path":"B"}`), Reason: ""}, nil
			}),
	)
	var validated []string
	validate := func(arguments []byte) error {
		validated = append(validated, string(arguments))
		return nil
	}

	// Act through the chain.
	final, rejection, err := service.ApplyToolPolicy(t.Context(), call, validate)

	// Assert the final input, the validation of each replacement before its successor, and no rejection.
	require.NoError(t, err)
	assert.False(t, rejection.IsPresent())
	assert.Equal(t, `{"path":"B"}`, final.Arguments.String())
	assert.Equal(t, call.ID, final.ID)
	assert.Equal(t, call.Name, final.Name)
	assert.Equal(t, []string{`{"path":"A"}`, `{"path":"B"}`}, validated)
	assert.Equal(t, `{"path":"original"}`, call.Arguments.String())
}

// TestApplyToolPolicyWithoutHandlers verifies that a call passes unchanged when no handler is registered.
func TestApplyToolPolicyWithoutHandlers(t *testing.T) {
	t.Parallel()
	// Arrange a service without tool-policy handlers; strict mocks reject every invocation.
	ctrl := gomock.NewController(t)
	service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), nil)
	call := policyCall(t, `{"path":"a"}`)

	// Act through the chain.
	final, rejection, err := service.ApplyToolPolicy(t.Context(), call, func([]byte) error {
		require.Fail(t, "no replacement exists to validate")
		return nil
	})

	// Assert the call is unchanged.
	require.NoError(t, err)
	assert.False(t, rejection.IsPresent())
	assert.Equal(t, call, final)
}

// TestApplyToolPolicyRejectionEndsChain verifies that a rejection returns the complete reason with the rejecting
// handler and that no later handler runs.
func TestApplyToolPolicyRejectionEndsChain(t *testing.T) {
	t.Parallel()
	// Arrange a rejecting first handler and a second handler that strict mocks forbid.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts, nil)
	commitToolPolicyHandlers(service, "first", "policy-reject")
	commitToolPolicyHandlers(service, "second", "policy-later")
	contexts.EXPECT().IssueContext("first").Return(extension.Context{}, nil)
	reason := "path outside the project: /etc/passwd"
	runtime.EXPECT().HandleToolPolicy(gomock.Any(), "first", "policy-reject", gomock.Any()).Return(
		ToolPolicyAction{Kind: Reject, Arguments: nil, Reason: reason}, nil,
	)

	// Act through the chain.
	_, rejection, err := service.ApplyToolPolicy(t.Context(), policyCall(t, `{}`), acceptAnyArguments)

	// Assert the rejection message holds the complete reason and the rejecting handler.
	require.NoError(t, err)
	message, rejected := rejection.Get()
	require.True(t, rejected)
	assert.Contains(t, message, reason)
	assert.Contains(t, message, `"first"`)
	assert.Contains(t, message, `"policy-reject"`)
}

// TestApplyToolPolicyFailureEndsChain verifies that a handler error and each invalid action stop the chain with an
// error that keeps the cause, before a later handler runs.
func TestApplyToolPolicyFailureEndsChain(t *testing.T) {
	t.Parallel()
	source := errors.New("schema source: value must be string")
	handlerErr := errors.New("policy source: unexpected EOF")
	tests := map[string]struct {
		action   ToolPolicyAction
		handler  error
		validate func([]byte) error
		cause    error
	}{
		"handler error": {
			action: ToolPolicyAction{}, handler: handlerErr, validate: acceptAnyArguments, cause: handlerErr,
		},
		"unknown action": {
			action: ToolPolicyAction{Kind: 0, Arguments: nil, Reason: ""}, handler: nil,
			validate: acceptAnyArguments, cause: nil,
		},
		"blank rejection reason": {
			action: ToolPolicyAction{Kind: Reject, Arguments: nil, Reason: "  "}, handler: nil,
			validate: acceptAnyArguments, cause: nil,
		},
		"replacement that is not a JSON object": {
			action: ToolPolicyAction{Kind: Replace, Arguments: []byte(`[1]`), Reason: ""}, handler: nil,
			validate: acceptAnyArguments, cause: nil,
		},
		"replacement that violates the schema": {
			action: ToolPolicyAction{Kind: Replace, Arguments: []byte(`{"path":1}`), Reason: ""}, handler: nil,
			validate: func([]byte) error { return source }, cause: source,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a failing first handler and a second handler that strict mocks forbid.
			ctrl := gomock.NewController(t)
			runtime := NewMockRuntime(ctrl)
			contexts := NewMockContextIssuer(ctrl)
			service := New(runtime, contexts, nil)
			commitToolPolicyHandlers(service, "first", "policy-first")
			commitToolPolicyHandlers(service, "second", "policy-later")
			contexts.EXPECT().IssueContext("first").Return(extension.Context{}, nil)
			runtime.EXPECT().HandleToolPolicy(gomock.Any(), "first", "policy-first", gomock.Any()).
				Return(test.action, test.handler)

			// Act through the chain.
			_, rejection, err := service.ApplyToolPolicy(t.Context(), policyCall(t, `{}`), test.validate)

			// Assert the error with its cause and no rejection; the strict mocks show that the later handler did not run.
			require.Error(t, err)
			assert.False(t, rejection.IsPresent())
			if test.cause != nil {
				require.ErrorIs(t, err, test.cause)
			}
		})
	}
}

// TestApplyToolPolicyCancellation verifies that a chain canceled by its owning context returns the cancellation.
func TestApplyToolPolicyCancellation(t *testing.T) {
	t.Parallel()
	// Arrange one handler and a canceled owning context.
	ctrl := gomock.NewController(t)
	service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), nil)
	commitToolPolicyHandlers(service, "first", "policy-first")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	// Act through the chain.
	_, _, err := service.ApplyToolPolicy(ctx, policyCall(t, `{}`), acceptAnyArguments)

	// Assert the cancellation; the strict mocks show that no handler ran.
	require.ErrorIs(t, err, context.Canceled)
}

// TestToolPolicyKindIsAccepted verifies that middleware registration accepts the tool-policy kind.
func TestToolPolicyKindIsAccepted(t *testing.T) {
	t.Parallel()
	// Arrange a registration with one tool-policy handler.
	ctrl := gomock.NewController(t)
	service := New(NewMockRuntime(ctrl), NewMockContextIssuer(ctrl), nil)
	registration := startup.PendingRegistration{
		ID: "extension", Path: "", Tools: nil,
		Handlers: []startup.RawHandlerDescriptor{{Present: true, ID: "policy", Kind: startup.RawHandlerKindToolPolicy}},
	}

	// Act through validation.
	accepted, err := service.ValidateMiddlewareHandlers(registration)

	// Assert acceptance.
	require.NoError(t, err)
	assert.Equal(t, []startup.AcceptedHandler{{ID: "policy", Kind: startup.RawHandlerKindToolPolicy}}, accepted)
}
