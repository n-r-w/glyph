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
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// contextFixture contains mocked dependencies for effective-context execution tests.
type contextFixture struct {
	// selection identifies the active model.
	selection model.Selection
	// catalog resolves the active and configured bindings.
	catalog *MockCatalogResolver
	// provider records raw provider attempts.
	provider *MockProviderAttempt
	// middleware supplies the captured composer.
	middleware *MockContextMiddleware
	// composed records the original value of each composition.
	composed []RequestContext
}

// newContextFixture creates mocked dependencies for one effective-context test.
func newContextFixture(t *testing.T) *contextFixture {
	t.Helper()
	controller := gomock.NewController(t)
	fixture := &contextFixture{
		selection:  model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow},
		catalog:    NewMockCatalogResolver(controller),
		provider:   NewMockProviderAttempt(controller),
		middleware: NewMockContextMiddleware(controller),
		composed:   nil,
	}
	return fixture
}

// expectComposer captures a composer that records each original value and appends marker B to it.
func expectComposer(fixture *contextFixture) {
	fixture.middleware.EXPECT().NewContextComposer().Return(
		func(_ context.Context, request RequestContext) (ComposedContext, error) {
			fixture.composed = append(fixture.composed, request)
			history := append(agent.CloneHistory(request.History), textHistoryEntry("B"))
			return ComposedContext{
				Context:         RequestContext{Instructions: request.Instructions + " B", History: history},
				HistoryReplaced: true,
			}, nil
		},
	)
}

// expectActive resolves the active model to the fixture provider.
func expectActive(fixture *contextFixture) {
	fixture.catalog.EXPECT().ActiveBinding().Return(CatalogBinding{
		Model: modelDescriptor(fixture.selection), ReasoningChoice: fixture.selection.ReasoningChoice,
		Provider: fixture.provider,
	})
}

// newContextService creates a bound service with the supplied retry policy and conversation owner.
func newContextService(
	t *testing.T,
	fixture *contextFixture,
	policy RetryPolicy,
	policies ConversationPolicies,
	output RetryOutput,
) *Service {
	t.Helper()
	service := New(fixture.catalog, policy, nil, output, policies)
	require.NoError(t, service.BindContextMiddleware(fixture.middleware))
	return service
}

// streamConversation executes one Agent Core request with base instructions through the effective-context port.
func streamConversation(ctx context.Context, service *Service, history []agent.HistoryEntry) error {
	return streamLogical(ctx, service, agentrun.ContextRequest{Instructions: "base", History: history, Tools: nil},
		func(agentrun.StreamEvent) error { return nil })
}

// enabledRetryPolicy allows one retry without delay.
func enabledRetryPolicy() RetryPolicy {
	return RetryPolicy{Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: time.Second}
}

// TestPrepareContextComposesAfterPreparation transforms prepared history and observes the untransformed request.
func TestPrepareContextComposesAfterPreparation(t *testing.T) {
	t.Parallel()
	// Arrange preparation that replaces Core history and an owner that observes the completed attempt.
	fixture := newContextFixture(t)
	expectActive(fixture)
	expectComposer(fixture)
	captured := NewMockCapturedConversation(gomock.NewController(t))
	prepared := []agent.HistoryEntry{textHistoryEntry("compacted")}
	captured.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(
		ConversationPreparation{History: prepared, Committed: true}, nil,
	)
	captured.EXPECT().Observe(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input ProviderRequest, _ model.Response) error {
			require.Equal(t, "base", input.Instructions)
			require.Equal(t, prepared, input.History)
			return nil
		},
	)
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
			require.Equal(t, "base B", request.Instructions)
			require.Equal(
				t,
				[]agent.HistoryEntry{textHistoryEntry("compacted"), textHistoryEntry("B")},
				request.History,
			)
			return emitContextPreparationSuccess(handle)
		},
	)
	service := newContextService(t, fixture, disabledRetryPolicy(), newTestCapturedPolicies(t, captured), nil)

	// Act through the Agent Core port.
	err := streamConversation(t.Context(), service, []agent.HistoryEntry{textHistoryEntry("original")})

	// Assert composition used the prepared history as its original value.
	require.NoError(t, err)
	require.Equal(t, []RequestContext{{Instructions: "base", History: prepared}}, fixture.composed)
}

