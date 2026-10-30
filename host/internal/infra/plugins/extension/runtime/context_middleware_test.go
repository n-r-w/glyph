//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// privateReplay is provider-owned replay data that must never reach an extension.
const privateReplay = "encrypted-private-replay"

// TestSystemInstructionsMapping maps original/current instructions and every decoded action kind.
func TestSystemInstructionsMapping(t *testing.T) {
	t.Parallel()
	// Arrange one instruction invocation.
	input := middleware.Transformation[string]{Context: middlewareBinding(), Original: "base", Current: "base A"}

	// Act by mapping the invocation and three handler responses.
	request := mapSystemInstructionsRequest("instructions", input)
	preserved := mapSystemInstructionsAction(instructionsResponse(
		extensionpb.SystemInstructionsAction_builder{Preserve: new(true), Replace: nil}.Build(),
	))
	replaced := mapSystemInstructionsAction(instructionsResponse(
		extensionpb.SystemInstructionsAction_builder{Preserve: nil, Replace: new("base B")}.Build(),
	))
	missing := mapSystemInstructionsAction(instructionsResponse(
		extensionpb.SystemInstructionsAction_builder{Preserve: nil, Replace: nil}.Build(),
	))

	// Assert the public payload and actions left for owner validation.
	require.Equal(t, "instructions", request.GetHandlerId())
	require.Equal(t, "binding", request.GetContext().GetContextId())
	require.Equal(t, "base", request.GetSystemInstructions().GetOriginal())
	require.Equal(t, "base A", request.GetSystemInstructions().GetCurrent())
	require.Equal(t, middleware.Transformed[string]{Kind: middleware.Preserve, Replacement: ""}, preserved)
	require.Equal(t, middleware.Transformed[string]{Kind: middleware.Replace, Replacement: "base B"}, replaced)
	require.Equal(t, middleware.Transformed[string]{Kind: 0, Replacement: ""}, missing)
}

// TestModelContextReplayRoundTrip hides private replay and restores only references retained by a replacement.
func TestModelContextReplayRoundTrip(t *testing.T) {
	t.Parallel()
	// Arrange an original with private replay and a current value from which an earlier handler removed it.
	original := contextHistory(t, mo.Some(model.ProviderContext{Source: model.ProviderContextSource{
		ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.None[string](),
	}, Payload: []byte(privateReplay)}))
	current := contextHistory(t, mo.None[model.ProviderContext]())
	input := middleware.Transformation[[]agent.HistoryEntry]{
		Context: middlewareBinding(), Original: original, Current: current,
	}
	projection := newReplayProjection()

	// Act by projecting the invocation and returning its original, current, and preserve actions.
	request, err := mapModelContextRequest("context", input, projection)
	require.NoError(t, err)
	encoded, err := proto.Marshal(request)
	require.NoError(t, err)
	fromOriginal, originalErr := mapModelContextAction(contextResponse(
		extensionpb.ModelContextAction_builder{Preserve: nil, Replace: request.GetModelContext().GetOriginal()}.Build(),
	), projection)
	fromCurrent, currentErr := mapModelContextAction(contextResponse(
		extensionpb.ModelContextAction_builder{Preserve: nil, Replace: request.GetModelContext().GetCurrent()}.Build(),
	), projection)
	preserved, preserveErr := mapModelContextAction(contextResponse(
		extensionpb.ModelContextAction_builder{Preserve: new(true), Replace: nil}.Build(),
	), projection)

	// Assert confidential projection, restoration from the original, deliberate discard, and preservation.
	require.NotContains(t, string(encoded), privateReplay)
	require.NotEmpty(t, reasoningReference(request.GetModelContext().GetOriginal()))
	require.NotContains(t, reasoningReference(request.GetModelContext().GetOriginal()), privateReplay)
	require.Empty(t, reasoningReference(request.GetModelContext().GetCurrent()))
	require.NoError(t, originalErr)
	require.Equal(
		t,
		middleware.Transformed[[]agent.HistoryEntry]{Kind: middleware.Replace, Replacement: original},
		fromOriginal,
	)
	require.NoError(t, currentErr)
	require.Equal(
		t,
		middleware.Transformed[[]agent.HistoryEntry]{Kind: middleware.Replace, Replacement: current},
		fromCurrent,
	)
	require.NoError(t, preserveErr)
	require.Equal(
		t,
		middleware.Transformed[[]agent.HistoryEntry]{Kind: middleware.Preserve, Replacement: nil},
		preserved,
	)
}

