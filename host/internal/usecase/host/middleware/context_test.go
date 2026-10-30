//go:build !integration

package middleware

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestContextComposition composes instructions and history in registration order with original/current semantics.
func TestContextComposition(t *testing.T) {
	t.Parallel()
	// Arrange two extensions whose handlers append, preserve, and replace from the immutable original.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts)
	first := startup.PendingRegistration{ID: "first", Path: "", Tools: nil, Handlers: []startup.RawHandlerDescriptor{
		{Present: true, ID: "history-a", Kind: startup.RawHandlerKindModelContext},
		{Present: true, ID: "instructions-a", Kind: startup.RawHandlerKindSystemInstructions},
		{Present: true, ID: "headers", Kind: startup.RawHandlerKindProviderHeaders},
	}}
	second := startup.PendingRegistration{ID: "second", Path: "", Tools: nil, Handlers: []startup.RawHandlerDescriptor{
		{Present: true, ID: "instructions-b", Kind: startup.RawHandlerKindSystemInstructions},
		{Present: true, ID: "history-keep", Kind: startup.RawHandlerKindModelContext},
		{Present: true, ID: "history-b", Kind: startup.RawHandlerKindModelContext},
	}}
	firstHandlers, err := service.ValidateMiddlewareHandlers(first)
	require.NoError(t, err)
	secondHandlers, err := service.ValidateMiddlewareHandlers(second)
	require.NoError(t, err)
	service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{
		{ID: "first", Path: "", Tools: nil, Handlers: firstHandlers},
		{ID: "second", Path: "", Tools: nil, Handlers: secondHandlers},
	})
	composer := service.NewContextComposer()
	service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{{
		ID: "late", Path: "", Tools: nil,
		Handlers: []startup.AcceptedHandler{{ID: "late", Kind: startup.RawHandlerKindSystemInstructions}},
	}})
	contexts.EXPECT().IssueContext("first").Return(extension.Context{}, nil).Times(2)
	contexts.EXPECT().IssueContext("second").Return(extension.Context{}, nil).Times(3)
	gomock.InOrder(
		runtime.EXPECT().HandleSystemInstructions(gomock.Any(), "first", "instructions-a", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Transformation[string]) (Transformed[string], error) {
				require.Equal(t, "base", input.Original)
				require.Equal(t, "base", input.Current)
				return Transformed[string]{Kind: Replace, Replacement: input.Current + " A"}, nil
			}),
		runtime.EXPECT().HandleSystemInstructions(gomock.Any(), "second", "instructions-b", gomock.Any()).DoAndReturn(
			func(_ context.Context, _, _ string, input Transformation[string]) (Transformed[string], error) {
				require.Equal(t, "base", input.Original)
				require.Equal(t, "base A", input.Current)
				return Transformed[string]{Kind: Replace, Replacement: input.Original + " B"}, nil
			}),
		runtime.EXPECT().HandleModelContext(gomock.Any(), "first", "history-a", gomock.Any()).DoAndReturn(
			func(
				_ context.Context, _, _ string, input Transformation[[]agent.HistoryEntry],
			) (Transformed[[]agent.HistoryEntry], error) {
				require.Equal(t, []agent.HistoryEntry{userEntry("question")}, input.Current)
				input.Original[0].User.MustGet().Content[0].Text = mo.Some("mutated")
				return Transformed[[]agent.HistoryEntry]{
					Kind: Replace, Replacement: append(input.Current, userEntry("A")),
				}, nil
			}),
		runtime.EXPECT().HandleModelContext(gomock.Any(), "second", "history-keep", gomock.Any()).Return(
			Transformed[[]agent.HistoryEntry]{Kind: Preserve, Replacement: nil}, nil,
		),
		runtime.EXPECT().HandleModelContext(gomock.Any(), "second", "history-b", gomock.Any()).DoAndReturn(
			func(
				_ context.Context, _, _ string, input Transformation[[]agent.HistoryEntry],
			) (Transformed[[]agent.HistoryEntry], error) {
				require.Equal(t, []agent.HistoryEntry{userEntry("question")}, input.Original)
				require.Equal(t, []agent.HistoryEntry{userEntry("question"), userEntry("A")}, input.Current)
				return Transformed[[]agent.HistoryEntry]{
					Kind: Replace, Replacement: append(input.Original, userEntry("B")),
				}, nil
			}),
	)
	request := modelexecution.RequestContext{Instructions: "base", History: []agent.HistoryEntry{userEntry("question")}}

	// Act through the captured composer consumed by model execution.
	final, err := composer(t.Context(), request)

	// Assert the final values, the immutable caller request, and that a later registration was not captured.
	require.NoError(t, err)
	require.Equal(t, "base B", final.Context.Instructions)
	require.Equal(t, []agent.HistoryEntry{userEntry("question"), userEntry("B")}, final.Context.History)
	require.True(t, final.HistoryReplaced)
	require.Equal(t, []agent.HistoryEntry{userEntry("question")}, request.History)
}

