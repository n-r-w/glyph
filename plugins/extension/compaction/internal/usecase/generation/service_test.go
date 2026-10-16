//go:build !integration

package generation

import (
	"context"
	"encoding/json/v2"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestGenerateLimitsToolTextAndAccountsOneRequest checks source preservation and combined Unicode tool-text limits.
func TestGenerateLimitsToolTextAndAccountsOneRequest(t *testing.T) {
	t.Parallel()
	// Arrange several text blocks and an intervening image in one result.
	request := newGenerationRequest()
	tool := request.Prefix[0]
	tool.Kind = compaction.ToolResult
	tool.CallID = "call"
	tool.Tool = "read"
	tool.Blocks = []compaction.Block{
		newGenerationBlock(compaction.Text, strings.Repeat("界", 1500)),
		newGenerationBlock(compaction.Image, ""),
		newGenerationBlock(compaction.Text, strings.Repeat("🙂", 1500)),
	}
	tool.Blocks[1].Data = []byte("encoded image")
	request.Prefix = []compaction.Entry{tool}
	request.Previous = mo.Some(
		compaction.Result{
			Summary:   "preceding summary",
			FirstKept: "old",
			Extension: mo.Some("custom"),
			Requests:  nil,
			Details:   mo.None[[]byte](),
		},
	)
	model := NewMockModel(gomock.NewController(t))
	usage := compaction.Usage{Counts: [6]int64{1, 2, 3, 4, 1, 10}}
	model.EXPECT().
		Request(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, input Input) (Response, error) {
			require.Equal(t, request.Model, input.Model)
			require.Equal(t, request.Reasoning, input.Reasoning)
			require.Contains(t, input.Instructions, request.Guidance)
			var source struct {
				Previous string
				Messages []struct {
					Role   string
					CallID string
					Tool   string
					Blocks []struct {
						Kind string
						Text string
					}
				}
			}
			require.NoError(t, json.Unmarshal([]byte(input.Source), &source))
			require.Equal(t, "preceding summary", source.Previous)
			require.Len(t, source.Messages, 1)
			require.Equal(t, "call", source.Messages[0].CallID)
			require.Equal(t, "read", source.Messages[0].Tool)
			var text strings.Builder
			for _, block := range source.Messages[0].Blocks {
				require.NotEqual(t, compaction.Image, block.Kind)
				text.WriteString(block.Text)
			}
			require.Contains(t, text.String(), strings.Repeat("界", 1500)+strings.Repeat("🙂", 500))
			require.Equal(t, 1, strings.Count(text.String(), truncationMarker))
			require.Equal(t, 2000+utf8.RuneCountInString(truncationMarker), utf8.RuneCountInString(text.String()))
			return Response{
				Outcome: OutcomeStop,
				Blocks: []compaction.Block{
					newGenerationBlock(compaction.Thought, "private thought"),
					newGenerationBlock(compaction.Text, "summary"),
				},
				Error: "",
				Usage: mo.Some(usage),
			}, nil
		})
	// Act through the generator boundary.
	result, err := New(model).Generate(t.Context(), request)
	// Assert one request's actual selection and usage, and untouched original records.
	require.NoError(t, err)
	require.Equal(t, "summary", result.Summary)
	require.Equal(t, "recent", result.FirstKept)
	require.Len(t, result.Requests, 1)
	require.Equal(t, request.Model.ID, result.Requests[0].Model)
	require.Equal(t, request.Model.Provider, result.Requests[0].Provider)
	require.Equal(t, request.Reasoning, result.Requests[0].Reasoning)
	require.Equal(t, usage, result.Requests[0].Usage.MustGet())
	require.Len(t, request.Prefix[0].Blocks, 3)
	require.Equal(t, 1500, utf8.RuneCountInString(request.Prefix[0].Blocks[2].Text))
}

