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

const validSchemaJSON = `{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`

// TestServiceValidatesLocalDescriptors verifies fields, schemas, constraints, and duplicate names.
func TestServiceValidatesLocalDescriptors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		tools     []startup.RawToolDescriptor
		errorText string
	}{
		"missing": {
			tools: []startup.RawToolDescriptor{
				{
					Present:             false,
					Name:                "",
					Description:         "",
					InputSchemaJSON:     nil,
					ConstrainedSampling: mo.None[startup.RawConstrainedSampling](),
				},
			},
			errorText: "descriptor 0 is missing",
		},
		"empty name": {
			tools:     []startup.RawToolDescriptor{rawDescriptor("", "description")},
			errorText: "descriptor 0 has an empty name",
		},
		"empty description": {
			tools:     []startup.RawToolDescriptor{rawDescriptor("read", "")},
			errorText: `tool "read" has an empty description`,
		},
		"duplicate": {
			tools:     []startup.RawToolDescriptor{rawDescriptor("read", "first"), rawDescriptor("read", "second")},
			errorText: `tool name "read" is duplicated`,
		},
		"invalid schema": {
			tools: []startup.RawToolDescriptor{
				{
					Present:             true,
					Name:                "read",
					Description:         "Read.",
					InputSchemaJSON:     []byte(`{"type":"string"}`),
					ConstrainedSampling: mo.None[startup.RawConstrainedSampling](),
				},
			},
			errorText: `tool "read" input schema: schema root type must be object`,
		},
		"missing constraint config": {
			tools: []startup.RawToolDescriptor{
				{
					Present:         true,
					Name:            "read",
					Description:     "Read.",
					InputSchemaJSON: []byte(validSchemaJSON),
					ConstrainedSampling: mo.Some(
						startup.RawConstrainedSampling{
							Kind:                 startup.RawConstrainedSamplingMissing,
							JSONSchemaPresent:    false,
							JSONSchemaStrictness: startup.RawJSONSchemaStrictnessUnspecified,
							Grammar: startup.RawGrammar{
								Present: false,
								Lark:    mo.None[string](),
								Regex:   mo.None[string](),
							},
						},
					),
				},
			},
			errorText: `tool "read" constrained sampling: config is missing`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one invalid raw registration.
			service := New(nil)
			// Act validate local tool policy.
			_, err := service.ValidateLocal(
				startup.PendingRegistration{ID: "extension", Path: "/extension", Tools: test.tools, Handlers: nil},
			)
			// Assert the existing validation error remains exact.
			require.EqualError(t, err, test.errorText)
		})
	}
}

// TestServiceMapsConstrainedSampling verifies accepted raw constraints map to domain descriptors.
func TestServiceMapsConstrainedSampling(t *testing.T) {
	t.Parallel()
	// Arrange one strict JSON Schema constraint.
	service := New(nil)
	raw := rawDescriptor("read", "Read.")
	raw.ConstrainedSampling = mo.Some(
		startup.RawConstrainedSampling{
			Kind:                 startup.RawConstrainedSamplingJSONSchema,
			JSONSchemaPresent:    true,
			JSONSchemaStrictness: startup.RawJSONSchemaStrictnessRequire,
			Grammar:              startup.RawGrammar{Present: false, Lark: mo.None[string](), Regex: mo.None[string]()},
		},
	)
	// Act validate and map the descriptor.
	descriptors, err := service.ValidateLocal(
		startup.PendingRegistration{
			ID:       "extension",
			Path:     "/extension",
			Tools:    []startup.RawToolDescriptor{raw},
			Handlers: nil,
		},
	)
	// Assert the mapped strictness and presence.
	require.NoError(t, err)
	require.Len(t, descriptors, 1)
	constraint, present := descriptors[0].ConstrainedSampling.Get()
	require.True(t, present)
	assert.Equal(t, tool.ConstrainedSamplingJSONSchema, constraint.Kind)
	assert.Equal(t, tool.JSONSchemaStrictRequire, constraint.JSONSchemaStrictness.OrEmpty())
}

