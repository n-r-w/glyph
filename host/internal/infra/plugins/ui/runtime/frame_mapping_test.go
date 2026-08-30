package runtime

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// TestChannelMapsEveryFrameAndCommand verifies the complete generated transport boundary.
func TestChannelMapsEveryFrameAndCommand(t *testing.T) {
	t.Parallel()

	// Arrange a runtime stream with every frame and command contract variant.
	service := &runtimeContractService{
		UnimplementedUIServiceServer: uipb.UnimplementedUIServiceServer{},
		received:                     make(chan *uipb.OpenRequest, 6),
	}
	client := uisdk.TestClient(t, service)
	stream, err := client.Open(t.Context())
	require.NoError(t, err)
	_, cancel := context.WithCancel(t.Context())
	transport := &channel{
		stream: stream,
		cancel: cancel,
		closed: atomic.Bool{},
		mutex:  sync.Mutex{},
	}
	frames := []domainui.Frame{
		testInitializationFrame(),
		testLifecycleFrame(),
		testSimpleFrame(domainui.FrameAuthorization, "https://auth.example"),
		testSimpleFrame(domainui.FrameInformation, "information"),
		testErrorFrame(),
		testModelSelectionFrame(),
	}

	// Act by sending every host frame and receiving every UI command.
	for _, frame := range frames {
		require.NoError(t, transport.Send(frame))
	}
	// Assert every frame reaches the service and every command maps exactly.
	for range frames {
		assert.NotNil(t, <-service.received)
	}
	for _, expected := range []domainui.Command{
		{
			Kind:            domainui.CommandSubmit,
			Text:            mo.Some("request"),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](), EntryLabel: mo.None[string](),
		},
		{
			Kind:            domainui.CommandStop,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](), EntryLabel: mo.None[string](),
		},
		{
			Kind:            domainui.CommandRetryAuthentication,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](), EntryLabel: mo.None[string](),
		},
		{
			Kind:            domainui.CommandQuit,
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](), EntryLabel: mo.None[string](),
		},
		{
			Kind:            domainui.CommandSelectModel,
			ProviderID:      mo.Some("openrouter"),
			ModelID:         mo.Some("sonnet"),
			Text:            mo.None[string](),
			ReasoningChoice: mo.None[domainui.ReasoningChoice](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](), EntryLabel: mo.None[string](),
		},
		{
			Kind:            domainui.CommandSelectReasoningChoice,
			ReasoningChoice: mo.Some(domainui.ReasoningChoiceXHigh),
			Text:            mo.None[string](),
			ProviderID:      mo.None[string](),
			ModelID:         mo.None[string](),
			SessionID:       mo.None[string](),
			SessionName:     mo.None[string](),
			TargetEntryID:   mo.None[string](),
			SummaryMode:     domainui.SummaryModeNoSummary,
			CustomFocus:     mo.None[string](), EntryLabel: mo.None[string](),
		},
	} {
		command, receiveErr := transport.Receive()
		require.NoError(t, receiveErr)
		assert.Equal(t, expected, command)
	}
}

