//go:build !integration

package tools

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
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
		ExecuteTool(t.Context(), "extension", "read", []byte(`{"path":"file"}`), gomock.Any()).
		Return(tool.Result{Contents: tool.TextContents("content"), IsError: false}, nil)
	// Act list tools, reject invalid arguments, and execute valid deterministic arguments.
	listed := service.Tools()
	invalid, invalidErr := service.Execute(
		t.Context(),
		model.ToolCall{ID: "bad", Name: "read", Arguments: map[string]any{}},
		func(tool.Progress) error { return nil },
	)
	result, executeErr := service.Execute(
		t.Context(),
		model.ToolCall{ID: "call", Name: "read", Arguments: map[string]any{"path": "file"}},
		func(tool.Progress) error { return nil },
	)
	// Assert sorted listing and model-visible results preserve behavior.
	require.NoError(t, invalidErr)
	assert.True(t, invalid.IsError)
	assert.Contains(t, invalid.Contents[0].Text.OrEmpty(), `invalid arguments for tool "read"`)
	require.NoError(t, executeErr)
	assert.Equal(t, "call", result.CallID)
	assert.Equal(t, "read", result.ToolName)
	assert.Equal(t, tool.TextContents("content"), result.Contents)
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
		model.ToolCall{ID: "call", Name: "read", Arguments: map[string]any{"path": "file"}},
		func(tool.Progress) error { return nil },
	)
	// Assert exact existing unavailable text.
	require.NoError(t, executeErr)
	assert.True(t, result.IsError)
	assert.Equal(t, `tool "read" is unavailable`, result.Contents[0].Text.OrEmpty())
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
