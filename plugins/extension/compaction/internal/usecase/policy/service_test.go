//go:build !integration

package policy

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestCompactComposesOrderedState validates ordered updates, originals, and generator bypass.
func TestCompactComposesOrderedState(t *testing.T) {
	t.Parallel()
	// Arrange two request handlers and two result handlers with a supplied result.
	input := newPolicyInput()
	first, second := newPolicyParticipant("first"), newPolicyParticipant("second")
	input.Requests = []compaction.Participant{first, second}
	input.Results = []compaction.Participant{first, second}
	input.Generators = []compaction.Participant{first, second}
	host := NewMockHost(gomock.NewController(t))
	service := New(host, 1)
	original, err := compaction.SelectRequest(
		input, 1, compaction.EstimateInput(input.History, input.Instructions, input.ToolText),
	)
	require.NoError(t, err)
	changed := original
	changed.Guidance = "composed guidance"
	supplied := newPolicyResult("supplied")
	updated := newPolicyResult("updated")
	initial := compaction.State{Original: original, Current: original, Result: mo.None[compaction.Result]()}
	accepted := compaction.State{Original: original, Current: changed, Result: mo.Some(supplied)}
	final := compaction.State{Original: original, Current: changed, Result: mo.Some(updated)}
	gomock.InOrder(
		host.EXPECT().
			Request(gomock.Any(), first, initial).
			Return(compaction.RequestAction{
				Cancel: false, RequestDisposition: compaction.RequestReplace, Request: mo.Some(changed),
				ResultDisposition: compaction.ResultReplace, Result: mo.Some(supplied),
			}, nil),
		host.EXPECT().Project(gomock.Any(), supplied).Return(input.History[1:], nil),
		host.EXPECT().
			Request(gomock.Any(), second, accepted).
			Return(compaction.RequestAction{
				Cancel: false, RequestDisposition: compaction.RequestPreserve, Request: mo.None[compaction.Request](),
				ResultDisposition: compaction.ResultPreserve, Result: mo.None[compaction.Result](),
			}, nil),
		host.EXPECT().
			InvokeResultHandler(gomock.Any(), first, accepted, supplied).
			Return(compaction.ResultAction{Cancel: false, Disposition: compaction.ResultReplace, Result: mo.Some(updated)}, nil),
		host.EXPECT().Project(gomock.Any(), updated).Return(input.History[1:], nil),
		host.EXPECT().
			InvokeResultHandler(gomock.Any(), second, final, supplied).
			Return(compaction.ResultAction{
				Cancel: false, Disposition: compaction.ResultPreserve, Result: mo.None[compaction.Result](),
			}, nil),
		host.EXPECT().Commit(gomock.Any(), updated).Return(nil),
	)
	// Act through the policy boundary.
	outcome, err := service.Compact(t.Context(), input)
	// Assert the last accepted state and immutable original values.
	require.NoError(t, err)
	require.False(t, outcome.Canceled)
	require.True(t, outcome.State.IsSome())
	require.Equal(t, final, outcome.State.MustGet())
}

// TestCompactClearsResult checks the requirement for one generator after a request handler clears a supplied result.
func TestCompactClearsResult(t *testing.T) {
	t.Parallel()
	// Arrange a supplied result followed by a clear action.
	for _, cardinality := range []int{0, 1, 2} {
		t.Run(string(rune('0'+cardinality)), func(t *testing.T) {
			t.Parallel()
			input := newPolicyInput()
			participant := newPolicyParticipant("handler")
			input.Requests = []compaction.Participant{participant, participant}
			for range cardinality {
				input.Generators = append(input.Generators, participant)
			}
			host := NewMockHost(gomock.NewController(t))
			supplied := newPolicyResult("supplied")
			gomock.InOrder(
				host.EXPECT().
					Request(gomock.Any(), participant, gomock.Any()).
					Return(compaction.RequestAction{
						Cancel:             false,
						RequestDisposition: compaction.RequestPreserve,
						Request:            mo.None[compaction.Request](),
						ResultDisposition:  compaction.ResultReplace,
						Result:             mo.Some(supplied),
					}, nil),
				host.EXPECT().Project(gomock.Any(), supplied).Return(input.History[1:], nil),
				host.EXPECT().
					Request(gomock.Any(), participant, gomock.Any()).
					Return(compaction.RequestAction{
						Cancel:             false,
						RequestDisposition: compaction.RequestPreserve,
						Request:            mo.None[compaction.Request](),
						ResultDisposition:  compaction.ResultClear,
						Result:             mo.None[compaction.Result](),
					}, nil),
			)
			if cardinality == 1 {
				gomock.InOrder(
					host.EXPECT().Generate(gomock.Any(), participant, gomock.Any()).Return(supplied, nil),
					host.EXPECT().Project(gomock.Any(), supplied).Return(input.History[1:], nil),
					host.EXPECT().Commit(gomock.Any(), supplied).Return(nil),
				)
			}
			// Act after both request actions.
			_, err := New(host, 1).Compact(t.Context(), input)
			// Assert cardinality is checked only after clearing.
			if cardinality == 1 {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, "exactly one generator")
			}
		})
	}
}

