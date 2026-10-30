//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// unsupportedImageText is the validation text for the first block of a text-only fixture model.
const unsupportedImageText = "model model does not accept image input at message 0 content 0"

// imageHistoryEntry builds one user history entry with an image block.
func imageHistoryEntry() agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryUser,
		User: mo.Some(model.Message{Content: []model.InputContent{{
			Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.Some("image/png"),
			Data: mo.Some([]byte{1}),
		}}}),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
}

// TestFinalContextRejectsImagesWithoutHandlers validates prepared history when no context handler is bound.
func TestFinalContextRejectsImagesWithoutHandlers(t *testing.T) {
	t.Parallel()
	// Arrange a text-only active model and image history without context middleware.
	fixture := newContextFixture(t)
	expectActive(fixture)
	service := New(fixture.catalog, disabledRetryPolicy(), nil, nil, newTestPreservingPolicies(t))

	// Act through the Agent Core port; strict mocks reject any provider attempt.
	err := streamConversation(t.Context(), service, []agent.HistoryEntry{imageHistoryEntry()})

	// Assert the plain validation error names the model, message, and content.
	require.ErrorContains(t, err, unsupportedImageText)
	_, categorized := errors.AsType[*LogicalFailureError](err)
	require.False(t, categorized)
}

// fixedComposer returns one final history with the supplied replacement state.
func fixedComposer(history []agent.HistoryEntry, replaced bool) ContextComposer {
	return func(_ context.Context, request RequestContext) (ComposedContext, error) {
		return ComposedContext{
			Context:         RequestContext{Instructions: request.Instructions, History: history},
			HistoryReplaced: replaced,
		}, nil
	}
}

// TestFinalContextValidationCategory distinguishes unsupported prepared history from unsupported handler output.
func TestFinalContextValidationCategory(t *testing.T) {
	t.Parallel()
	for name, replaced := range map[string]bool{"preserved": false, "replaced": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a text-only active model and final image history.
			fixture := newContextFixture(t)
			expectActive(fixture)
			image := []agent.HistoryEntry{imageHistoryEntry()}
			fixture.middleware.EXPECT().NewContextComposer().Return(fixedComposer(image, replaced))
			service := newContextService(t, fixture, disabledRetryPolicy(), newTestPreservingPolicies(t), nil)

			// Act through the Agent Core port; strict mocks reject any provider attempt.
			err := streamConversation(t.Context(), service, image)

			// Assert the complete text and the category selected by the replacement state.
			require.ErrorContains(t, err, unsupportedImageText)
			failure, categorized := errors.AsType[*LogicalFailureError](err)
			require.Equal(t, replaced, categorized)
			if replaced {
				require.Equal(t, FailureExtensionFailed, failure.Category)
			}
		})
	}
}

// TestConfiguredFinalContextValidation validates the composed configured input instead of the caller input.
func TestConfiguredFinalContextValidation(t *testing.T) {
	t.Parallel()
	for name, scenario := range map[string]struct {
		input    []agent.HistoryEntry
		final    []agent.HistoryEntry
		replaced bool
		failure  mo.Option[FailureCategory]
	}{
		"handler removes image": {
			input: []agent.HistoryEntry{imageHistoryEntry()}, final: []agent.HistoryEntry{textHistoryEntry("text")},
			replaced: true, failure: mo.None[FailureCategory](),
		},
		"preserved image": {
			input: []agent.HistoryEntry{imageHistoryEntry()}, final: []agent.HistoryEntry{imageHistoryEntry()},
			replaced: false, failure: mo.Some[FailureCategory](""),
		},
		"handler adds image": {
			input: []agent.HistoryEntry{textHistoryEntry("text")}, final: []agent.HistoryEntry{imageHistoryEntry()},
			replaced: true, failure: mo.Some(FailureExtensionFailed),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a text-only configured model and one composition result.
			fixture := newContextFixture(t)
			fixture.catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), fixture.selection).Return(CatalogBinding{
				Model: modelDescriptor(fixture.selection), ReasoningChoice: fixture.selection.ReasoningChoice,
				Provider: fixture.provider,
			}, nil)
			fixture.middleware.EXPECT().NewContextComposer().Return(fixedComposer(scenario.final, scenario.replaced))
			if scenario.failure.IsNone() {
				fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
						return emitContextPreparationSuccess(handle)
					},
				)
			}
			service := newContextService(
				t, fixture, disabledRetryPolicy(), NewMockConversationPolicies(gomock.NewController(t)), nil,
			)

			// Act through the configured-model caller contract.
			_, err := service.RequestConfigured(t.Context(), fixture.selection, "base", scenario.input, nil)

			// Assert success or the category selected by the replacement state.
			category, fails := scenario.failure.Get()
			if !fails {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, unsupportedImageText)
			failure, categorized := errors.AsType[*LogicalFailureError](err)
			require.Equal(t, category != "", categorized)
			if categorized {
				require.Equal(t, category, failure.Category)
			}
		})
	}
}

// TestOverflowRevisionValidatesFinalContext rejects a recomposed revision before another provider attempt.
func TestOverflowRevisionValidatesFinalContext(t *testing.T) {
	t.Parallel()
	// Arrange an overflow whose recovered revision is composed into unsupported image history.
	fixture := newContextFixture(t)
	expectActive(fixture)
	output := NewMockRetryOutput(gomock.NewController(t))
	captured := NewMockCapturedConversation(gomock.NewController(t))
	captured.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(
		ConversationPreparation{History: []agent.HistoryEntry{textHistoryEntry("prepared")}, Committed: false}, nil,
	)
	captured.EXPECT().RecoverOverflow(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		ConversationPreparation{History: []agent.HistoryEntry{textHistoryEntry("recovered")}, Committed: true}, nil,
	)
	compositions := 0
	fixture.middleware.EXPECT().NewContextComposer().Return(
		func(_ context.Context, request RequestContext) (ComposedContext, error) {
			compositions++
			history := request.History
			if compositions == 2 {
				history = []agent.HistoryEntry{imageHistoryEntry()}
			}
			return ComposedContext{
				Context: RequestContext{Instructions: request.Instructions, History: history}, HistoryReplaced: true,
			}, nil
		},
	)
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(&ProviderFailureError{
		Classification: ProviderFailureContextOverflow, RetryDelay: mo.None[time.Duration](),
		Cause: errors.New("context length exceeded"),
	})
	service := newContextService(t, fixture, enabledRetryPolicy(), newTestCapturedPolicies(t, captured), output)

	// Act through the Agent Core port; strict mocks reject a second provider attempt.
	err := streamConversation(t.Context(), service, nil)

	// Assert the recomposed revision failed its final check as an extension failure.
	require.ErrorContains(t, err, unsupportedImageText)
	require.ErrorContains(t, err, "context length exceeded")
	failure, categorized := errors.AsType[*LogicalFailureError](err)
	require.True(t, categorized)
	require.Equal(t, FailureExtensionFailed, failure.Category)
}