// TestPreparedRetryReusesEffectiveContext sends one composed revision to every transient retry.
func TestPreparedRetryReusesEffectiveContext(t *testing.T) {
	t.Parallel()
	// Arrange one transient provider failure followed by success.
	fixture := newContextFixture(t)
	expectActive(fixture)
	expectComposer(fixture)
	output := NewMockRetryOutput(gomock.NewController(t))
	output.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil)
	attempts := 0
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
			attempts++
			require.Equal(t, "base B", request.Instructions)
			if attempts == 1 {
				return &ProviderFailureError{
					Classification: ProviderFailureTransient, RetryDelay: mo.None[time.Duration](),
					Cause: errors.New("rate limited"),
				}
			}
			return emitContextPreparationSuccess(handle)
		},
	).Times(2)
	service := newContextService(t, fixture, enabledRetryPolicy(), newTestPreservingPolicies(t), output)

	// Act through the Agent Core port.
	err := streamConversation(t.Context(), service, []agent.HistoryEntry{textHistoryEntry("original")})

	// Assert both raw attempts used the single composition.
	require.NoError(t, err)
	require.Len(t, fixture.composed, 1)
}

// TestOverflowRecoveryComposesRecoveredContext composes committed recovery with the same captured handlers.
func TestOverflowRecoveryComposesRecoveredContext(t *testing.T) {
	t.Parallel()
	// Arrange one provider overflow and a policy owner that commits replacement history.
	fixture := newContextFixture(t)
	expectActive(fixture)
	expectComposer(fixture)
	output := NewMockRetryOutput(gomock.NewController(t))
	output.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil)
	captured := NewMockCapturedConversation(gomock.NewController(t))
	prepared := []agent.HistoryEntry{textHistoryEntry("prepared")}
	recovered := []agent.HistoryEntry{textHistoryEntry("recovered")}
	captured.EXPECT().Prepare(gomock.Any(), gomock.Any()).Return(
		ConversationPreparation{History: prepared, Committed: false}, nil,
	)
	captured.EXPECT().RecoverOverflow(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input ProviderRequest, _ error) (ConversationPreparation, error) {
			require.Equal(t, "base", input.Instructions)
			require.Equal(t, prepared, input.History)
			return ConversationPreparation{History: recovered, Committed: true}, nil
		},
	)
	captured.EXPECT().Observe(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input ProviderRequest, _ model.Response) error {
			require.Equal(t, "base", input.Instructions)
			require.Equal(t, recovered, input.History)
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
			require.Equal(t, "base B", request.Instructions)
			require.Equal(
				t,
				[]agent.HistoryEntry{textHistoryEntry("recovered"), textHistoryEntry("B")},
				request.History,
			)
			return emitContextPreparationSuccess(handle)
		},
	).Times(2)
	service := newContextService(t, fixture, enabledRetryPolicy(), newTestCapturedPolicies(t, captured), output)

	// Act through the Agent Core port.
	err := streamConversation(t.Context(), service, []agent.HistoryEntry{textHistoryEntry("original")})

	// Assert the recovered revision was composed from untransformed values with the captured composer.
	require.NoError(t, err)
	require.Equal(t, []RequestContext{
		{Instructions: "base", History: prepared},
		{Instructions: "base", History: recovered},
	}, fixture.composed)
}

// TestConfiguredRequestComposesContext applies the shared composition to explicit configured input.
func TestConfiguredRequestComposesContext(t *testing.T) {
	t.Parallel()
	// Arrange a configured binding and a policy dependency that must not capture a conversation owner.
	fixture := newContextFixture(t)
	expectComposer(fixture)
	fixture.catalog.EXPECT().ResolveConfiguredBinding(gomock.Any(), fixture.selection).Return(CatalogBinding{
		Model: modelDescriptor(fixture.selection), ReasoningChoice: fixture.selection.ReasoningChoice,
		Provider: fixture.provider,
	}, nil)
	fixture.provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request ProviderRequest, handle StreamHandler) error {
			require.Equal(t, "base B", request.Instructions)
			require.Equal(t, []agent.HistoryEntry{textHistoryEntry("question"), textHistoryEntry("B")}, request.History)
			return emitContextPreparationSuccess(handle)
		},
	)
	service := newContextService(
		t,
		fixture,
		disabledRetryPolicy(),
		NewMockConversationPolicies(gomock.NewController(t)),
		nil,
	)

	// Act through the configured-model caller contract.
	bindPassThroughFinalizer(t, service)
	_, err := service.RequestConfigured(
		t.Context(), fixture.selection, "base", []agent.HistoryEntry{textHistoryEntry("question")}, nil,
	)

	// Assert the explicit input was the composition original.
	require.NoError(t, err)
	require.Equal(t, []RequestContext{{
		Instructions: "base", History: []agent.HistoryEntry{textHistoryEntry("question")},
	}}, fixture.composed)
}