// TestServiceValidatesConstrainedSamplingErrors verifies strictness and grammar policy errors.
func TestServiceValidatesConstrainedSamplingErrors(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		constraint startup.RawConstrainedSampling
		errorText  string
	}{
		"unspecified strictness": {
			constraint: startup.RawConstrainedSampling{
				Kind:                 startup.RawConstrainedSamplingJSONSchema,
				JSONSchemaPresent:    true,
				JSONSchemaStrictness: startup.RawJSONSchemaStrictnessUnspecified,
				Grammar: startup.RawGrammar{
					Present: false,
					Lark:    mo.None[string](),
					Regex:   mo.None[string](),
				},
			},
			errorText: `tool "read" constrained sampling: JSON Schema strictness is unspecified`,
		},
		"invalid strictness": {
			constraint: startup.RawConstrainedSampling{
				Kind:                 startup.RawConstrainedSamplingJSONSchema,
				JSONSchemaPresent:    true,
				JSONSchemaStrictness: startup.RawJSONSchemaStrictness(99),
				Grammar: startup.RawGrammar{
					Present: false,
					Lark:    mo.None[string](),
					Regex:   mo.None[string](),
				},
			},
			errorText: `tool "read" constrained sampling: JSON Schema strictness is invalid`,
		},
		"empty grammar": {
			constraint: startup.RawConstrainedSampling{
				Kind:                 startup.RawConstrainedSamplingGrammar,
				JSONSchemaPresent:    false,
				JSONSchemaStrictness: startup.RawJSONSchemaStrictnessUnspecified,
				Grammar: startup.RawGrammar{
					Present: true,
					Lark:    mo.None[string](),
					Regex:   mo.None[string](),
				},
			},
			errorText: `tool "read" constrained sampling: grammar requires at least one nonempty grammar variant`,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one invalid constrained sampling registration.
			service := New(nil)
			raw := rawDescriptor("read", "Read.")
			raw.ConstrainedSampling = mo.Some(test.constraint)
			// Act validate constrained sampling policy.
			_, err := service.ValidateLocal(
				startup.PendingRegistration{
					ID:       "extension",
					Path:     "/extension",
					Tools:    []startup.RawToolDescriptor{raw},
					Handlers: nil,
				},
			)
			// Assert the exact existing policy error.
			require.EqualError(t, err, test.errorText)
		})
	}
}

// TestServiceConflictsRejectsEveryOwnerDeterministically verifies global conflict grouping and order.
func TestServiceConflictsRejectsEveryOwnerDeterministically(t *testing.T) {
	t.Parallel()
	// Arrange registrations with two conflicting names and one safe tool.
	service := New(nil)
	registrations := []startup.AcceptedRegistration{
		{
			ID:       "second",
			Path:     "/second",
			Tools:    []tool.Descriptor{descriptor("write"), descriptor("read")},
			Handlers: nil,
		},
		{ID: "first", Path: "/first", Tools: []tool.Descriptor{descriptor("read"), descriptor("write")}, Handlers: nil},
		{ID: "safe", Path: "/safe", Tools: []tool.Descriptor{descriptor("bash")}, Handlers: nil},
	}
	// Act detect global conflicts.
	issues := service.Conflicts(registrations)
	// Assert issue names and owner IDs are deterministic.
	require.Len(t, issues, 2)
	assert.EqualError(t, issues[0].Err, `tool name "read" conflicts`)
	assert.Equal(t, []string{"first", "second"}, issues[0].PluginIDs)
	assert.EqualError(t, issues[1].Err, `tool name "write" conflicts`)
	assert.Equal(t, []string{"first", "second"}, issues[1].PluginIDs)
}

