//go:build !integration

package extensionmodels

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// TestConfiguredRequestPassesExactInput verifies model ownership forwards one explicit request unchanged.
func TestConfiguredRequestPassesExactInput(t *testing.T) {
	t.Parallel()

	// Arrange a valid binding, an empty instruction string, and ordered text history.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	requester := NewMockModelRequester(controller)
	contexts := NewMockContextValidator(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil).Times(2)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceHigh}
	history := []agent.HistoryEntry{{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("question")),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}}
	expected := model.Response{
		Content: []model.Content{{
			Kind: model.ContentText, Text: mo.Some("answer"), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	requester.EXPECT().Request(gomock.Any(), selection, "", history).Return(expected, nil)
	service := New(catalog, requester, contexts)

	// Act through the extension-facing model owner.
	actual, err := service.Request(t.Context(), "extension", "runtime", reference, selection, "", history)

	// Assert the provider response and request values are unchanged.
	require.NoError(t, err)
	assert.Equal(t, expected, actual)
}

// TestConfiguredRequestRejectsStaleCompletion verifies a replaced binding cannot publish a provider result.
func TestConfiguredRequestRejectsStaleCompletion(t *testing.T) {
	t.Parallel()

	// Arrange a valid binding and block provider execution before invalidating it.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	requester := NewMockModelRequester(controller)
	contexts := NewMockContextValidator(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	staleErr := errors.New("binding was replaced")
	var stale atomic.Bool
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).DoAndReturn(
		func(string, string, extensiondomain.ContextRef) error {
			if stale.Load() {
				return staleErr
			}
			return nil
		},
	).AnyTimes()
	entered := make(chan struct{})
	release := make(chan struct{})
	requester.EXPECT().Request(gomock.Any(), gomock.Any(), "instructions", gomock.Any()).DoAndReturn(
		func(context.Context, model.Selection, string, []agent.HistoryEntry) (model.Response, error) {
			close(entered)
			<-release
			return model.Response{}, nil
		},
	)
	service := New(catalog, requester, contexts)
	result := make(chan error, 1)

	// Act by replacing the binding while the provider request runs.
	go func() {
		_, err := service.Request(
			t.Context(),
			"extension",
			"runtime",
			reference,
			model.Selection{},
			"instructions",
			nil,
		)
		result <- err
	}()
	<-entered
	stale.Store(true)
	close(release)

	// Assert the stale operation cannot return a usable result.
	require.ErrorIs(t, <-result, staleErr)
}

// TestConfiguredRequestClassifiesProviderFailures verifies every provider-owned failure keeps its complete cause.
func TestConfiguredRequestClassifiesProviderFailures(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		// name identifies the provider failure.
		name string
		// selectionCode contains a provider catalogue category when present.
		selectionCode string
		// expected contains the public configured-request category.
		expected string
	}{
		{name: "missing model", selectionCode: selectionCodeNotFound, expected: modelUnavailableCode},
		{name: "unsupported reasoning", selectionCode: selectionCodeReasoningUnsupported, expected: modelUnavailableCode},
		{name: "credentials", selectionCode: selectionCodeCredentialUnavailable, expected: credentialUnavailableCode},
		{name: "provider execution", selectionCode: "", expected: modelFailedCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a valid binding and one provider failure with a complete diagnostic cause.
			controller := gomock.NewController(t)
			catalog := NewMockCatalog(controller)
			requester := NewMockModelRequester(controller)
			contexts := NewMockContextValidator(controller)
			reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
			contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
			requestErr := errors.New("complete provider cause")
			if test.selectionCode != "" {
				classified := NewMockRequestFailure(controller)
				classified.EXPECT().SelectionCode().Return(test.selectionCode)
				classified.EXPECT().Error().Return(requestErr.Error()).AnyTimes()
				requestErr = classified
			}
			requester.EXPECT().Request(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(model.Response{}, requestErr)
			service := New(catalog, requester, contexts)

			// Act through model-owned failure classification.
			_, err := service.Request(t.Context(), "extension", "runtime", reference, model.Selection{}, "", nil)

			// Assert category and complete provider cause remain available together.
			var failure extensioncontroller.ModelFailure
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, test.expected, failure.ModelCode())
			assert.Contains(t, err.Error(), "complete provider cause")
		})
	}
}
