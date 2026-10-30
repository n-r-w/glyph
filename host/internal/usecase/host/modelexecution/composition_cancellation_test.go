//go:build !integration

package modelexecution

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
)

// TestCompositionCancellationKeepsAcquiredFailure separates pure cancellation from a concurrent handler failure.
func TestCompositionCancellationKeepsAcquiredFailure(t *testing.T) {
	t.Parallel()
	handlerFailure := errors.New(`middleware handler "context": extension source text`)
	for name, acquired := range map[string]bool{"pure cancellation": false, "acquired failure": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a canceled owning context and a composer that returns cancellation or a handler failure.
			fixture := newContextFixture(t)
			expectActive(fixture)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			fixture.middleware.EXPECT().NewContextComposer().Return(
				func(ctx context.Context, _ RequestContext) (ComposedContext, error) {
					if acquired {
						return ComposedContext{}, handlerFailure
					}
					return ComposedContext{}, ctx.Err()
				},
			)
			service := newContextService(t, fixture, disabledRetryPolicy(), newTestPreservingPolicies(t), nil)

			// Act through the Agent Core port; strict mocks reject any provider attempt.
			err := streamConversation(ctx, service, []agent.HistoryEntry{textHistoryEntry("question")})

			// Assert cancellation meaning, or the extension category with cancellation as a contributing cause.
			require.ErrorIs(t, err, context.Canceled)
			failure, categorized := errors.AsType[*LogicalFailureError](err)
			require.Equal(t, acquired, categorized)
			if acquired {
				require.Equal(t, FailureExtensionFailed, failure.Category)
				require.ErrorIs(t, err, handlerFailure)
				require.ErrorContains(t, err, handlerFailure.Error())
			}
		})
	}
}
