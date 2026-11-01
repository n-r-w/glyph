//go:build !integration

package tools

import (
	"slices"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

const (
	// replacementSchemaJSON is the input schema of the replacing tool; the original tool schema requires a path.
	replacementSchemaJSON = `{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],` +
		`"additionalProperties":false}`
	// replacementDescription is the description of the replacing tool.
	replacementDescription = "Replacement."
)

// replacementRegistration builds an accepted registration with one tool named read that is ordinary or replacing.
func replacementRegistration(id string, replaces bool) startup.AcceptedRegistration {
	read := descriptor("read")
	read.ReplacesTool = replaces
	return startup.AcceptedRegistration{ID: id, Path: "/" + id, Tools: []tool.Descriptor{read}, Handlers: nil}
}

// permutations returns every order of the items.
func permutations[T any](items []T) [][]T {
	if len(items) <= 1 {
		return [][]T{slices.Clone(items)}
	}
	result := make([][]T, 0)
	for index := range items {
		rest := slices.Clone(items)
		first := rest[index]
		rest = slices.Delete(rest, index, index+1)
		for _, tail := range permutations(rest) {
			result = append(result, append([]T{first}, tail...))
		}
	}
	return result
}

// TestServiceConflictsApplyReplacementRules verifies the four collision rules in every registration order.
func TestServiceConflictsApplyReplacementRules(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		// registrations declare the tool read; the flag marks a declared replacement.
		registrations []startup.AcceptedRegistration
		// ordinary lists the owners of the ordinary conflict, or nil.
		ordinary []string
		// replacements lists the owners of the replacement conflict, or nil.
		replacements []string
	}{
		"one ordinary and one replacement": {
			registrations: []startup.AcceptedRegistration{
				replacementRegistration("base", false), replacementRegistration("replacer", true),
			},
			ordinary: nil, replacements: nil,
		},
		"two replacements and the original": {
			registrations: []startup.AcceptedRegistration{
				replacementRegistration("base", false),
				replacementRegistration("first", true),
				replacementRegistration("second", true),
			},
			ordinary: nil, replacements: []string{"first", "second"},
		},
		"two ordinary descriptors": {
			registrations: []startup.AcceptedRegistration{
				replacementRegistration("first", false), replacementRegistration("second", false),
			},
			ordinary: []string{"first", "second"}, replacements: nil,
		},
		"replacement without the original": {
			registrations: []startup.AcceptedRegistration{replacementRegistration("replacer", true)},
			ordinary:      nil, replacements: nil,
		},
		"replacement next to an ordinary conflict": {
			registrations: []startup.AcceptedRegistration{
				replacementRegistration("first", false),
				replacementRegistration("second", false),
				replacementRegistration("replacer", true),
			},
			ordinary: []string{"first", "second"}, replacements: nil,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, registrations := range permutations(test.registrations) {
				// Arrange the registrations in one load order.
				service := New(nil)

				// Act by detecting the conflicts.
				ordinary := service.NameConflicts(registrations)
				replacements := service.ReplacementConflicts(registrations)

				// Assert the findings do not depend on the order.
				if test.ordinary == nil {
					assert.Empty(t, ordinary)
				} else {
					require.Len(t, ordinary, 1)
					assert.Equal(t, test.ordinary, ordinary[0].PluginIDs)
				}
				if test.replacements == nil {
					assert.Empty(t, replacements)
				} else {
					require.Len(t, replacements, 1)
					assert.Equal(t, "read", replacements[0].ToolName)
					assert.Equal(t, test.replacements, replacements[0].Issue.PluginIDs)
				}
			}
		})
	}
}