// TestModelContextReplayRejectsInvalidReferences fails unknown, altered, duplicated, and misplaced references.
func TestModelContextReplayRejectsInvalidReferences(t *testing.T) {
	t.Parallel()
	for name, edit := range map[string]func(contents []*extensionpb.ReplayableModelContent, reference string){
		"unknown": func(contents []*extensionpb.ReplayableModelContent, _ string) {
			contents[1].SetReplayReference("unknown")
		},
		"altered": func(contents []*extensionpb.ReplayableModelContent, reference string) {
			contents[1].SetReplayReference(reference + "0")
		},
		"duplicated": func(contents []*extensionpb.ReplayableModelContent, reference string) {
			duplicate := proto.CloneOf(contents[1])
			duplicate.SetReplayReference(reference)
			contents[2] = duplicate
		},
		"misplaced": func(contents []*extensionpb.ReplayableModelContent, reference string) {
			contents[0].SetReplayReference(reference)
			contents[1].SetReplayReference("")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a projected original whose replacement edits its replay reference.
			original := contextHistory(t, mo.Some(model.ProviderContext{Source: model.ProviderContextSource{
				ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.None[string](),
			}, Payload: []byte(privateReplay)}))
			projection := newReplayProjection()
			request, err := mapModelContextRequest("context", middleware.Transformation[[]agent.HistoryEntry]{
				Context: middlewareBinding(), Original: original, Current: original,
			}, projection)
			require.NoError(t, err)
			replacement := proto.CloneOf(request.GetModelContext().GetOriginal())
			edit(replacement.GetEntries()[1].GetModel().GetContent(), reasoningReference(replacement))

			// Act by decoding the handler replacement.
			_, err = mapModelContextAction(contextResponse(
				extensionpb.ModelContextAction_builder{Preserve: nil, Replace: replacement}.Build(),
			), projection)

			// Assert the replacement fails as an invalid replay reference.
			require.ErrorContains(t, err, "replay reference")
		})
	}
}

// contextHistory builds user, model, and tool-result entries with optional reasoning replay.
func contextHistory(t *testing.T, replay mo.Option[model.ProviderContext]) []agent.HistoryEntry {
	t.Helper()
	arguments, err := model.NewToolCallArguments([]byte(`{"path":"a"}`))
	require.NoError(t, err)
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentText, Text: mo.Some("answer"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning, Text: mo.Some("thinking"), Final: true,
				ProviderContext: replay, ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.Some(model.ToolCall{ID: "call", Name: "read", Arguments: arguments}),
			},
		},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](),
		Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	return []agent.HistoryEntry{
		{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("question")),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		},
		{
			Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response),
			ToolResult: mo.None[agent.ToolResult](),
		},
		{
			Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
			ToolResult: mo.Some(agent.ToolResult{CallID: "call", ToolName: "read", Contents: []tool.ResultContent{{
				Kind: tool.ResultContentText, Text: mo.Some("file"), Image: mo.None[tool.ResultImage](),
			}}, IsError: false}),
		},
	}
}

// reasoningReference returns the replay reference of the projected reasoning block.
func reasoningReference(history *extensionpb.ModelContextHistory) string {
	return history.GetEntries()[1].GetModel().GetContent()[1].GetReplayReference()
}

// instructionsResponse wraps one instruction action in a handler response.
func instructionsResponse(action *extensionpb.SystemInstructionsAction) *extensionpb.HandleResponse {
	response := new(extensionpb.HandleResponse)
	response.SetSystemInstructions(action)
	return response
}

// contextResponse wraps one history action in a handler response.
func contextResponse(action *extensionpb.ModelContextAction) *extensionpb.HandleResponse {
	response := new(extensionpb.HandleResponse)
	response.SetModelContext(action)
	return response
}

// middlewareBinding returns one issued extension binding.
func middlewareBinding() extension.Context {
	return extension.Context{
		ID: "binding", ExtensionID: "extension", RuntimeInstanceID: "runtime", SessionID: "session",
		WorkingDirectory: "/project",
	}
}