// TestGenerateRejectsUnusableResponses checks preservation of complete model and validation causes.
func TestGenerateRejectsUnusableResponses(t *testing.T) {
	t.Parallel()
	// Arrange terminal outcomes and malformed public content.
	cases := []struct {
		name    string
		outcome string
		blocks  []compaction.Block
		cause   string
		failure error
		usable  bool
	}{
		{
			name:    "stop",
			outcome: "stop",
			blocks:  []compaction.Block{newGenerationBlock(compaction.Text, "summary")},
			cause:   "",
			failure: nil,
			usable:  true,
		},
		{
			name:    "length",
			outcome: "length",
			blocks:  []compaction.Block{newGenerationBlock(compaction.Text, "summary")},
			cause:   "",
			failure: nil,
			usable:  true,
		},
		{
			name:    "failed",
			outcome: "failed",
			blocks:  nil,
			cause:   "provider complete failure",
			failure: nil,
			usable:  false,
		},
		{name: "aborted", outcome: "aborted", blocks: nil, cause: "provider abort cause", failure: nil, usable: false},
		{
			name:    "empty",
			outcome: "stop",
			blocks:  []compaction.Block{newGenerationBlock(compaction.Text, "  ")},
			cause:   "",
			failure: nil,
			usable:  false,
		},
		{
			name:    "reasoning-only",
			outcome: "stop",
			blocks:  []compaction.Block{newGenerationBlock(compaction.Thought, "thought")},
			cause:   "",
			failure: nil,
			usable:  false,
		},
		{
			name:    "tool",
			outcome: "stop",
			blocks:  []compaction.Block{newGenerationBlock(compaction.Call, "")},
			cause:   "",
			failure: nil,
			usable:  false,
		},
		{
			name:    "transport",
			outcome: "",
			blocks:  nil,
			cause:   "",
			failure: errors.New("complete transport cause"),
			usable:  false,
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			model := NewMockModel(gomock.NewController(t))
			model.EXPECT().
				Request(gomock.Any(), gomock.Any()).
				Return(Response{Outcome: test.outcome, Blocks: test.blocks, Error: test.cause, Usage: mo.None[compaction.Usage]()}, test.failure)
			// Act on one terminal response.
			result, err := New(model).Generate(t.Context(), newGenerationRequest())
			// Assert public text is usable only for STOP/LENGTH without calls.
			if test.usable {
				require.NoError(t, err)
				require.Equal(t, "summary", result.Summary)
			} else {
				require.Error(t, err)
			}
			if test.cause != "" {
				require.ErrorContains(t, err, test.cause)
			}
			if test.failure != nil {
				require.ErrorIs(t, err, test.failure)
			}
		})
	}
}

// newGenerationRequest supplies a composed request.
func newGenerationRequest() compaction.Request {
	entry := compaction.Entry{
		ID:           "old",
		Kind:         compaction.User,
		Blocks:       []compaction.Block{newGenerationBlock(compaction.Text, "original text")},
		CallID:       "",
		Tool:         "",
		Failed:       false,
		Extension:    "",
		EntryType:    "",
		Visibility:   compaction.VisibilityUnspecified,
		Marker:       mo.None[compaction.Result](),
		PrivateBytes: 0,
		Tokens:       1,
	}
	recent := entry
	recent.ID = "recent"
	return compaction.Request{
		Trigger:  compaction.TriggerManual,
		Retry:    false,
		Guidance: "preserve design decisions",
		Model: compaction.Model{
			Provider:   "provider",
			ID:         "composed-summary-model",
			Window:     100000,
			Reserve:    1000,
			Modalities: nil,
			Reasoning:  mo.None[compaction.Reasoning](),
			Tools:      mo.None[compaction.ToolCapabilities](),
			Pricing:    mo.None[compaction.Pricing](),
		},
		Reasoning:     "high",
		Prefix:        []compaction.Entry{entry},
		Suffix:        []compaction.Entry{recent},
		Previous:      mo.None[compaction.Result](),
		ContextTokens: 10,
		Estimated:     true,
		Window:        10000,
		Reserve:       1000,
		Retained:      1,
	}
}

// newGenerationBlock supplies one semantic content block.
func newGenerationBlock(kind, text string) compaction.Block {
	return compaction.Block{Kind: kind, Text: text, MediaType: "", Data: nil, CallID: "", Name: "", Arguments: nil}
}
