//go:build !integration

package contextcompaction

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
)

// TestRequestProducerFailureRetainsPrecedingState checks retained orchestration ownership.
func TestRequestProducerFailureRetainsPrecedingState(t *testing.T) {
	t.Parallel()
	// Arrange successful composition followed by an invalid producer.
	// Act through retained orchestration.
	// Assert preceding failure-observer state in the suite scenario.
	suite.Run(t, new(requestProducerFailureSuite))
}

// requestProducerFailureSuite checks failure-observer composition state.
type requestProducerFailureSuite struct {
	// Suite owns assertions for retained request composition.
	suite.Suite
}

// TestScenario checks detached successful values and preceding state after producer rejection.
func (s *requestProducerFailureSuite) TestScenario() {
	t := s.T()
	// Arrange a successful result supplier followed by an invalid producer.
	service, sessions, runtime, contexts, request, _ := newFailurePathHarness(t)
	first := Handler{ExtensionID: "first", RuntimeID: "runtime", HandlerID: "supply"}
	invalid := Handler{ExtensionID: "invalid", RuntimeID: "runtime", HandlerID: "replace"}
	observer := Handler{ExtensionID: "observer", RuntimeID: "runtime", HandlerID: "failure"}
	runtime.EXPECT().SnapshotCompactionHandlers().Return(HandlerSet{
		Requests: []Handler{first, invalid}, Generators: nil, Results: nil,
		Successes: nil, Failures: []Handler{observer},
	})
	for _, handler := range []Handler{first, invalid, observer} {
		expectCompactionContext(contexts, handler.ExtensionID)
	}
	ready := failurePathResult("first")
	ready.Details = mo.Some([]byte("preceding details"))
	var original, preceding Request
	sessions.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(
		[]agent.HistoryEntry{textHistory("fits")}, nil,
	)
	gomock.InOrder(
		runtime.EXPECT().HandleCompactionRequest(gomock.Any(), first, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ Handler, _ extension.Context, input RequestInvocation) (RequestAction, error) {
				original, preceding = cloneRequest(input.Original), cloneRequest(input.Current)
				return RequestAction{
					Cancel: false, RequestAction: RequestActionPreserve, Request: mo.None[Request](),
					ResultAction: ResultActionReplace, Result: mo.Some(ready),
				}, nil
			},
		),
		runtime.EXPECT().HandleCompactionRequest(gomock.Any(), invalid, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ Handler, _ extension.Context, input RequestInvocation) (RequestAction, error) {
				ready.Details.MustGet()[0] = 'X'
				require.Equal(t, []byte("preceding details"), input.CurrentResult.MustGet().Details.MustGet())
				require.Equal(t, original, input.Original)
				require.Equal(t, preceding, input.Current)
				input.Current.Suffix[0].EstimatedTokens = 999
				return RequestAction{
					Cancel: false, RequestAction: RequestActionReplace, Request: mo.Some(input.Current),
					ResultAction: ResultActionReplace, Result: mo.Some(ready),
				}, nil
			},
		),
		runtime.EXPECT().ObserveCompactionFailure(gomock.Any(), observer, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ Handler, _ extension.Context, input OutcomeInvocation) error {
				state := input.State.MustGet()
				require.Equal(t, original, state.OriginalRequest)
				require.Equal(t, preceding, state.CurrentRequest)
				require.Equal(t, []byte("preceding details"), state.Result.MustGet().Details.MustGet())
				return nil
			},
		),
	)

	// Act through retained automatic orchestration.
	result, err := service.Compact(t.Context(), TriggerThreshold, mo.None[string](), false, request)

	// Assert producer rejection prevents replacement and retains the preceding observer state.
	require.ErrorContains(t, err, `compaction extension source "first" does not match handler owner "invalid"`)
	require.True(t, result.Committed.IsNone())
}
