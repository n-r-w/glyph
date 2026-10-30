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

// TestAbsentPolicyDispatchesOrdinaryHistory checks ordinary dispatch without an admitted policy owner.
func TestAbsentPolicyDispatchesOrdinaryHistory(t *testing.T) {
	t.Parallel()
	// Arrange ordinary dispatch without a captured policy owner.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)

	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	descriptor := modelDescriptor(selection)
	history := []agent.HistoryEntry{textHistoryEntry("ordinary history")}
	catalog.EXPECT().
		ActiveBinding().
		Return(CatalogBinding{Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider})
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, input ProviderRequest, handle StreamHandler) error {
			require.Equal(t, history, input.History)
			return emitContextPreparationSuccess(handle)
		},
	)
	execution := New(catalog, disabledRetryPolicy(), nil, nil, newTestConversationObservations(t))
	// Act through the logical conversation entry point.
	err := streamLogical(t.Context(), execution,
		agentrun.ContextRequest{
			Instructions: "instructions",
			History:      history,
			Tools:        nil,
		},
		func(_ agentrun.StreamEvent) error { return nil },
	)
	// Assert ordinary dispatch remains available without policy authority.
	require.NoError(t, err)
}

// TestAbsentPolicySuppressesCompactionOverflow checks terminal overflow without a captured policy owner.
func TestAbsentPolicySuppressesCompactionOverflow(t *testing.T) {
	t.Parallel()
	// Arrange a provider overflow without a captured policy owner.
	controller := gomock.NewController(t)
	catalog := NewMockCatalogResolver(controller)
	provider := NewMockProviderAttempt(controller)

	output := NewMockRetryOutput(controller)
	selection := model.Selection{Provider: "provider", Model: "model", ReasoningChoice: model.ReasoningChoiceLow}
	descriptor := modelDescriptor(selection)
	catalog.EXPECT().
		ActiveBinding().
		Return(CatalogBinding{Model: descriptor, ReasoningChoice: selection.ReasoningChoice, Provider: provider})
	cause := errors.New("provider rejected oversized ordinary input: complete source")
	calls := 0
	provider.EXPECT().
		Stream(gomock.Any(), gomock.Any(), gomock.Any()).
		AnyTimes().
		DoAndReturn(func(_ context.Context, _ ProviderRequest, _ StreamHandler) error {
			calls++
			return &ProviderFailureError{
				Classification: ProviderFailureContextOverflow,
				RetryDelay:     mo.None[time.Duration](),
				Cause:          cause,
			}
		})
	output.EXPECT().DeliverRetry(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	execution := New(
		catalog,
		RetryPolicy{Enabled: true, MaxRetries: 1, Delays: []time.Duration{0}, MaxProviderDelay: 0},
		nil,
		output,
		newTestConversationObservations(t),
	)
	// Act through the ordinary logical conversation path.
	err := streamLogical(t.Context(), execution,
		agentrun.ContextRequest{
			Instructions: "instructions",
			History:      []agent.HistoryEntry{textHistoryEntry("ordinary oversized input")},
			Tools:        nil,
		},
		func(_ agentrun.StreamEvent) error { return nil },
	)
	// Assert the original overflow remains terminal without compaction or another provider attempt.
	require.ErrorIs(t, err, cause)
	failure, present := errors.AsType[*LogicalFailureError](err)
	require.True(t, present)
	require.Equal(t, FailureContextLimit, failure.Category)
	require.Equal(t, 1, calls)
}