// TestServiceListsAndExecutesAcceptedTools verifies sorting, availability, argument validation, and result mapping.
func TestServiceListsAndExecutesAcceptedTools(t *testing.T) {
	t.Parallel()
	// Arrange two locally valid tools and one available owning runtime.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	service := New(runtime)
	contexts := NewMockContextIssuer(controller)
	binding := extension.Context{
		ID:                "binding",
		ExtensionID:       "extension",
		RuntimeInstanceID: "runtime",
		SessionID:         "session",
		WorkingDirectory:  "/project",
	}
	contexts.EXPECT().IssueContext("extension").Return(binding, nil)
	service.BindContextIssuer(contexts)
	bindPassThroughPolicy(service, controller)
	registration := startup.PendingRegistration{
		ID:       "extension",
		Path:     "/extension",
		Tools:    []startup.RawToolDescriptor{rawDescriptor("write", "Write."), rawDescriptor("read", "Read.")},
		Handlers: nil,
	}
	descriptors, err := service.ValidateLocal(registration)
	require.NoError(t, err)
	accepted := []startup.AcceptedRegistration{{ID: "extension", Path: "/extension", Tools: descriptors, Handlers: nil}}
	service.Commit(accepted)
	runtime.EXPECT().ToolRuntimeAvailable("extension").Return(true).Times(3)
	runtime.EXPECT().
		ExecuteTool(t.Context(), "extension", "read", []byte(`{ "path":"\u0066ile" }`), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, _ []byte, _ tool.ProgressHandler, supplied extension.Context) (tool.Result, error) {
			assert.Equal(t, binding, supplied)
			return tool.Result{Contents: tool.TextContents("content"), IsError: false}, nil
		})
	// Act list tools, reject invalid arguments, and execute valid deterministic arguments.
	listed := service.Tools()
	invalid, invalidErr := service.Execute(
		t.Context(),
		model.ToolCall{ID: "bad", Name: "read", Arguments: testToolCallArguments(`{}`)},
		func(tool.Progress) error { return nil },
	)
	result, executeErr := service.Execute(
		t.Context(),
		model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{ "path":"\u0066ile" }`)},
		func(tool.Progress) error { return nil },
	)
	// Assert sorted listing and model-visible results preserve behavior.
	require.NoError(t, invalidErr)
	assert.True(t, invalid.IsError)
	assert.Contains(t, invalid.Contents[0].Text.OrEmpty(), `invalid arguments for tool "read"`)
	assert.Equal(t, mo.Some(agent.ToolErrorInvalidArguments), invalid.ErrorCategory)
	require.NoError(t, executeErr)
	assert.Equal(t, "call", result.CallID)
	assert.Equal(t, "read", result.ToolName)
	assert.Equal(t, tool.TextContents("content"), result.Contents)
	assert.True(t, result.ErrorCategory.IsAbsent())
	assert.Equal(t, []string{"read", "write"}, []string{listed[0].Name, listed[1].Name})
}

// TestServiceUsesOneAvailabilityDecisionPerExtensionSnapshot verifies one owner cannot contribute a partial tool list.
func TestServiceUsesOneAvailabilityDecisionPerExtensionSnapshot(t *testing.T) {
	t.Parallel()

	// Arrange two accepted tools from one extension and availability that changes after its first check.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	service := New(runtime)
	registration := startup.PendingRegistration{
		ID:       "extension",
		Path:     "/extension",
		Tools:    []startup.RawToolDescriptor{rawDescriptor("write", "Write."), rawDescriptor("read", "Read.")},
		Handlers: nil,
	}
	descriptors, err := service.ValidateLocal(registration)
	require.NoError(t, err)
	service.Commit([]startup.AcceptedRegistration{{
		ID:       "extension",
		Path:     "/extension",
		Tools:    descriptors,
		Handlers: nil,
	}})
	availabilityCalls := 0
	runtime.EXPECT().ToolRuntimeAvailable("extension").AnyTimes().DoAndReturn(func(string) bool {
		availabilityCalls++
		return availabilityCalls == 1
	})

	// Act by taking one tool snapshot.
	listed := service.Tools()

	// Assert the first availability decision applies to both sorted tools.
	require.Len(t, listed, 2)
	assert.Equal(t, []string{"read", "write"}, []string{listed[0].Name, listed[1].Name})
	assert.Equal(t, 1, availabilityCalls)
}

// TestServiceReturnsUnavailableResult verifies missing and unavailable owners remain model-visible errors.
func TestServiceReturnsUnavailableResult(t *testing.T) {
	t.Parallel()
	// Arrange an accepted tool whose runtime is unavailable.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	service := New(runtime)
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
	runtime.EXPECT().ToolRuntimeAvailable("extension").Return(false)
	// Act execute the unavailable tool.
	result, executeErr := service.Execute(
		t.Context(),
		model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{ "path":"\u0066ile" }`)},
		func(tool.Progress) error { return nil },
	)
	// Assert exact existing unavailable text.
	require.NoError(t, executeErr)
	assert.True(t, result.IsError)
	assert.Equal(t, `tool "read" is unavailable`, result.Contents[0].Text.OrEmpty())
	assert.Equal(t, mo.Some(agent.ToolErrorUnavailableTool), result.ErrorCategory)
}

