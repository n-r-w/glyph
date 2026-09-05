//go:build !integration

package providers

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestRequestExecutesExactSelectionWithoutMutation verifies one alternate configured request preserves
// active state.
func TestRequestExecutesExactSelectionWithoutMutation(t *testing.T) {
	t.Parallel()

	// Arrange two configured selections and one terminal response from the alternate provider.
	controller := gomock.NewController(t)
	activeProvider := agentrun.NewMockModelProvider(controller)
	alternateProvider := agentrun.NewMockModelProvider(controller)
	validator := NewMockCredentialChecker(controller)
	validator.EXPECT().CheckCredentials(gomock.Any()).Return(nil)
	active := model.Selection{Provider: "active", Model: "main", ReasoningChoice: model.ReasoningChoiceLow}
	alternate := model.Selection{Provider: "alternate", Model: "summary", ReasoningChoice: model.ReasoningChoiceHigh}
	catalog, err := New([]Entry{
		{
			Descriptor:        descriptor("active", "main", model.ReasoningChoiceLow),
			Provider:          activeProvider,
			CredentialChecker: nil,
			Authentication:    nil,
		},
		{
			Descriptor:        descriptor("alternate", "summary", model.ReasoningChoiceHigh),
			Provider:          alternateProvider,
			CredentialChecker: validator,
			Authentication:    nil,
		},
	}, active)
	require.NoError(t, err)
	history := []agent.HistoryEntry{
		{
			Kind:       agent.HistoryEntryUser,
			User:       mo.Some(model.TextMessage("question")),
			Model:      mo.None[model.Response](),
			ToolResult: mo.None[agent.ToolResult](),
		},
		{
			Kind: agent.HistoryEntryModel,
			User: mo.None[model.Message](),
			Model: mo.Some(model.Response{
				Content: []model.Content{{
					Kind: model.ContentText, Text: mo.Some("answer"), Final: true,
					ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
				}},
				Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
				Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
				Usage: mo.None[model.Usage](), Diagnostics: nil,
			}),
			ToolResult: mo.None[agent.ToolResult](),
		},
	}
	terminal := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentText, Text: mo.Some("summary"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
				ProviderContext: mo.Some(model.ProviderContext{
					Source: model.ProviderContextSource{
						ProviderID: "alternate", API: "private-api", Model: "summary",
						CompatibilityKey: mo.None[string](),
					},
					Payload: []byte("private-context"),
				}), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(model.ToolCall{
					ID: "call", Name: "ignored", Arguments: map[string]any{"value": "exact"},
				}),
			},
		},
		Outcome:       mo.Some(model.OutcomeToolUse),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.Some(model.ProviderID("alternate")),
		Model:         mo.Some(model.ID("summary")),
		ResponseModel: mo.Some(model.ID("reported-summary")),
		ResponseID:    mo.Some("response"),
		Usage: mo.Some(model.Usage{
			InputTokens: 3, OutputTokens: 5, CachedInputTokens: 2,
			CacheWriteTokens: 1, ReasoningTokens: 2, TotalTokens: 8,
		}),
		Diagnostics: []model.Diagnostic{{Code: "notice", Message: "complete diagnostic"}},
	}
	alternateProvider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request agentrun.ModelRequest, handle agentrun.StreamHandler) error {
			assert.Equal(t, "instructions", request.Instructions)
			actualSelection := model.Selection{
				Provider: request.Model.Provider, Model: request.Model.Model,
				ReasoningChoice: request.ReasoningChoice,
			}
			assert.Equal(t, alternate, actualSelection)
			assert.Equal(t, history, request.History)
			assert.Empty(t, request.Tools)
			return handle(
				agentrun.StreamEvent{
					Kind:     agentrun.StreamEventDone,
					Position: mo.None[int](),
					Content:  mo.None[model.Content](),
					Delta:    mo.None[string](),
					Preview:  mo.None[model.ToolCallPreview](),
					ToolCall: mo.None[model.ToolCall](),
					Response: mo.Some(terminal),
				},
			)
		},
	)

	// Act with the alternate explicit selection.
	response, err := catalog.Request(t.Context(), alternate, "instructions", history)

	// Assert exact execution result and unchanged active selection.
	require.NoError(t, err)
	assert.Equal(t, terminal, response)
	assert.Equal(t, active, catalog.ActiveSelection())
}

