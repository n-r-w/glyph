//go:build !integration

package extension

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestCompletedConversationRecordsBeforeAcknowledgement checks recording and acknowledgement
// of completed conversations.
func TestCompletedConversationRecordsBeforeAcknowledgement(t *testing.T) {
	t.Parallel()
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "acknowledged", true: "recording-failed"}[failed], func(t *testing.T) {
			t.Parallel()
			// Arrange a public callback containing actual input, response, and reported usage.
			policy := NewMockPolicy(gomock.NewController(t))
			request := new(pb.HandleRequest)
			request.SetContext(
				pb.ExtensionContext_builder{
					ContextId:         new("binding"),
					ExtensionId:       nil,
					Cwd:               nil,
					RuntimeInstanceId: new("runtime"),
					SessionId:         new("session"),
				}.Build(),
			)
			request.SetCompletedConversation(pb.CompletedConversationObservation_builder{
				Model: pb.ModelDescriptor_builder{
					ProviderId: new(
						"provider",
					),
					ModelId:         new("model"),
					InputModalities: nil,
					ContextWindow:   nil,
					MaxTokens:       nil,
					Reasoning:       nil,
					Tools:           nil,
					Pricing:         nil,
				}.Build(),
				ReasoningChoice: new("high"),
				Instructions:    new("instructions"),
				History: []*pb.CompactionHistoryRecord{
					pb.CompactionHistoryRecord_builder{
						Model: nil, ToolResult: nil,
						User: pb.SessionTreeUserMessage_builder{
							Content: []*pb.SessionTreeUserContent{
								pb.SessionTreeUserContent_builder{Text: new("user"), Image: nil}.Build(),
							},
						}.Build(),
						PrivatePayloadBytes: new(int64(8)),
					}.Build(),
				},
				Tools: nil,
				Response: pb.ConfiguredModelResult_builder{
					Outcome:         nil,
					ErrorMessage:    nil,
					ProviderId:      nil,
					ModelId:         nil,
					ResponseModelId: nil,
					ResponseId:      nil,
					Diagnostics:     nil,
					Content: []*pb.ConfiguredModelContent{
						pb.ConfiguredModelContent_builder{
							Text: nil, Refusal: nil, Reasoning: nil,
							ToolCall: pb.ConfiguredModelToolCall_builder{
								Id:            new("call"),
								Name:          new("tool"),
								ArgumentsJson: []byte(`{"x":1}`),
							}.Build(),
						}.Build(),
					},
					Usage: pb.ConfiguredModelUsage_builder{
						InputTokens:       new(int64(7)),
						OutputTokens:      new(int64(4)),
						CachedInputTokens: nil, CacheWriteTokens: nil, ReasoningTokens: nil, TotalTokens: nil,
					}.Build(),
				}.Build(),
				ResponsePrivatePayloadBytes: new(int64(12)),
			}.Build())
			cause := errors.New("observation recording cause")
			policy.EXPECT().
				Observe(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, observed compaction.Observation) error {
					require.Equal(
						t,
						compaction.Binding{Context: "binding", Runtime: "runtime", Session: "session"},
						observed.Input.Binding,
					)
					require.Equal(t, "high", observed.Input.Reasoning)
					require.Equal(t, "instructions", observed.Input.Instructions)
					require.Equal(t, int64(8), observed.Input.History[0].PrivateBytes)
					require.Equal(t, int64(12), observed.Response.PrivateBytes)
					require.Equal(t, "call", observed.Response.Blocks[0].CallID)
					require.Equal(t, "tool", observed.Response.Blocks[0].Name)
					require.JSONEq(t, `{"x":1}`, string(observed.Response.Blocks[0].Arguments))
					require.Equal(t, int64(7), observed.Usage.MustGet().Counts[0])
					if failed {
						return cause
					}
					return nil
				})
			operation := &handler{service: New(policy, nil, false), request: request}
			// Act through the production callback dispatcher.
			response, err := operation.Run(t.Context())
			// Assert acknowledgement follows successful recording only.
			if failed {
				require.ErrorIs(t, err, cause)
				require.Nil(t, response)
			} else {
				require.NoError(t, err)
				require.NotNil(t, response.GetCompletedConversation())
			}
		})
	}
}
