//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

// newFinalizedConfiguredService creates a configured-request service whose provider completes once and whose
// finalizer is the returned mock.
func newFinalizedConfiguredService(t *testing.T) (*Service, *MockResponseFinalizer, *contextFixture) {
	t.Helper()
	fixture := newContextFixture(t)
	fixture.catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), fixture.selection).Return(CatalogBinding{
		Model: modelDescriptor(fixture.selection), ReasoningChoice: fixture.selection.ReasoningChoice,
		Provider: fixture.provider,
	}, nil)
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ ProviderRequest, handle StreamHandler) error {
			return emitContextPreparationSuccess(handle)
		},
	)
	controller := gomock.NewController(t)
	finalizer := NewMockResponseFinalizer(controller)
	service := New(fixture.catalog, disabledRetryPolicy(), nil, nil, NewMockConversationPolicies(controller))
	require.NoError(t, service.BindResponseFinalizer(finalizer))
	return service, finalizer, fixture
}

// bindPassThroughFinalizer binds a finalizer that returns each configured response unchanged.
func bindPassThroughFinalizer(t *testing.T, service *Service) {
	t.Helper()
	finalizer := NewMockResponseFinalizer(gomock.NewController(t))
	finalizer.EXPECT().FinalizeConfiguredResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, response model.Response) (model.Response, error) { return response, nil },
	).AnyTimes()
	require.NoError(t, service.BindResponseFinalizer(finalizer))
}

// TestRequestConfiguredReturnsFinalizedResponse verifies that the caller receives the final value of the configured
// terminal response.
func TestRequestConfiguredReturnsFinalizedResponse(t *testing.T) {
	t.Parallel()
	// Arrange a finalizer that replaces the terminal response content.
	service, finalizer, fixture := newFinalizedConfiguredService(t)
	finalizer.EXPECT().FinalizeConfiguredResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, response model.Response) (model.Response, error) {
			require.Equal(t, mo.Some(model.OutcomeStop), response.Outcome)
			response.Content = []model.Content{{
				Kind: model.ContentText, Text: mo.Some("final"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			}}
			return response, nil
		},
	)

	// Act through the configured-model caller contract.
	response, err := service.RequestConfigured(
		t.Context(), fixture.selection, "base", []agent.HistoryEntry{textHistoryEntry("question")}, nil,
	)

	// Assert the caller receives the final response.
	require.NoError(t, err)
	require.Equal(t, "final", response.Text())
}

// TestRequestConfiguredFinalizationFailure verifies that a failed configured finalization is an extension failure.
func TestRequestConfiguredFinalizationFailure(t *testing.T) {
	t.Parallel()
	// Arrange a finalizer that fails once.
	service, finalizer, fixture := newFinalizedConfiguredService(t)
	source := errors.New(`middleware handler "finalized": extension source text`)
	finalizer.EXPECT().FinalizeConfiguredResponse(gomock.Any(), gomock.Any()).Return(model.Response{}, source)

	// Act through the configured-model caller contract.
	_, err := service.RequestConfigured(
		t.Context(), fixture.selection, "base", []agent.HistoryEntry{textHistoryEntry("question")}, nil,
	)

	// Assert the extension-failure category and the complete cause.
	require.ErrorIs(t, err, source)
	failure, classified := errors.AsType[*LogicalFailureError](err)
	require.True(t, classified)
	require.Equal(t, FailureExtensionFailed, failure.Category)
	require.ErrorContains(t, err, "finalize configured model response: "+source.Error())
}

// TestRequestConfiguredRequiresFinalizer verifies that a configured request without a bound finalizer fails before
// provider work.
func TestRequestConfiguredRequiresFinalizer(t *testing.T) {
	t.Parallel()
	// Arrange an unbound service whose catalog and provider must not be used.
	fixture := newContextFixture(t)
	service := New(
		fixture.catalog,
		disabledRetryPolicy(),
		nil,
		nil,
		NewMockConversationPolicies(gomock.NewController(t)),
	)

	// Act through the configured-model caller contract.
	_, err := service.RequestConfigured(
		t.Context(), fixture.selection, "base", []agent.HistoryEntry{textHistoryEntry("question")}, nil,
	)

	// Assert the configured request failed before binding resolution and dispatch.
	require.EqualError(t, err, "configured model request requires a bound response finalizer")
}

// TestRequestConfiguredFinalizationCancellation verifies that owner cancellation reported by a configured
// finalization keeps its uncategorized meaning.
func TestRequestConfiguredFinalizationCancellation(t *testing.T) {
	t.Parallel()
	// Arrange a finalizer that observes the cancellation of the owning context.
	service, finalizer, fixture := newFinalizedConfiguredService(t)
	ctx, cancel := context.WithCancel(t.Context())
	finalizer.EXPECT().FinalizeConfiguredResponse(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ model.Response) (model.Response, error) {
			cancel()
			return model.Response{}, fmt.Errorf(`middleware handler "finalized": %w`, ctx.Err())
		},
	)

	// Act through the configured-model caller contract.
	_, err := service.RequestConfigured(
		ctx, fixture.selection, "base", []agent.HistoryEntry{textHistoryEntry("question")}, nil,
	)

	// Assert the cancellation without the extension-failure category.
	require.ErrorIs(t, err, context.Canceled)
	_, classified := errors.AsType[*LogicalFailureError](err)
	require.False(t, classified)
}