// TestCompactStopsBeforeInstallingRejectedState checks preservation of accepted state on failure or cancellation.
func TestCompactStopsBeforeInstallingRejectedState(t *testing.T) {
	t.Parallel()
	// Arrange invalid, canceled, failed, and structurally rejected request updates.
	cases := []struct {
		name     string
		action   compaction.RequestAction
		failure  error
		project  bool
		canceled bool
	}{
		{name: "invalid", action: compaction.RequestAction{}, failure: nil, project: false, canceled: false},
		{
			name: "cancel",
			action: compaction.RequestAction{
				Cancel:             true,
				RequestDisposition: compaction.RequestUnspecified,
				Request:            mo.None[compaction.Request](),
				ResultDisposition:  compaction.ResultUnspecified,
				Result:             mo.None[compaction.Result](),
			},
			failure:  nil,
			project:  false,
			canceled: true,
		},
		{
			name:     "failure",
			action:   compaction.RequestAction{},
			failure:  errors.New("complete participant cause"),
			project:  false,
			canceled: false,
		},
		{
			name: "projection",
			action: compaction.RequestAction{
				Cancel:             false,
				RequestDisposition: compaction.RequestPreserve,
				Request:            mo.None[compaction.Request](),
				ResultDisposition:  compaction.ResultReplace,
				Result:             mo.Some(newPolicyResult("rejected")),
			},
			failure:  nil,
			project:  true,
			canceled: false,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := newPolicyInput()
			participant := newPolicyParticipant("handler")
			input.Requests = []compaction.Participant{participant, participant}
			host := NewMockHost(gomock.NewController(t))
			action := test.action
			if test.project {
				replacement, requestErr := compaction.SelectRequest(
					input, 1, compaction.EstimateInput(input.History, input.Instructions, input.ToolText),
				)
				require.NoError(t, requestErr)
				replacement.Guidance = "rejected request update"
				action.RequestDisposition = compaction.RequestReplace
				action.Request = mo.Some(replacement)
			}
			host.EXPECT().Request(gomock.Any(), participant, gomock.Any()).Return(action, test.failure)
			if test.project {
				host.EXPECT().Project(gomock.Any(), gomock.Any()).Return(nil, errors.New("complete projection cause"))
			}
			// Act through the first participant.
			outcome, err := New(host, 1).Compact(t.Context(), input)
			// Assert no later call or persistence occurs and no rejected state is installed.
			require.Equal(t, test.canceled, outcome.Canceled)
			require.True(t, outcome.State.IsSome())
			require.True(t, outcome.State.MustGet().Result.IsNone())
			require.Equal(t, outcome.State.MustGet().Original, outcome.State.MustGet().Current)
			if test.canceled {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if test.failure != nil {
				require.ErrorContains(t, err, test.failure.Error())
			}
			if test.project {
				require.ErrorContains(t, err, "complete projection cause")
			}
		})
	}
}

// TestCompactUsesCapturedBudget checks candidate rejection despite a larger summary model.
func TestCompactUsesCapturedBudget(t *testing.T) {
	t.Parallel()
	// Arrange a handler selecting a larger summary model and a large projected candidate.
	input := newPolicyInput()
	input.Model.Window = 100
	input.Model.Reserve = 10
	participant := newPolicyParticipant("handler")
	input.Requests = []compaction.Participant{participant}
	input.Generators = []compaction.Participant{participant}
	request, err := compaction.SelectRequest(
		input, 1, compaction.EstimateInput(input.History, input.Instructions, input.ToolText),
	)
	require.NoError(t, err)
	request.Model.ID = "summary-model"
	request.Model.Window = 100000
	host := NewMockHost(gomock.NewController(t))
	host.EXPECT().
		Request(gomock.Any(), participant, gomock.Any()).
		Return(compaction.RequestAction{
			Cancel: false, RequestDisposition: compaction.RequestReplace, Request: mo.Some(request),
			ResultDisposition: compaction.ResultPreserve, Result: mo.None[compaction.Result](),
		}, nil)
	result := newPolicyResult("summary")
	host.EXPECT().Generate(gomock.Any(), participant, gomock.Any()).Return(result, nil)
	large := append([]compaction.Entry(nil), input.History...)
	large[0].Blocks = []compaction.Block{
		{
			Kind:      compaction.Text,
			Text:      strings.Repeat("x", 1000),
			MediaType: "",
			Data:      nil,
			CallID:    "",
			Name:      "",
			Arguments: nil,
		},
	}
	host.EXPECT().Project(gomock.Any(), result).Return(large, nil)
	// Act with the captured conversation budget.
	_, err = New(host, 1).Compact(t.Context(), input)
	// Assert failure precedes installation and persistence.
	require.ErrorContains(t, err, "conversation input budget")
}

