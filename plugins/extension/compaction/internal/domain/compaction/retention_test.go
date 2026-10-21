//go:build !integration

package compaction

import (
	"strings"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestSelectRequest checks indivisible tool-group retention across repeated compaction.
func TestSelectRequest(t *testing.T) {
	t.Parallel()
	// Arrange original entries with two results from one model response.
	entries := []Entry{
		newTextEntry("old", User, "older context"),
		newTextEntry("recent", User, "recent request"),
		newTextEntry("calls", Assistant, ""),
		newTextEntry("result-a", ToolResult, "first result"),
		newTextEntry("result-b", ToolResult, "second result"),
	}
	entries[2].Blocks = []Block{newCallBlock("a"), newCallBlock("b")}
	entries[3].CallID = "a"
	entries[4].CallID = "b"
	input := newInput(entries)
	tokens := EstimateInput(input.History, input.Instructions, input.ToolText)
	// Act with a target smaller than the final tool group.
	request, err := SelectRequest(input, 1, tokens)
	// Assert the producing response stays with both results.
	require.NoError(t, err)
	require.Len(t, request.Prefix, 2)
	require.Len(t, request.Suffix, 3)
	require.Equal(t, "calls", request.Suffix[0].ID)
	require.Positive(t, request.ContextTokens)
	require.True(t, request.Estimated)
	previous := Result{
		Summary:   "preceding summary",
		FirstKept: "recent",
		Extension: mo.Some("custom"),
		Requests:  nil,
		Details:   mo.None[[]byte](),
	}
	input.Previous = mo.Some(previous)
	input.History = input.History[1:]
	request, err = SelectRequest(input, 1, tokens)
	require.NoError(t, err)
	require.Len(t, request.Prefix, 1)
	require.Equal(t, "recent", request.Prefix[0].ID)
	require.Equal(t, previous, request.Previous.MustGet())
	input.Previous = mo.Some(
		Result{
			Summary:   "later summary",
			FirstKept: "calls",
			Extension: mo.Some("custom"),
			Requests:  nil,
			Details:   mo.None[[]byte](),
		},
	)
	input.History = input.History[1:]
	_, err = SelectRequest(input, 1, tokens)
	require.ErrorContains(t, err, "no older reducible")
}

// TestEstimateInput counts full request contributions independently of image encoding.
func TestEstimateInput(t *testing.T) {
	t.Parallel()
	// Arrange identical image counts with different encoded sizes.
	history := []Entry{newTextEntry("image", User, "caption")}
	history[0].Blocks = append(
		history[0].Blocks,
		Block{Kind: Image, Text: "", MediaType: "image/png", Data: []byte{1}, CallID: "", Name: "", Arguments: nil},
	)
	// Act on text, image, instruction, tool, and private-data growth.
	base := EstimateInput(history, "", "")
	history[0].Blocks[1].Data = []byte(strings.Repeat("x", 10000))
	encoded := EstimateInput(history, "", "")
	full := EstimateInput(history, strings.Repeat("i", 100), strings.Repeat("t", 100))
	history[0].PrivateBytes = 100
	private := EstimateInput(history, "", "")
	// Assert every input contribution except encoded image length affects sizing.
	require.Positive(t, base)
	require.Equal(t, base, encoded)
	require.Greater(t, full, base)
	require.Greater(t, private, base)
}

// newTextEntry supplies one prepared text record.
func newTextEntry(id, kind, text string) Entry {
	sourceKind := SourceUser
	switch kind {
	case Assistant:
		sourceKind = SourceModel
	case ToolResult:
		sourceKind = SourceToolResult
	}

	return Entry{
		ID:   id,
		Kind: kind,
		Blocks: []Block{
			{Kind: Text, Text: text, MediaType: "", Data: nil, CallID: "", Name: "", Arguments: nil},
		},
		CallID:          "",
		Tool:            "",
		Failed:          false,
		SourcePosition:  0,
		SourceKind:      sourceKind,
		PreviousSummary: false,
		PrivateBytes:    0,
		Tokens:          0,
	}
}

// newCallBlock supplies a tool request in a model response.
func newCallBlock(id string) Block {
	return Block{Kind: Call, Text: "", MediaType: "", Data: nil, CallID: id, Name: "tool", Arguments: []byte(`{}`)}
}

// newInput supplies a captured conversation for retention tests.
func newInput(entries []Entry) Input {
	for index := range entries {
		entries[index].SourcePosition = int64(index)
	}
	return Input{
		Binding:  Binding{},
		History:  entries,
		Previous: mo.None[Result](),
		Model: Model{
			Provider:   "provider",
			ID:         "model",
			Window:     100000,
			Reserve:    1000,
			Modalities: nil,
			Reasoning:  mo.None[Reasoning](),
			Tools:      mo.None[ToolCapabilities](),
			Pricing:    mo.None[Pricing](),
		},
		Reasoning:    "off",
		Instructions: "conversation instructions",
		ToolText:     "tool schema",
		ToolShape:    "tool shape",
		Guidance:     "manual guidance",
		Purpose:      PurposeManual,
		Requests:     nil,
		Generators:   nil,
		Results:      nil,
	}
}

// TestSelectRequestPreparedSourceSpans preserves original-branch spans across reordered and shared sources.
func TestSelectRequestPreparedSourceSpans(t *testing.T) {
	t.Parallel()
	// Arrange prepared A,C,synthetic-result,B,D with original positions A,B,C,D.
	entries := []Entry{
		newTextEntry("old", User, "old"), newTextEntry("calls", Assistant, ""),
		newTextEntry("actual", ToolResult, "actual result"), newTextEntry("calls", ToolResult, "skipped result"),
		newTextEntry("intervening", User, "between call and result"), newTextEntry("recent", User, "recent"),
	}
	entries[1].Blocks = []Block{newCallBlock("a"), newCallBlock("b")}
	entries[2].CallID = "a"
	entries[3].CallID = "b"
	entries[3].Failed = true
	entries[3].SourceKind = SourceModel
	input := newInput(entries)
	for index, position := range []int64{0, 1, 3, 1, 2, 4} {
		input.History[index].SourcePosition = position
	}
	// Act with targets that either summarize or retain the whole original span.
	summarized, err := SelectRequest(input, 1, 100)
	require.NoError(t, err)
	retained, err := SelectRequest(input, input.History[5].EstimateTokens()+1, 100)
	// Assert neither shared response sources nor the interleaved original message are split.
	require.NoError(t, err)
	require.Len(t, summarized.Prefix, 5)
	require.Equal(t, "recent", summarized.Suffix[0].ID)
	require.Len(t, retained.Prefix, 1)
	require.Equal(t, "calls", retained.Suffix[0].ID)
	require.NoError(t, summarized.Validate())
	require.NoError(t, retained.Validate())
}

// TestSelectRequestCountsPreviousSummaryOnce sizes complete input while retaining only recent unsummarized groups.
func TestSelectRequestCountsPreviousSummaryOnce(t *testing.T) {
	t.Parallel()
	// Arrange a rendered summary larger than the retained target and two remaining original groups.
	input := newInput([]Entry{newTextEntry("old", User, "old"), newTextEntry("recent", User, "recent")})
	previous := Result{
		Summary:   "preceding summary",
		FirstKept: "old",
		Extension: mo.Some("custom"),
		Requests:  nil,
		Details:   mo.None[[]byte](),
	}
	input.Previous = mo.Some(previous)
	summary := newTextEntry("marker", User, strings.Repeat("s", 1000))
	summary.SourcePosition = 20
	summary.SourceKind = SourceCompaction
	summary.PreviousSummary = true
	without := EstimateInput(input.History, input.Instructions, input.ToolText)
	input.History = append([]Entry{summary}, input.History...)
	// Act through automatic estimation and retained-history selection.
	complete := EstimateInput(input.History, input.Instructions, input.ToolText)
	selected, err := SelectRequest(input, 1, complete)
	// Assert one rendered summary contributes to sizing and none to the selected prefix or recent-history target.
	require.NoError(t, err)
	require.Equal(t, without+summary.EstimateTokens(), complete)
	require.Equal(t, complete, selected.ContextTokens)
	require.Len(t, selected.Prefix, 1)
	require.Equal(t, "old", selected.Prefix[0].ID)
	require.Len(t, selected.Suffix, 1)
	require.Equal(t, "recent", selected.Suffix[0].ID)
	require.Equal(t, previous, selected.Previous.MustGet())
}