// TestRestoredSessionImageDataPresence verifies restored image presence and ownership after UI serialization.
func TestRestoredSessionImageDataPresence(t *testing.T) {
	t.Parallel()

	// Arrange user and tool-result images for every observable data-presence state.
	tests := []struct {
		name        string
		data        mo.Option[[]byte]
		expectError bool
		expectData  []byte
	}{
		{name: "absent data", data: mo.None[[]byte](), expectError: true, expectData: nil},
		{name: "present nil data", data: mo.Some[[]byte](nil), expectError: false, expectData: []byte{}},
		{name: "present non-nil empty data", data: mo.Some([]byte{}), expectError: false, expectData: []byte{}},
		{name: "nonempty data", data: mo.Some([]byte{1, 2, 3}), expectError: false, expectData: []byte{1, 2, 3}},
	}

	for _, test := range tests {
		t.Run("user "+test.name, func(t *testing.T) {
			t.Parallel()
			inputData := test.data
			if data, present := test.data.Get(); present {
				inputData = mo.Some(bytes.Clone(data))
			}
			// Act by mapping and serializing a restored user image.
			mapped, err := mapRestoredSessionEntries([]domainui.SessionEntry{{
				ID: "user", CreatedAt: time.Unix(1, 0), Kind: domainui.SessionEntryUser,
				User: mo.Some(model.Message{Content: []model.InputContent{{
					Kind: model.InputContentImage, Text: mo.None[string](),
					MediaType: mo.Some("image/png"), Data: inputData,
				}}}),
				Model: mo.None[domainui.ModelResponse](), ToolResult: mo.None[agent.ToolResult](),
				BranchSummary: mo.None[domainui.BranchSummary](),
			}})

			// Assert validation, oneof selection, presence, bytes, and ownership.
			if test.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if source, present := inputData.Get(); present && len(source) != 0 {
				source[0] = 99
			}
			payload, err := proto.Marshal(mapped[0])
			require.NoError(t, err)
			roundTripped := new(uipb.SessionEntry)
			require.NoError(t, proto.Unmarshal(payload, roundTripped))
			require.Len(t, roundTripped.GetUser().GetContent(), 1)
			content := roundTripped.GetUser().GetContent()[0]
			assert.Equal(t, uipb.UserContent_Image_case, content.WhichContent())
			require.NotNil(t, content.GetImage())
			assert.True(t, content.GetImage().HasData())
			assert.Equal(t, test.expectData, content.GetImage().GetData())
		})

		t.Run("tool result "+test.name, func(t *testing.T) {
			t.Parallel()
			inputData := test.data
			if data, present := test.data.Get(); present {
				inputData = mo.Some(bytes.Clone(data))
			}
			image := mo.None[tool.ResultImage]()
			if data, present := inputData.Get(); present {
				image = mo.Some(tool.ResultImage{MediaType: "image/png", Data: data})
			}
			// Act by mapping and serializing a restored tool-result image.
			mapped, err := mapRestoredSessionEntries([]domainui.SessionEntry{{
				ID: "tool", CreatedAt: time.Unix(1, 0), Kind: domainui.SessionEntryToolResult,
				User: mo.None[model.Message](), Model: mo.None[domainui.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "render", IsError: false,
					Contents: []tool.ResultContent{{
						Kind: tool.ResultContentImage, Text: mo.None[string](), Image: image,
					}},
				}),
				BranchSummary: mo.None[domainui.BranchSummary](),
			}})

			// Assert absent images stay absent and present image bytes retain presence and ownership.
			require.NoError(t, err)
			if test.expectError {
				require.Empty(t, mapped[0].GetToolResult().GetContents())
				return
			}
			if source, present := inputData.Get(); present && len(source) != 0 {
				source[0] = 99
			}
			payload, err := proto.Marshal(mapped[0])
			require.NoError(t, err)
			roundTripped := new(uipb.SessionEntry)
			require.NoError(t, proto.Unmarshal(payload, roundTripped))
			require.Len(t, roundTripped.GetToolResult().GetContents(), 1)
			content := roundTripped.GetToolResult().GetContents()[0]
			assert.Equal(t, uipb.ToolResultContent_Image_case, content.WhichContent())
			require.NotNil(t, content.GetImage())
			assert.True(t, content.GetImage().HasData())
			assert.Equal(t, test.expectData, content.GetImage().GetData())
		})
	}
}

// TestRestoredSessionBranchSummaryMapsCompletePayload verifies summary restoration through the UI wire contract.
func TestRestoredSessionBranchSummaryMapsCompletePayload(t *testing.T) {
	t.Parallel()

	// Arrange one complete branch-summary transcript entry.
	summary := domainui.BranchSummary{
		Summary: "branch context", FirstEntryID: "first", LastEntryID: "last",
		Provider: model.ProviderID("provider"), Model: model.ID("model"),
		ReasoningChoice: model.ReasoningChoiceMedium,
		Usage: mo.Some(session.TokenUsage{
			InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3,
			CacheWriteTokens: 4, ReasoningTokens: 5, TotalTokens: 15,
		}),
		EstimatedCost: mo.Some(session.EstimatedCost{
			Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
		}),
	}
	entry := domainui.SessionEntry{
		ID: "summary", CreatedAt: time.Unix(1, 0), Kind: domainui.SessionEntryBranchSummary,
		User: mo.None[model.Message](), Model: mo.None[domainui.ModelResponse](),
		ToolResult: mo.None[agent.ToolResult](), BranchSummary: mo.Some(summary),
	}

	// Act by mapping the restored entry to the generated UI contract.
	mapped, err := mapRestoredSessionEntries([]domainui.SessionEntry{entry})

	// Assert the oneof and complete summary payload survive mapping.
	require.NoError(t, err)
	require.Len(t, mapped, 1)
	require.Equal(t, uipb.SessionEntry_BranchSummary_case, mapped[0].WhichEntry())
	require.Equal(t, summary.Summary, mapped[0].GetBranchSummary().GetSummary())
	require.Equal(t, summary.FirstEntryID, mapped[0].GetBranchSummary().GetFirstEntryId())
	require.Equal(t, summary.LastEntryID, mapped[0].GetBranchSummary().GetLastEntryId())
	require.Equal(t, string(summary.Provider), mapped[0].GetBranchSummary().GetProviderId())
	require.Equal(t, string(summary.Model), mapped[0].GetBranchSummary().GetModelId())
	require.NotNil(t, mapped[0].GetBranchSummary().GetUsage())
	require.NotNil(t, mapped[0].GetBranchSummary().GetEstimatedCost())
}
