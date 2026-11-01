//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// categorizedResultEntry returns a history entry with a tool result of the given error flag and category.
func categorizedResultEntry(text string, isError bool, category mo.Option[agent.ToolErrorCategory]) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: "call", ToolName: "read", Contents: tool.TextContents(text), IsError: isError,
			ErrorCategory: category,
		}),
	}
}

// annotatedResultEntry returns the expected request form of a categorized error result.
func annotatedResultEntry(text string, category agent.ToolErrorCategory) agent.HistoryEntry {
	entry := categorizedResultEntry(text, true, mo.Some(category))
	result := entry.ToolResult.OrEmpty()
	result.Contents = append(
		tool.TextContents(fmt.Sprintf(toolErrorCategoryFormat, category.Name())),
		result.Contents...)
	entry.ToolResult = mo.Some(result)
	return entry
}

// TestAnnotateErrorCategories verifies that only categorized error results receive the category as a leading text
// block and that the input history stays unchanged.
func TestAnnotateErrorCategories(t *testing.T) {
	t.Parallel()

	// Arrange history with a user message, a plain error result, a successful result, and categorized error results.
	categories := []agent.ToolErrorCategory{
		agent.ToolErrorUnavailableTool, agent.ToolErrorInvalidArguments, agent.ToolErrorFailure,
	}
	history := []agent.HistoryEntry{
		textHistoryEntry("question"),
		categorizedResultEntry("skipped", true, mo.None[agent.ToolErrorCategory]()),
		categorizedResultEntry("done", false, mo.None[agent.ToolErrorCategory]()),
	}
	expected := agent.CloneHistory(history)
	for _, category := range categories {
		history = append(history, categorizedResultEntry("failed", true, mo.Some(category)))
		expected = append(expected, annotatedResultEntry("failed", category))
	}
	original := agent.CloneHistory(history)

	// Act through the annotation.
	annotated := annotateErrorCategories(history)

	// Assert the annotated copy and the untouched input.
	assert.Equal(t, expected, annotated)
	assert.Equal(t, original, history)
}

// TestPrepareContextAnnotatesCategoryBeforeHandlers verifies that the model request names the category before the
// context handlers run, in every revision, while the captured conversation and Agent Core keep the stored contents.
func TestPrepareContextAnnotatesCategoryBeforeHandlers(t *testing.T) {
	t.Parallel()

	// Arrange a conversation whose prepared and recovered histories end with an invalid-arguments result.
	fixture := newContextFixture(t)
	expectActive(fixture)
	expectComposer(fixture)
	output := NewMockRetryOutput(gomock.NewController(t))
	output.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil)
	stored := categorizedResultEntry("invalid", true, mo.Some(agent.ToolErrorInvalidArguments))
	prepared := []agent.HistoryEntry{textHistoryEntry("prepared"), stored}
	recovered := []agent.HistoryEntry{textHistoryEntry("recovered"), stored}
	captured := NewMockCapturedConversation(gomock.NewController(t))
	captured.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(
		ConversationPreparation{History: agent.CloneHistory(prepared), Committed: false}, nil,
	)
	captured.EXPECT().RecoverOverflow(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		ConversationPreparation{History: agent.CloneHistory(recovered), Committed: true}, nil,
	)
	captured.EXPECT().Observe(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input ProviderRequest, _ model.Response) error {
			assert.Equal(t, recovered, input.History)
			return nil
		},
	)
	attempts := 0
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
			attempts++
			if attempts == 1 {
				return &ProviderFailureError{
					Classification: ProviderFailureContextOverflow, RetryDelay: mo.None[time.Duration](),
					Cause: errors.New("context length exceeded"),
				}
			}
			assert.Equal(t, []agent.HistoryEntry{
				textHistoryEntry("recovered"), annotatedResultEntry("invalid", agent.ToolErrorInvalidArguments),
				textHistoryEntry("B"),
			}, request.History)
			return emitContextPreparationSuccess(handle)
		},
	).Times(2)
	service := newContextService(t, fixture, enabledRetryPolicy(), newTestCapturedPolicies(t, captured), output)
	coreHistory := []agent.HistoryEntry{textHistoryEntry("original"), stored}

	// Act through the Agent Core port.
	err := streamConversation(t.Context(), service, coreHistory)

	// Assert the handlers received the annotated originals of both revisions and the inputs stayed unchanged.
	require.NoError(t, err)
	require.Len(t, fixture.composed, 2)
	assert.Equal(t, []agent.HistoryEntry{
		textHistoryEntry("prepared"), annotatedResultEntry("invalid", agent.ToolErrorInvalidArguments),
	}, fixture.composed[0].History)
	assert.Equal(t, []agent.HistoryEntry{
		textHistoryEntry("recovered"), annotatedResultEntry("invalid", agent.ToolErrorInvalidArguments),
	}, fixture.composed[1].History)
	assert.Equal(t, []agent.HistoryEntry{textHistoryEntry("original"), stored}, coreHistory)
}
