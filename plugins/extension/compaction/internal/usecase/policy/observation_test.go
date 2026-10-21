//go:build !integration

package policy

import (
	"slices"
	"strconv"
	"sync"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestObservationContinuation checks reuse of reported usage for conversation continuations.
func TestObservationContinuation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*compaction.Input)
		reuse  bool
	}{
		{"exact", func(*compaction.Input) {}, true},
		{"source-metadata", func(i *compaction.Input) {
			i.History[0].ID = "rebound-source"
			i.History[0].SourcePosition = 99
			i.History[0].SourceKind = compaction.SourceExtensionMessage
			i.History[0].PreviousSummary = true
		}, true},
		{"history-text", func(i *compaction.Input) { i.History[0].Blocks[0].Text = "changed" }, false},
		{
			"image-encoding",
			func(i *compaction.Input) { i.History[0].Blocks[1].Data = []byte("different encoding") },
			false,
		},
		{"tool-arguments", func(i *compaction.Input) { i.History[2].Blocks[1].Arguments = []byte(`{"x":2}`) }, false},
		{"tool-identity", func(i *compaction.Input) { i.History[2].Blocks[1].CallID = "other" }, false},
		{"response-text", func(i *compaction.Input) { i.History[2].Blocks[0].Text = "changed" }, false},
		{"private-bytes", func(i *compaction.Input) { i.History[2].PrivateBytes++ }, false},
		{"history-removed", func(i *compaction.Input) { i.History = i.History[1:] }, false},
		{"incomplete-response", func(i *compaction.Input) { i.History = i.History[:2] }, false},
		{"reasoning", func(i *compaction.Input) { i.Reasoning = "high" }, false},
		{"instructions", func(i *compaction.Input) { i.Instructions += " changed" }, false},
		{"tools", func(i *compaction.Input) { i.ToolShape += " changed" }, false},
		{"provider", func(i *compaction.Input) { i.Model.Provider = "other" }, false},
		{"model", func(i *compaction.Input) { i.Model.ID = "other" }, false},
		{"binding", func(i *compaction.Input) { i.Binding.Context = "other" }, false},
		{"runtime", func(i *compaction.Input) { i.Binding.Runtime = "other" }, false},
		{"session", func(i *compaction.Input) { i.Binding.Session = "other" }, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a successful attempt and an exact continuation with new user input.
			service := New(nil, 1)
			observed, next := newObservedContinuation()
			require.NoError(t, service.Observe(t.Context(), observed))
			test.change(&next)
			expected := compaction.EstimateInput(next.History, next.Instructions, next.ToolText)
			if test.reuse {
				expected = 98 + next.History[3].EstimateTokens()
			}
			// Act on the next captured conversation.
			actual := service.estimateInput(next)
			// Assert that after request-shape changes, estimateInput uses a content estimate.
			require.Equal(t, expected, actual)
		})
	}
}

// TestObservationOwnership checks detached storage, missing usage, and restart fallback.
func TestObservationOwnership(t *testing.T) {
	t.Parallel()
	// Arrange detached request and response values.
	observed, next := newObservedContinuation()
	service := New(nil, 1)
	require.NoError(t, service.Observe(t.Context(), observed))
	// Act by mutating the caller's values after acknowledgement.
	observed.Input.History[0].Blocks[0].Text = "caller mutation"
	observed.Response.Blocks[0].Text = "response mutation"
	observed.Input.History[0].Blocks[1].Data[0] = 'X'
	observed.Response.Blocks[1].Arguments[0] = 'X'
	// Assert stored observations retain the actual completed context.
	require.Equal(t, int64(98)+next.History[3].EstimateTokens(), service.estimateInput(next))
	require.Equal(
		t,
		compaction.EstimateInput(next.History, next.Instructions, next.ToolText),
		New(nil, 1).estimateInput(next),
	)
	// Arrange a later observation without usage for the same binding/model.
	observed, _ = newObservedContinuation()
	observed.Usage = mo.None[compaction.Usage]()
	// Act after the missing-usage observation.
	require.NoError(t, service.Observe(t.Context(), observed))
	// Assert that after the observation without usage, estimateInput uses a content estimate.
	require.Equal(
		t,
		compaction.EstimateInput(next.History, next.Instructions, next.ToolText),
		service.estimateInput(next),
	)
}

// TestObservationConcurrentBindings checks callback access without cross-binding estimates.
func TestObservationConcurrentBindings(t *testing.T) {
	t.Parallel()
	// Arrange a shared policy owner.
	service := New(nil, 1)
	var workers, recorded sync.WaitGroup
	recorded.Add(20)
	ready := make(chan struct{})
	errors := make(chan error, 20)
	for index := range 20 {
		workers.Go(func() {
			observed, next := newObservedContinuation()
			observed.Input.Binding.Context = strconv.Itoa(index / 2)
			observed.Input.Model.ID = strconv.Itoa(index % 2)
			next.Binding = observed.Input.Binding
			next.Model = observed.Input.Model
			usage := observed.Usage.MustGet()
			usage.Counts[0] += int64(index * 100)
			observed.Usage = mo.Some(usage)
			// Act with concurrent observation callbacks before any estimate is read.
			errors <- service.Observe(t.Context(), observed)
			recorded.Done()
			<-ready
			// Assert that the service keeps reported usage separate for each binding and model.
			assert.Equal(t, int64(98+index*100)+next.History[3].EstimateTokens(), service.estimateInput(next))
		})
	}
	recorded.Wait()
	close(ready)
	workers.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
}

// newObservedContinuation supplies a completed response followed by input growth.
func newObservedContinuation() (compaction.Observation, compaction.Input) {
	input := newPolicyInput()
	input.Binding = compaction.Binding{Context: "binding", Runtime: "runtime", Session: "session"}
	input.ToolShape = "tools"
	image := compaction.Block{
		Kind:      compaction.Image,
		Text:      "",
		MediaType: "image/png",
		Data:      []byte("image"),
		CallID:    "",
		Name:      "",
		Arguments: nil,
	}
	input.History[0].Blocks = append(slices.Clone(input.History[0].Blocks), image)
	response := input.History[1]
	response.Kind = compaction.Assistant
	response.SourceKind = compaction.SourceModel
	response.Blocks = append(
		slices.Clone(response.Blocks),
		compaction.Block{
			Kind:      compaction.Call,
			Text:      "",
			MediaType: "",
			Data:      nil,
			CallID:    "call",
			Name:      "tool",
			Arguments: []byte(`{"x":1}`),
		},
	)
	response.PrivateBytes = 12
	next := input
	next.History = slices.Clone(input.History)
	for index := range next.History {
		next.History[index].Blocks = slices.Clone(next.History[index].Blocks)
		for j := range next.History[index].Blocks {
			next.History[index].Blocks[j].Data = slices.Clone(next.History[index].Blocks[j].Data)
		}
	}
	next.History = append(next.History, response, input.History[0])
	for index := range next.History {
		next.History[index].SourcePosition = int64(index)
	}
	next.History[2].Blocks = slices.Clone(response.Blocks)
	next.History[2].Blocks[1].Arguments = slices.Clone(response.Blocks[1].Arguments)
	return compaction.Observation{
		Input:    input,
		Response: response,
		Usage:    mo.Some(compaction.Usage{Counts: [6]int64{80, 10, 5, 3, 2, 98}}),
	}, next
}