// TestContextCompositionStopsChain stops at a handler error or invalid action and invokes no later handler.
func TestContextCompositionStopsChain(t *testing.T) {
	t.Parallel()
	source := errors.New("context source: unexpected EOF")
	for name, outcome := range map[string]struct {
		action Transformed[string]
		err    error
	}{
		"handler_error":  {action: Transformed[string]{Kind: 0, Replacement: ""}, err: source},
		"invalid_action": {action: Transformed[string]{Kind: Acknowledge, Replacement: ""}, err: nil},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a failing instruction handler followed by handlers that must not run.
			ctrl := gomock.NewController(t)
			runtime := NewMockRuntime(ctrl)
			contexts := NewMockContextIssuer(ctrl)
			service := New(runtime, contexts)
			service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{{
				ID: "ext", Path: "", Tools: nil,
				Handlers: []startup.AcceptedHandler{
					{ID: "failing", Kind: startup.RawHandlerKindSystemInstructions},
					{ID: "later", Kind: startup.RawHandlerKindSystemInstructions},
					{ID: "history", Kind: startup.RawHandlerKindModelContext},
				},
			}})
			contexts.EXPECT().IssueContext("ext").Return(extension.Context{}, nil)
			runtime.EXPECT().
				HandleSystemInstructions(gomock.Any(), "ext", "failing", gomock.Any()).
				Return(outcome.action, outcome.err)
			request := modelexecution.RequestContext{Instructions: "base", History: nil}

			// Act on one prepared-context revision.
			_, err := service.NewContextComposer()(t.Context(), request)

			// Assert handler identity and complete cause; strict mocks reject later invocations.
			require.ErrorContains(t, err, `"failing"`)
			if outcome.err != nil {
				require.ErrorIs(t, err, source)
				require.ErrorContains(t, err, source.Error())
			}
		})
	}
}

// TestContextCompositionRejectsInvalidHistory fails a replacement whose entry payload does not match its kind.
func TestContextCompositionRejectsInvalidHistory(t *testing.T) {
	t.Parallel()
	// Arrange a history handler that returns a user entry without its payload.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts)
	service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{{
		ID: "ext", Path: "", Tools: nil,
		Handlers: []startup.AcceptedHandler{
			{ID: "invalid", Kind: startup.RawHandlerKindModelContext},
			{ID: "later", Kind: startup.RawHandlerKindModelContext},
		},
	}})
	contexts.EXPECT().IssueContext("ext").Return(extension.Context{}, nil)
	invalid := agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	}
	runtime.EXPECT().HandleModelContext(gomock.Any(), "ext", "invalid", gomock.Any()).Return(
		Transformed[[]agent.HistoryEntry]{Kind: Replace, Replacement: []agent.HistoryEntry{invalid}}, nil,
	)

	// Act on one prepared-context revision.
	_, err := service.NewContextComposer()(t.Context(), modelexecution.RequestContext{Instructions: "", History: nil})

	// Assert the validation cause and handler identity; strict mocks reject the later handler.
	require.ErrorContains(t, err, `"invalid"`)
	require.ErrorContains(t, err, "user history payload is missing")
}