// TestServiceCategorizesReportedError verifies that an error result reported by the tool gets the tool-failure
// category while a successful result gets none.
func TestServiceCategorizesReportedError(t *testing.T) {
	t.Parallel()
	// Arrange an accepted tool that reports an error on its first call and succeeds on its second call.
	controller := gomock.NewController(t)
	runtime := NewMockRuntime(controller)
	service := New(runtime)
	contexts := NewMockContextIssuer(controller)
	contexts.EXPECT().IssueContext("extension").Return(extension.Context{}, nil).Times(2)
	service.BindContextIssuer(contexts)
	bindPassThroughPolicy(service, controller)
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
	runtime.EXPECT().ToolRuntimeAvailable("extension").Return(true).Times(2)
	reported := true
	runtime.EXPECT().
		ExecuteTool(t.Context(), "extension", "read", gomock.Any(), gomock.Any(), gomock.Any()).
		Times(2).
		DoAndReturn(func(context.Context, string, string, []byte, tool.ProgressHandler, extension.Context) (tool.Result, error) {
			defer func() { reported = false }()
			return tool.Result{Contents: tool.TextContents("result"), IsError: reported}, nil
		})
	call := model.ToolCall{ID: "call", Name: "read", Arguments: testToolCallArguments(`{ "path":"file" }`)}

	// Act execute the tool twice.
	failed, failedErr := service.Execute(t.Context(), call, func(tool.Progress) error { return nil })
	succeeded, succeededErr := service.Execute(t.Context(), call, func(tool.Progress) error { return nil })

	// Assert the category only on the reported error.
	require.NoError(t, failedErr)
	require.NoError(t, succeededErr)
	assert.Equal(t, mo.Some(agent.ToolErrorFailure), failed.ErrorCategory)
	assert.True(t, succeeded.ErrorCategory.IsAbsent())
}

// bindPassThroughPolicy binds a tool policy that allows every call unchanged.
func bindPassThroughPolicy(service *Service, controller *gomock.Controller) {
	policy := NewMockToolPolicy(controller)
	policy.EXPECT().ApplyToolPolicy(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, call model.ToolCall, _ func([]byte) error) (model.ToolCall, mo.Option[string], error) {
			return call, mo.None[string](), nil
		},
	).AnyTimes()
	service.BindToolPolicy(policy)
}

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
		Return(tool.Result{Contents: tool.TextContents("content"), IsError: false}, nil)

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

// rawDescriptor constructs one valid raw tool descriptor.
func rawDescriptor(name, description string) startup.RawToolDescriptor {
	return startup.RawToolDescriptor{
		Present:             true,
		Name:                name,
		Description:         description,
		InputSchemaJSON:     []byte(validSchemaJSON),
		ConstrainedSampling: mo.None[startup.RawConstrainedSampling](),
	}
}

// descriptor constructs one accepted descriptor for conflict tests.
func descriptor(name string) tool.Descriptor {
	return tool.Descriptor{
		Name:                name,
		Description:         name,
		InputSchemaJSON:     []byte(validSchemaJSON),
		ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
	}
}