// TestServiceCommitSelectsReplacementDescriptorSchemaAndOwner verifies that the replacement wins in every load order
// with its description, its input schema, and its owning extension, while the other tools of both stay available.
func TestServiceCommitSelectsReplacementDescriptorSchemaAndOwner(t *testing.T) {
	t.Parallel()
	for _, replacerFirst := range []bool{false, true} {
		// Arrange an original and a replacement of read with different schemas, and one more tool for each.
		controller := gomock.NewController(t)
		runtime := NewMockRuntime(controller)
		contexts := NewMockContextIssuer(controller)
		contexts.EXPECT().IssueContext(gomock.Any()).Return(extension.Context{}, nil).AnyTimes()
		runtime.EXPECT().ToolRuntimeAvailable(gomock.Any()).Return(true).AnyTimes()
		service := New(runtime)
		service.BindContextIssuer(contexts)
		bindPassThroughPolicy(service, controller)
		base := startup.PendingRegistration{
			ID: "base", Path: "/base", Handlers: nil,
			Tools: []startup.RawToolDescriptor{rawDescriptor("read", "Original."), rawDescriptor("write", "Write.")},
		}
		replacement := rawDescriptor("read", replacementDescription)
		replacement.InputSchemaJSON = []byte(replacementSchemaJSON)
		replacement.ReplacesTool = true
		replacer := startup.PendingRegistration{
			ID: "replacer", Path: "/replacer", Handlers: nil,
			Tools: []startup.RawToolDescriptor{replacement, rawDescriptor("grep", "Grep.")},
		}
		accepted := make([]startup.AcceptedRegistration, 0, 2)
		for _, pending := range []startup.PendingRegistration{base, replacer} {
			descriptors, err := service.ValidateLocal(pending)
			require.NoError(t, err)
			accepted = append(accepted, startup.AcceptedRegistration{
				ID: pending.ID, Path: pending.Path, Tools: descriptors, Handlers: nil,
			})
		}
		if replacerFirst {
			slices.Reverse(accepted)
		}
		service.Commit(accepted)
		runtime.EXPECT().
			ExecuteTool(gomock.Any(), "replacer", "read", []byte(`{"value":"x"}`), gomock.Any(), gomock.Any()).
			Return(tool.Result{Contents: tool.TextContents("replaced"), IsError: false}, nil)

		// Act by listing the tools, calling read with arguments of the replacement schema, and with arguments of the
		// original schema.
		listed := service.Tools()
		replaced, replacedErr := service.Execute(
			t.Context(), model.ToolCall{ID: "call-1", Name: "read", Arguments: testToolCallArguments(`{"value":"x"}`)},
			func(tool.Progress) error { return nil },
		)
		original, originalErr := service.Execute(
			t.Context(), model.ToolCall{ID: "call-2", Name: "read", Arguments: testToolCallArguments(`{"path":"f"}`)},
			func(tool.Progress) error { return nil },
		)

		// Assert one read tool with the replacement description, execution by the replacing extension under its
		// schema, and the other tools of both extensions.
		names := make([]string, 0, len(listed))
		for _, descriptor := range listed {
			names = append(names, descriptor.Name)
			if descriptor.Name == "read" {
				assert.Equal(t, replacementDescription, descriptor.Description)
				assert.JSONEq(t, replacementSchemaJSON, string(descriptor.InputSchemaJSON))
			}
		}
		assert.Equal(t, []string{"grep", "read", "write"}, names)
		require.NoError(t, replacedErr)
		assert.False(t, replaced.IsError)
		assert.Equal(t, tool.TextContents("replaced"), replaced.Contents)
		require.NoError(t, originalErr)
		assert.True(t, original.IsError)
		assert.Equal(t, mo.Some(agent.ToolErrorInvalidArguments), original.ErrorCategory)
	}
}

// TestStartupAcceptsReplacementOfRegistrationNotRejectedByOrdinaryConflict verifies, in every load order, that a
// replacement of a registration that an ordinary conflict rejects does not conflict with another replacement of the
// same name: the other replacement is accepted as an ordinary tool.
func TestStartupAcceptsReplacementOfRegistrationNotRejectedByOrdinaryConflict(t *testing.T) {
	t.Parallel()
	// Arrange A and B that both own the ordinary tool x, A that also replaces read, and C that replaces read.
	replacement := rawDescriptor("read", replacementDescription)
	replacement.ReplacesTool = true
	pending := []startup.PendingRegistration{
		{ID: "a", Path: "/a", Tools: []startup.RawToolDescriptor{rawDescriptor("x", "X."), replacement}, Handlers: nil},
		{ID: "b", Path: "/b", Tools: []startup.RawToolDescriptor{rawDescriptor("x", "X.")}, Handlers: nil},
		{ID: "c", Path: "/c", Tools: []startup.RawToolDescriptor{replacement}, Handlers: nil},
	}
	for _, ordered := range permutations(pending) {
		controller := gomock.NewController(t)
		runtimes := startup.NewMockRuntimeLoader(controller)
		sessionTree := startup.NewMockSessionTreeRegistrar(controller)
		lifecycle := startup.NewMockLifecycleRegistrar(controller)
		toolRuntime := NewMockRuntime(controller)
		toolRuntime.EXPECT().ToolRuntimeAvailable("c").Return(true).AnyTimes()
		tools := New(toolRuntime)
		runtimes.EXPECT().
			LoadPending(gomock.Any(), gomock.Any()).
			Return(startup.PendingLoad{Issues: nil, Registrations: ordered}, nil)
		sessionTree.EXPECT().
			ValidateSessionTreeHandlers(gomock.Any()).
			Return([]startup.AcceptedHandler{}, nil).
			AnyTimes()
		lifecycle.EXPECT().ValidateLifecycleHandlers(gomock.Any()).Return([]startup.AcceptedHandler{}, nil).AnyTimes()
		sessionTree.EXPECT().CommitSessionTreeHandlers(gomock.Any())
		lifecycle.EXPECT().CommitLifecycleHandlers(gomock.Any())
		runtimes.EXPECT().RejectPending([]string{"a", "b"})
		runtimes.EXPECT().Accept(gomock.Any())

		// Act by loading the registrations in one order.
		report, err := startup.New(runtimes, tools, sessionTree, lifecycle, nil).Load(
			t.Context(), startup.Request{DataDirectory: "/data", ExtensionDirectory: "/plugins"},
		)

		// Assert only the ordinary conflict is reported, C stays accepted, and its replacement is the read tool.
		require.NoError(t, err)
		require.Len(t, report.Issues, 1)
		assert.Equal(t, []string{"a", "b"}, report.Issues[0].PluginIDs)
		require.Len(t, report.Extensions, 1)
		assert.Equal(t, "c", report.Extensions[0].ID)
		listed := tools.Tools()
		require.Len(t, listed, 1)
		assert.Equal(t, "read", listed[0].Name)
	}
}