// TestRequestChecksCredentialsWithoutSelectionMutation verifies provider-owned credentials are checked
// before execution.
func TestRequestChecksCredentialsWithoutSelectionMutation(t *testing.T) {
	t.Parallel()

	// Arrange one configured entry whose provider-owned authentication rejects credentials.
	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	authentication := NewMockProviderAuthentication(controller)
	authentication.EXPECT().CheckCredentials(gomock.Any()).Return(context.Canceled)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog, err := New([]Entry{{
		Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
		CredentialChecker: nil, Authentication: authentication,
	}}, selection)
	require.NoError(t, err)

	// Act by executing a request with the selected entry.
	_, err = catalog.Request(t.Context(), selection, "instructions", nil)

	// Assert credential classification and unchanged active selection without a provider stream call.
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeCredentialUnavailable, selectionErr.Code)
	assert.Equal(t, selection, catalog.ActiveSelection())
}

// TestRequestFailureAndCancellationPreserveSelection verifies terminal provider errors never mutate active state.
func TestRequestFailureAndCancellationPreserveSelection(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		// name identifies the terminal provider path.
		name string
		// streamErr is the complete provider result.
		streamErr error
	}{
		{name: "failure", streamErr: errors.New("complete provider stream failure")},
		{name: "cancellation", streamErr: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange active and alternate models with a failing alternate provider.
			controller := gomock.NewController(t)
			activeProvider := agentrun.NewMockModelProvider(controller)
			alternateProvider := agentrun.NewMockModelProvider(controller)
			active := model.Selection{Provider: "active", Model: "main", ReasoningChoice: model.ReasoningChoiceOff}
			alternate := model.Selection{
				Provider:        "alternate",
				Model:           "other",
				ReasoningChoice: model.ReasoningChoiceOff,
			}
			catalog, err := New([]Entry{
				{
					Descriptor: descriptor("active", "main", model.ReasoningChoiceOff), Provider: activeProvider,
					CredentialChecker: nil, Authentication: nil,
				},
				{
					Descriptor: descriptor("alternate", "other", model.ReasoningChoiceOff), Provider: alternateProvider,
					CredentialChecker: nil, Authentication: nil,
				},
			}, active)
			require.NoError(t, err)
			alternateProvider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).Return(test.streamErr)

			// Act through one explicit alternate request.
			_, err = catalog.Request(t.Context(), alternate, "instructions", nil)

			// Assert the complete provider cause and active conversation selection remain unchanged.
			require.ErrorIs(t, err, test.streamErr)
			assert.Contains(t, err.Error(), test.streamErr.Error())
			assert.Equal(t, active, catalog.ActiveSelection())
		})
	}
}

// TestRequestRejectsUnavailableSelectionWithoutMutation verifies exact provider, model, and reasoning
// validation.
func TestRequestRejectsUnavailableSelectionWithoutMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		selection model.Selection
		code      ErrorCode
	}{
		{
			name:      "model",
			selection: model.Selection{Provider: "missing", Model: "model", ReasoningChoice: model.ReasoningChoiceOff},
			code:      ErrorCodeNotFound,
		},
		{
			name: "reasoning",
			selection: model.Selection{
				Provider:        "provider",
				Model:           "model",
				ReasoningChoice: model.ReasoningChoiceHigh,
			},
			code: ErrorCodeReasoningUnsupported,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one configured model and strict unused runtime dependencies.
			controller := gomock.NewController(t)
			provider := agentrun.NewMockModelProvider(controller)
			validator := NewMockCredentialChecker(controller)
			authentication := NewMockProviderAuthentication(controller)
			active := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
			catalog, err := New([]Entry{{
				Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
				CredentialChecker: validator, Authentication: authentication,
			}}, active)
			require.NoError(t, err)

			// Act with an unavailable explicit selection.
			_, err = catalog.Request(t.Context(), test.selection, "instructions", nil)

			// Assert rejection occurs before credentials or execution and active state remains unchanged.
			var selectionErr *SelectionError
			require.ErrorAs(t, err, &selectionErr)
			assert.Equal(t, test.code, selectionErr.Code)
			assert.Equal(t, active, catalog.ActiveSelection())
		})
	}
}