// TestAutomaticPreparationPreservesHistory checks preservation and independent outgoing-budget rejection.
func TestAutomaticPreparationPreservesHistory(t *testing.T) {
	t.Parallel()
	for _, above := range []bool{false, true} {
		t.Run(fmt.Sprint(above), func(t *testing.T) {
			t.Parallel()
			// Arrange ordinary input at or just above the captured input budget.
			input := newPolicyInput()
			input.Purpose = compaction.PurposeAutomatic
			input.Model.Window = input.Model.Reserve + compaction.EstimateInput(
				input.History,
				input.Instructions,
				input.ToolText,
			)
			if above {
				input.Model.Window--
			}
			host := NewMockHost(gomock.NewController(t))
			// Act through ordinary preparation and independent outgoing validation.
			outcome, err := New(host, 1).Compact(t.Context(), input)
			validation := New(host, 1).ValidateOutgoing(t.Context(), input)
			// Assert that at the input budget, Compact preserves history. Above that budget, Compact starts composition.
			if above {
				require.ErrorContains(t, err, "generator")
				require.False(t, outcome.Preserve)
				require.Equal(t, compaction.TriggerThreshold, outcome.State.MustGet().Original.Trigger)
				require.ErrorContains(t, validation, "conversation input budget")
			} else {
				require.NoError(t, err)
				require.True(t, outcome.Preserve)
				require.True(t, outcome.State.IsNone())
				require.NoError(t, validation)
			}
		})
	}
}

// newPolicyParticipant supplies a capability for policy tests.
func newPolicyParticipant(handler string) compaction.Participant {
	return compaction.Participant{Extension: "extension", Runtime: "runtime", Handler: handler, Order: 0}
}

// newPolicyResult supplies an extension-produced candidate.
func newPolicyResult(summary string) compaction.Result {
	return compaction.Result{
		Summary:   summary,
		FirstKept: "recent",
		Extension: mo.Some("extension"),
		Requests:  nil,
		Details:   mo.None[[]byte](),
	}
}

// newPolicyInput supplies reducible original history.
func newPolicyInput() compaction.Input {
	block := compaction.Block{
		Kind:      compaction.Text,
		Text:      "text",
		MediaType: "",
		Data:      nil,
		CallID:    "",
		Name:      "",
		Arguments: nil,
	}
	old := compaction.Entry{
		ID:           "old",
		Kind:         compaction.User,
		Blocks:       []compaction.Block{block},
		CallID:       "",
		Tool:         "",
		Failed:       false,
		Extension:    "",
		EntryType:    "",
		Visibility:   compaction.VisibilityUnspecified,
		Marker:       mo.None[compaction.Result](),
		PrivateBytes: 0,
		Tokens:       0,
	}
	recent := old
	recent.ID = "recent"
	return compaction.Input{
		Binding:  compaction.Binding{},
		Entries:  []compaction.Entry{old, recent},
		History:  []compaction.Entry{old, recent},
		Previous: mo.None[compaction.Result](),
		Model: compaction.Model{
			Provider:   "provider",
			ID:         "conversation",
			Window:     10000,
			Reserve:    100,
			Modalities: nil,
			Reasoning:  mo.None[compaction.Reasoning](),
			Tools:      mo.None[compaction.ToolCapabilities](),
			Pricing:    mo.None[compaction.Pricing](),
		},
		Reasoning:    "off",
		Instructions: "conversation instructions",
		ToolText:     "tool schema",
		ToolShape:    "tool shape",
		Guidance:     "guidance",
		Purpose:      compaction.PurposeManual,
		Requests:     nil,
		Generators:   nil,
		Results:      nil,
	}
}
