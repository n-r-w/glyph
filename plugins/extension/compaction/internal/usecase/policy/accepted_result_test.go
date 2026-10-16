//go:build !integration

package policy

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestCompactPreservedResultUsesAcceptedProjection checks accepted generation and final cancellation.
func TestCompactPreservedResultUsesAcceptedProjection(t *testing.T) {
	t.Parallel()
	for _, canceled := range []bool{false, true} {
		name := "commit"
		if canceled {
			name = "canceled-after-preserve"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a generated candidate followed by a preserving result handler.
			input := newPolicyInput()
			participant := newPolicyParticipant("preserve")
			input.Generators = []compaction.Participant{participant}
			input.Results = []compaction.Participant{participant}
			candidate := newPolicyResult("accepted summary")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			host := NewMockHost(gomock.NewController(t))
			gomock.InOrder(
				host.EXPECT().Generate(gomock.Any(), participant, gomock.Any()).Return(candidate, nil),
				host.EXPECT().Project(gomock.Any(), candidate).Return(input.History[1:], nil),
				host.EXPECT().InvokeResultHandler(gomock.Any(), participant, gomock.Any(), candidate).
					Do(func(context.Context, compaction.Participant, compaction.State, compaction.Result) {
						if canceled {
							cancel()
						}
					}).
					Return(compaction.ResultAction{
						Cancel: false, Disposition: compaction.ResultPreserve, Result: mo.None[compaction.Result](),
					}, nil),
			)
			if !canceled {
				host.EXPECT().Commit(gomock.Any(), candidate).Return(nil)
			}
			// Act through the complete manual policy.
			outcome, err := New(host, 1).Compact(ctx, input)
			// Assert preservation retains the accepted candidate and cancellation prevents commit.
			if canceled {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.NoError(t, err)
			}
			require.True(t, outcome.State.IsSome())
			require.Equal(t, candidate, outcome.State.MustGet().Result.MustGet())
		})
	}
}