// userEntry builds one text-only user history entry.
func userEntry(text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(text)), Model: mo.None[model.Response](),
		ToolResult: mo.None[agent.ToolResult](),
	}
}

// TestContextCompositionAcceptsIntermediateImages leaves model-input checks to the final history.
func TestContextCompositionAcceptsIntermediateImages(t *testing.T) {
	t.Parallel()
	// Arrange handler A adding an image and handler B restoring the original text history.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts)
	service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{{
		ID: "ext", Path: "", Tools: nil,
		Handlers: []startup.AcceptedHandler{
			{ID: "add-image", Kind: startup.RawHandlerKindModelContext},
			{ID: "remove-image", Kind: startup.RawHandlerKindModelContext},
		},
	}})
	contexts.EXPECT().IssueContext("ext").Return(extension.Context{}, nil).Times(2)
	image := agent.HistoryEntry{
		Kind: agent.HistoryEntryUser,
		User: mo.Some(model.Message{Content: []model.InputContent{{
			Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.Some("image/png"),
			Data: mo.Some([]byte{1}),
		}}}),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
	gomock.InOrder(
		runtime.EXPECT().HandleModelContext(gomock.Any(), "ext", "add-image", gomock.Any()).Return(
			Transformed[[]agent.HistoryEntry]{Kind: Replace, Replacement: []agent.HistoryEntry{image}}, nil,
		),
		runtime.EXPECT().HandleModelContext(gomock.Any(), "ext", "remove-image", gomock.Any()).DoAndReturn(
			func(
				_ context.Context, _, _ string, input Transformation[[]agent.HistoryEntry],
			) (Transformed[[]agent.HistoryEntry], error) {
				return Transformed[[]agent.HistoryEntry]{Kind: Replace, Replacement: input.Original}, nil
			}),
	)
	request := modelexecution.RequestContext{Instructions: "", History: []agent.HistoryEntry{userEntry("text")}}

	// Act on one prepared-context revision.
	final, err := service.NewContextComposer()(t.Context(), request)

	// Assert the final text history and its replacement state.
	require.NoError(t, err)
	require.Equal(t, []agent.HistoryEntry{userEntry("text")}, final.Context.History)
	require.True(t, final.HistoryReplaced)
}

// TestContextCompositionReportsPreservedHistory reports no replacement when every history handler preserves.
func TestContextCompositionReportsPreservedHistory(t *testing.T) {
	t.Parallel()
	// Arrange one preserving history handler.
	ctrl := gomock.NewController(t)
	runtime := NewMockRuntime(ctrl)
	contexts := NewMockContextIssuer(ctrl)
	service := New(runtime, contexts)
	service.CommitMiddlewareHandlers([]startup.AcceptedRegistration{{
		ID: "ext", Path: "", Tools: nil,
		Handlers: []startup.AcceptedHandler{{ID: "keep", Kind: startup.RawHandlerKindModelContext}},
	}})
	contexts.EXPECT().IssueContext("ext").Return(extension.Context{}, nil)
	runtime.EXPECT().HandleModelContext(gomock.Any(), "ext", "keep", gomock.Any()).Return(
		Transformed[[]agent.HistoryEntry]{Kind: Preserve, Replacement: nil}, nil,
	)
	request := modelexecution.RequestContext{Instructions: "", History: []agent.HistoryEntry{userEntry("text")}}

	// Act on one prepared-context revision.
	final, err := service.NewContextComposer()(t.Context(), request)

	// Assert the original history and no replacement.
	require.NoError(t, err)
	require.Equal(t, request.History, final.Context.History)
	require.False(t, final.HistoryReplaced)
}
