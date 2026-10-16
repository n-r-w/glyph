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
	// Act with a target smaller than the final tool group.
	request, err := SelectRequest(input, 1)
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
	request, err = SelectRequest(input, 1)
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
	_, err = SelectRequest(input, 1)
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

// newTextEntry supplies one original content record.
func newTextEntry(id, kind, text string) Entry {
	return Entry{
		ID:           id,
		Kind:         kind,
		Blocks:       []Block{{Kind: Text, Text: text, MediaType: "", Data: nil, CallID: "", Name: "", Arguments: nil}},
		CallID:       "",
		Tool:         "",
		Failed:       false,
		Extension:    "",
		EntryType:    "",
		Visibility:   VisibilityUnspecified,
		Marker:       mo.None[Result](),
		PrivateBytes: 0,
		Tokens:       0,
	}
}

// newCallBlock supplies a tool request in a model response.
func newCallBlock(id string) Block {
	return Block{Kind: Call, Text: "", MediaType: "", Data: nil, CallID: id, Name: "tool", Arguments: []byte(`{}`)}
}

// newInput supplies a captured conversation for retention tests.
func newInput(entries []Entry) Input {
	return Input{
		Entries:  entries,
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
		Guidance:     "manual guidance",
		Purpose:      PurposeManual,
		Requests:     nil,
		Generators:   nil,
		Results:      nil,
	}
}
