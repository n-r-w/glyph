package providers

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// TestCompleteConfiguredExecutesExactSelectionWithoutMutation verifies one alternate configured request preserves active state.
func TestCompleteConfiguredExecutesExactSelectionWithoutMutation(t *testing.T) {
	t.Parallel()

	// Arrange two configured selections and one terminal response from the alternate provider.
	controller := gomock.NewController(t)
	activeProvider := agentrun.NewMockModelProvider(controller)
	alternateProvider := agentrun.NewMockModelProvider(controller)
	validator := NewMockSelectionCredentialValidator(controller)
	validator.EXPECT().ValidateSelectionCredentials(gomock.Any()).Return(nil)
	active := model.Selection{Provider: "active", Model: "main", ReasoningChoice: model.ReasoningChoiceLow}
	alternate := model.Selection{Provider: "alternate", Model: "summary", ReasoningChoice: model.ReasoningChoiceHigh}
	catalog, err := New([]Entry{
		{Descriptor: descriptor("active", "main", model.ReasoningChoiceLow), Provider: activeProvider, SelectionCredentialValidator: nil, Authentication: nil},
		{Descriptor: descriptor("alternate", "summary", model.ReasoningChoiceHigh), Provider: alternateProvider, SelectionCredentialValidator: validator, Authentication: nil},
	}, active)
	require.NoError(t, err)
	history := []agent.HistoryEntry{{Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("abandoned")), Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult]()}}
	terminal := model.Response{
		Content: []model.Content{{Kind: model.ContentText, Text: mo.Some("summary"), Final: true, ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall]()}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.Some(model.ProviderID("alternate")), Model: mo.Some(model.ID("summary")),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
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
			return handle(agentrun.StreamEvent{Kind: agentrun.StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](), Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](), ToolCall: mo.None[model.ToolCall](), Response: mo.Some(terminal)})
		},
	)

	// Act with the alternate configured selection.
	response, err := catalog.CompleteConfigured(t.Context(), alternate, "instructions", history)

	// Assert exact execution result and unchanged active selection.
	require.NoError(t, err)
	assert.Equal(t, terminal, response)
	assert.Equal(t, active, catalog.Selection())
}

// TestCompleteConfiguredValidatesAuthenticationWithoutSelectionMutation verifies provider-owned credentials are checked before execution.
func TestCompleteConfiguredValidatesAuthenticationWithoutSelectionMutation(t *testing.T) {
	t.Parallel()

	// Arrange one configured entry whose provider-owned authentication rejects credentials.
	controller := gomock.NewController(t)
	provider := agentrun.NewMockModelProvider(controller)
	authentication := NewMockProviderAuthentication(controller)
	authentication.EXPECT().CheckProviderAuthentication(gomock.Any()).Return(context.Canceled)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
	catalog, err := New([]Entry{{
		Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
		SelectionCredentialValidator: nil, Authentication: authentication,
	}}, selection)
	require.NoError(t, err)

	// Act by executing the configured entry.
	_, err = catalog.CompleteConfigured(t.Context(), selection, "instructions", nil)

	// Assert credential classification and unchanged active selection without a provider stream call.
	var selectionErr *SelectionError
	require.ErrorAs(t, err, &selectionErr)
	assert.Equal(t, ErrorCodeCredentialUnavailable, selectionErr.Code)
	assert.Equal(t, selection, catalog.Selection())
}

// TestCompleteConfiguredRejectsUnavailableSelectionWithoutMutation verifies exact provider, model, and reasoning validation.
func TestCompleteConfiguredRejectsUnavailableSelectionWithoutMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		selection model.Selection
		code      ErrorCode
	}{
		{name: "model", selection: model.Selection{Provider: "missing", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}, code: ErrorCodeNotFound},
		{name: "reasoning", selection: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}, code: ErrorCodeReasoningUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one configured model and strict unused runtime dependencies.
			controller := gomock.NewController(t)
			provider := agentrun.NewMockModelProvider(controller)
			validator := NewMockSelectionCredentialValidator(controller)
			authentication := NewMockProviderAuthentication(controller)
			active := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceOff}
			catalog, err := New([]Entry{{
				Descriptor: descriptor("provider", "model", model.ReasoningChoiceOff), Provider: provider,
				SelectionCredentialValidator: validator, Authentication: authentication,
			}}, active)
			require.NoError(t, err)

			// Act with an unavailable exact selection.
			_, err = catalog.CompleteConfigured(t.Context(), test.selection, "instructions", nil)

			// Assert rejection occurs before credentials or execution and active state remains unchanged.
			var selectionErr *SelectionError
			require.ErrorAs(t, err, &selectionErr)
			assert.Equal(t, test.code, selectionErr.Code)
			assert.Equal(t, active, catalog.Selection())
		})
	}
}
