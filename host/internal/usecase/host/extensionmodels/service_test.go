//go:build !integration

package extensionmodels

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// categorizedRequesterFailure is a test logical failure with complete nested causes.
type categorizedRequesterFailure struct {
	// code is the stable logical category.
	code string
	// cause contains all contributing failures.
	cause error
}

// Error returns complete contributing failure text.
func (failure *categorizedRequesterFailure) Error() string { return failure.cause.Error() }

// Unwrap exposes every contributing failure.
func (failure *categorizedRequesterFailure) Unwrap() error { return failure.cause }

// FailureCode returns the stable logical category.
func (failure *categorizedRequesterFailure) FailureCode() string { return failure.code }

// TestConfiguredRequestPreservesLogicalCategoryAndCauses verifies typed model-execution failures pass unchanged.
func TestConfiguredRequestPreservesLogicalCategoryAndCauses(t *testing.T) {
	t.Parallel()

	// Arrange one typed logical failure with two contributing causes.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	requester := NewMockModelRequester(controller)
	contexts := NewMockContextValidator(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	firstCause := errors.New("first retry attempt failed")
	lastCause := errors.New("last retry attempt failed")
	requester.EXPECT().RequestConfigured(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(model.Response{}, &categorizedRequesterFailure{
		code: "RETRY_EXHAUSTED", cause: errors.Join(firstCause, lastCause),
	})
	service := New(catalog, requester, contexts)

	// Act through the configured-model boundary.
	_, err := service.Request(t.Context(), "extension", "runtime", reference, model.Selection{}, "", nil, nil)

	// Assert category and all causes remain available through the public failure.
	var failure extensioncontroller.ModelFailure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, "RETRY_EXHAUSTED", failure.ModelCode())
	require.ErrorIs(t, err, firstCause)
	require.ErrorIs(t, err, lastCause)
}

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
	requester.EXPECT().RequestConfigured(gomock.Any(), selection, "", history, gomock.Any()).Return(expected, nil)
	service := New(catalog, requester, contexts)

	// Act through the extension-facing model owner.
	actual, err := service.Request(t.Context(), "extension", "runtime", reference, selection, "", history, nil)

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
	requester.EXPECT().RequestConfigured(
		gomock.Any(), gomock.Any(), "instructions", gomock.Any(), gomock.Any(),
	).DoAndReturn(
		func(context.Context, model.Selection, string, []agent.HistoryEntry, func(int64, int64, time.Duration, string) error) (model.Response, error) {
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
			nil, nil,
		)
		result <- err
	}()
	<-entered
	stale.Store(true)
	close(release)

	// Assert the stale operation cannot return a usable result.
	require.ErrorIs(t, <-result, staleErr)
}

// TestConfiguredRequestClassifiesActiveProviderTimeout verifies an untyped timeout is an internal failure.
func TestConfiguredRequestClassifiesActiveProviderTimeout(t *testing.T) {
	t.Parallel()

	// Arrange an active caller context and a provider-attempt deadline failure.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	requester := NewMockModelRequester(controller)
	contexts := NewMockContextValidator(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	requester.EXPECT().RequestConfigured(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).Return(model.Response{}, context.DeadlineExceeded)
	service := New(catalog, requester, contexts)

	// Act through the configured-model boundary.
	_, err := service.Request(t.Context(), "extension", "runtime", reference, model.Selection{}, "", nil, nil)

	// Assert the untyped active failure keeps an internal category rather than becoming cancellation.
	var failure extensioncontroller.ModelFailure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, internalCode, failure.ModelCode())
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

// TestConfiguredRequestMixedCancellationPreservesIndependentFailure verifies caller cancellation cannot hide a cause.
func TestConfiguredRequestMixedCancellationPreservesIndependentFailure(t *testing.T) {
	t.Parallel()

	// Arrange provider work that acquires an independent failure as caller cancellation arrives.
	controller := gomock.NewController(t)
	catalog := NewMockCatalog(controller)
	requester := NewMockModelRequester(controller)
	contexts := NewMockContextValidator(controller)
	reference := extensiondomain.ContextRef{ID: "context", RuntimeInstanceID: "runtime", SessionID: "session"}
	contexts.EXPECT().ValidateContext("extension", "runtime", reference).Return(nil)
	ctx, cancel := context.WithCancel(t.Context())
	independent := errors.New("independent configured provider failure")
	requester.EXPECT().RequestConfigured(
		gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
	).DoAndReturn(func(context.Context, model.Selection, string, []agent.HistoryEntry, func(int64, int64, time.Duration, string) error) (
		model.Response, error,
	) {
		cancel()
		return model.Response{}, errors.Join(context.Canceled, independent)
	})
	service := New(catalog, requester, contexts)

	// Act through the configured-model boundary.
	_, err := service.Request(ctx, "extension", "runtime", reference, model.Selection{}, "", nil, nil)

	// Assert both causes survive under a failure category rather than pure operation cancellation.
	var failure extensioncontroller.ModelFailure
	require.ErrorAs(t, err, &failure)
	assert.Equal(t, internalCode, failure.ModelCode())
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, independent)
}

// TestConfiguredRequestClassifiesRequesterFailures verifies typed selection and untyped local failures.
func TestConfiguredRequestClassifiesRequesterFailures(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		// name identifies the requester failure.
		name string
		// selectionCode contains a provider catalogue category when present.
		selectionCode string
		// expected contains the public configured-request category.
		expected string
	}{
		{name: "missing model", selectionCode: selectionCodeNotFound, expected: modelUnavailableCode},
		{name: "unsupported reasoning", selectionCode: selectionCodeReasoningUnsupported, expected: modelUnavailableCode},
		{name: "credentials", selectionCode: selectionCodeCredentialUnavailable, expected: credentialUnavailableCode},
		{name: "uncategorized requester failure", selectionCode: "", expected: internalCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange a valid binding and one requester failure with a complete diagnostic cause.
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
			requester.EXPECT().RequestConfigured(
				gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(),
			).Return(model.Response{}, requestErr)
			service := New(catalog, requester, contexts)

			// Act through model-owned failure classification.
			_, err := service.Request(t.Context(), "extension", "runtime", reference, model.Selection{}, "", nil, nil)

			// Assert category and complete requester cause remain available together.
			var failure extensioncontroller.ModelFailure
			require.ErrorAs(t, err, &failure)
			assert.Equal(t, test.expected, failure.ModelCode())
			assert.Contains(t, err.Error(), "complete provider cause")
		})
	}
}
