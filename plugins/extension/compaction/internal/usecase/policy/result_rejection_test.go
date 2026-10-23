//go:build !integration

package policy

import (
	"context"
	"errors"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestCompactStopsBeforeInstallingRejectedResult preserves accepted state and forbids later participation and Commit.
func TestCompactStopsBeforeInstallingRejectedResult(t *testing.T) {
	t.Parallel()
	participantErr := errors.New("result participant source failure")
	projectionErr := errors.New("compaction boundary is outside its branch")
	rejected := newPolicyResult("rejected")
	for _, scenario := range []struct {
		name           string
		action         compaction.ResultAction
		participantErr error
		projectionErr  error
		cause          string
		canceled       bool
	}{
		{
			name: "mixed-cancel", action: compaction.ResultAction{
				Cancel: true, Disposition: compaction.ResultPreserve, Result: mo.None[compaction.Result](),
			},
			participantErr: nil, projectionErr: nil, cause: "cancellation must be exclusive", canceled: false,
		},
		{
			name: "missing-payload", action: compaction.ResultAction{
				Cancel: false, Disposition: compaction.ResultReplace, Result: mo.None[compaction.Result](),
			},
			participantErr: nil, projectionErr: nil,
			cause: "compaction result replacement payload does not match disposition", canceled: false,
		},
		{
			name: "unexpected-payload", action: compaction.ResultAction{
				Cancel: false, Disposition: compaction.ResultPreserve, Result: mo.Some(rejected),
			},
			participantErr: nil, projectionErr: nil,
			cause: "compaction result replacement payload does not match disposition", canceled: false,
		},
		{
			name: "boundary", action: compaction.ResultAction{
				Cancel: false, Disposition: compaction.ResultReplace, Result: mo.Some(rejected),
			},
			participantErr: nil, projectionErr: projectionErr, cause: projectionErr.Error(), canceled: false,
		},
		{
			name: "participant-failure", action: compaction.ResultAction{}, participantErr: participantErr,
			projectionErr: nil, cause: participantErr.Error(), canceled: false,
		},
		{
			name: "cancellation-containing-error", action: compaction.ResultAction{},
			participantErr: errors.Join(context.Canceled, participantErr),
			projectionErr:  nil, cause: participantErr.Error(), canceled: false,
		},
		{
			name: "explicit-cancel", action: compaction.ResultAction{
				Cancel: true, Disposition: compaction.ResultUnspecified, Result: mo.None[compaction.Result](),
			},
			participantErr: nil, projectionErr: nil, cause: "", canceled: true,
		},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			// Arrange an accepted request and result replacement followed by a terminal participant and a later handler.
			input := newPolicyInput()
			requestHandler := newPolicyParticipant("request")
			first := newPolicyParticipant("first")
			second := newPolicyParticipant("second")
			later := newPolicyParticipant("later")
			input.Requests = []compaction.Participant{requestHandler}
			input.Results = []compaction.Participant{first, second, later}
			original, err := compaction.SelectRequest(
				input,
				1,
				compaction.EstimateInput(input.History, input.Instructions, input.ToolText),
			)
			require.NoError(t, err)
			changed := original
			changed.Guidance = "accepted guidance"
			supplied, updated := newPolicyResult("supplied"), newPolicyResult("accepted")
			initial := compaction.State{Original: original, Current: original, Result: mo.None[compaction.Result]()}
			acceptedRequest := compaction.State{Original: original, Current: changed, Result: mo.Some(supplied)}
			acceptedResult := compaction.State{Original: original, Current: changed, Result: mo.Some(updated)}
			host := NewMockHost(gomock.NewController(t))
			gomock.InOrder(
				host.EXPECT().Request(gomock.Any(), requestHandler, initial).Return(compaction.RequestAction{
					Cancel: false, RequestDisposition: compaction.RequestReplace, Request: mo.Some(changed),
					ResultDisposition: compaction.ResultReplace, Result: mo.Some(supplied),
				}, nil),
				host.EXPECT().Project(gomock.Any(), supplied).Return(input.History[1:], nil),
				host.EXPECT().
					InvokeResultHandler(gomock.Any(), first, acceptedRequest, supplied).
					Return(compaction.ResultAction{
						Cancel: false, Disposition: compaction.ResultReplace, Result: mo.Some(updated),
					}, nil),
				host.EXPECT().Project(gomock.Any(), updated).Return(input.History[1:], nil),
				host.EXPECT().
					InvokeResultHandler(gomock.Any(), second, acceptedResult, supplied).
					Return(scenario.action, scenario.participantErr),
			)
			if scenario.projectionErr != nil {
				host.EXPECT().Project(gomock.Any(), rejected).Return(nil, scenario.projectionErr)
			}
			// Act through the bundled policy entry point without canceling the owning context.
			outcome, err := New(host, 1).Compact(t.Context(), input)
			// Assert complete accepted state and causes; unexpected later-handler and Commit calls fail the mock.
			require.NoError(t, t.Context().Err())
			require.Equal(t, scenario.canceled, outcome.Canceled)
			require.False(t, outcome.Preserve)
			require.True(t, outcome.State.IsSome())
			require.Equal(t, acceptedResult, outcome.State.MustGet())
			if scenario.canceled {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, scenario.cause)
			}
			if scenario.participantErr != nil {
				require.ErrorIs(t, err, scenario.participantErr)
			}
			if errors.Is(scenario.participantErr, context.Canceled) {
				require.ErrorIs(t, err, context.Canceled)
				require.ErrorContains(t, err, context.Canceled.Error())
			}
			if scenario.projectionErr != nil {
				require.ErrorIs(t, err, scenario.projectionErr)
			}
		})
	}
}
