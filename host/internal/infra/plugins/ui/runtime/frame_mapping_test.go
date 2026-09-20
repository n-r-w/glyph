//go:build !integration

package runtime

import (
	"bytes"
	"testing"
	"time"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapCompactionFramePreservesPostCommitFailure verifies public UI result classification and text.
func TestMapCompactionFramePreservesPostCommitFailure(t *testing.T) {
	t.Parallel()
	// Arrange one completed compaction frame with a typed post-commit failure projection.
	frame := controllerui.NewFrame(controllerui.FrameCompaction)
	frame.CompactionCanceled = mo.Some(false)
	frame.CompactionError = mo.Some("publication failed after commit")
	frame.CompactionFailureCode = mo.Some(controllerui.FailureCodeInternal)
	entry, present, err := controllerui.ProjectSessionEntry(session.Entry{
		ID: "compaction", ParentID: mo.None[string](), CreatedAt: time.Unix(1, 0),
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
		EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(session.CompactionEntry{
			Summary: "summary", FirstKeptEntryID: "kept",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
			},
			EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
		}),
	}, 0)
	require.NoError(t, err)
	require.True(t, present)
	frame.SessionEntries = []controllerui.SessionEntry{entry}

	// Act through the production UI Plugin protobuf mapping.
	request, err := mapFrame(frame)

	// Assert complete text and stable category survive in the completed result.
	require.NoError(t, err)
	result := request.GetEvent().GetCompleted().GetCompaction()
	require.Equal(t, "compaction", result.GetCommitted().GetId())
	require.Equal(t, "publication failed after commit", result.GetError())
	require.Equal(t, controllerui.FailureCodeInternal, result.GetFailureCode())
}

// TestMapCompactionFieldsRequireDedicatedTypedPayloads verifies stage and cancellation cannot use fork input.
func TestMapCompactionFieldsRequireDedicatedTypedPayloads(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		frame controllerui.Frame
	}{
		{name: "missing progress stage", frame: controllerui.NewFrame(controllerui.FrameCompactionProgress)},
		{name: "missing completion cancellation", frame: controllerui.NewFrame(controllerui.FrameCompaction)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			// Act through the public frame mapper with the dedicated field absent.
			_, err := mapFrame(testCase.frame)

			// Assert incomplete compaction state is rejected instead of inferred from NextInput.
			require.Error(t, err)
		})
	}

	// Arrange complete typed compaction progress and cancellation frames.
	progress := controllerui.NewFrame(controllerui.FrameCompactionProgress)
	progress.CompactionStage = mo.Some("running")
	completion := controllerui.NewFrame(controllerui.FrameCompaction)
	completion.CompactionCanceled = mo.Some(true)

	// Act through both complete frame variants.
	progressRequest, progressErr := mapFrame(progress)
	completionRequest, completionErr := mapFrame(completion)

	// Assert exact stage and Boolean cancellation reach the public contract.
	require.NoError(t, progressErr)
	require.Equal(t, "running", progressRequest.GetEvent().GetProgress().GetCompaction().GetStage())
	require.NoError(t, completionErr)
	require.True(t, completionRequest.GetEvent().GetCompleted().GetCompaction().GetCanceled())
}

// TestMapOperationFrames verifies each operation frame maps to an operation-stream envelope.
func TestMapOperationFrames(t *testing.T) {
	t.Parallel()

	// Arrange each operation frame category.
	frames := []controllerui.Frame{
		testLifecycleFrame(),
		testSimpleFrame(controllerui.FrameAuthorization, "https://auth.example"),
		testModelSelectionFrame(),
	}

	// Act by mapping every frame into the new operation stream.
	for _, frame := range frames {
		mapped, err := mapFrame(frame)

		// Assert every retained frame produces one non-empty stream envelope.
		require.NoError(t, err)
		require.NotNil(t, mapped)
		assert.NotEqual(t, uiv1.OpenRequest_Content_not_set_case, mapped.WhichContent())
	}
}

// TestMapSelectionCompletionIncludesDeliveryIssue verifies committed diagnostics cross the UI contract.
func TestMapSelectionCompletionIncludesDeliveryIssue(t *testing.T) {
	t.Parallel()
	// Arrange one committed selection frame with a complete post-commit publication failure.
	frame := testModelSelectionFrame()
	frame.SelectionIssues = []controllerui.OperationIssue{{
		Code:        controllerui.OperationIssueDeliveryFailed,
		ExtensionID: "", HandlerID: "", Message: "complete delivery cause",
	}}

	// Act by mapping the terminal result.
	mapped, err := mapFrame(frame)

	// Assert exact code and text are present in the public completion.
	require.NoError(t, err)
	issues := mapped.GetEvent().GetCompleted().GetModelSelection().GetIssues()
	require.Len(t, issues, 1)
	assert.Equal(t, uiv1.OperationIssueCode_OPERATION_ISSUE_CODE_DELIVERY_FAILED, issues[0].GetCode())
	assert.Equal(t, "complete delivery cause", issues[0].GetMessage())
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
			// Arrange a restored user image with the case-specific optional byte payload.
			inputData := test.data
			if data, present := test.data.Get(); present {
				inputData = mo.Some(bytes.Clone(data))
			}
			// Act by mapping and serializing a restored user image.
			mapped, err := mapRestoredSessionEntries([]controllerui.SessionEntry{{
				ID: "user", CreatedAt: time.Unix(1, 0), Kind: controllerui.SessionEntryUser,
				User: mo.Some(model.Message{Content: []model.InputContent{{
					Kind: model.InputContentImage, Text: mo.None[string](),
					MediaType: mo.Some("image/png"), Data: inputData,
				}}}),
				Model: mo.None[controllerui.ModelResponse](), ToolResult: mo.None[agent.ToolResult](),
				BranchSummary: mo.None[controllerui.BranchSummary](),
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
			roundTripped := new(uiv1.SessionEntry)
			require.NoError(t, proto.Unmarshal(payload, roundTripped))
			require.Len(t, roundTripped.GetUser().GetContent(), 1)
			content := roundTripped.GetUser().GetContent()[0]
			assert.Equal(t, uiv1.UserContent_Image_case, content.WhichContent())
			require.NotNil(t, content.GetImage())
			assert.True(t, content.GetImage().HasData())
			assert.Equal(t, test.expectData, content.GetImage().GetData())
		})

		t.Run("tool result "+test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange a restored tool result with the case-specific optional image bytes.
			inputData := test.data
			if data, present := test.data.Get(); present {
				inputData = mo.Some(bytes.Clone(data))
			}
			image := mo.None[tool.ResultImage]()
			if data, present := inputData.Get(); present {
				image = mo.Some(tool.ResultImage{MediaType: "image/png", Data: data})
			}
			// Act by mapping and serializing a restored tool-result image.
			mapped, err := mapRestoredSessionEntries([]controllerui.SessionEntry{{
				ID: "tool", CreatedAt: time.Unix(1, 0), Kind: controllerui.SessionEntryToolResult,
				User: mo.None[model.Message](), Model: mo.None[controllerui.ModelResponse](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "render", IsError: false,
					Contents: []tool.ResultContent{{
						Kind: tool.ResultContentImage, Text: mo.None[string](), Image: image,
					}},
				}),
				BranchSummary: mo.None[controllerui.BranchSummary](),
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
			roundTripped := new(uiv1.SessionEntry)
			require.NoError(t, proto.Unmarshal(payload, roundTripped))
			require.Len(t, roundTripped.GetToolResult().GetContents(), 1)
			content := roundTripped.GetToolResult().GetContents()[0]
			assert.Equal(t, uiv1.ToolResultContent_Image_case, content.WhichContent())
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
	summary := controllerui.BranchSummary{
		Summary: "branch context", FirstEntryID: "first", LastEntryID: "last",
		Source: session.BranchSummarySource{
			ExtensionID: mo.None[string](), Model: mo.Some(session.BranchSummaryModelSource{
				Selection: model.Selection{
					Provider:        "provider",
					Model:           "model",
					ReasoningChoice: model.ReasoningChoiceMedium,
				},
				Usage: mo.Some(session.TokenUsage{
					InputTokens: 1, OutputTokens: 7, CacheReadTokens: 3,
					CacheWriteTokens: 4, ReasoningTokens: 5, TotalTokens: 15,
				}),
			}),
		},
		EstimatedCost: mo.Some(session.EstimatedCost{
			Input: 1, Output: 2, CacheRead: 3, CacheWrite: 4, Total: 10,
		}),
	}
	entry := controllerui.SessionEntry{
		ID:               "summary",
		CreatedAt:        time.Unix(1, 0),
		Kind:             controllerui.SessionEntryBranchSummary,
		User:             mo.None[model.Message](),
		Model:            mo.None[controllerui.ModelResponse](),
		ToolResult:       mo.None[agent.ToolResult](),
		BranchSummary:    mo.Some(summary),
		ExtensionMessage: mo.None[controllerui.ExtensionMessage](),
	}

	// Act by mapping the restored entry to the generated UI contract.
	mapped, err := mapRestoredSessionEntries([]controllerui.SessionEntry{entry})

	// Assert the oneof and complete summary payload survive mapping.
	require.NoError(t, err)
	require.Len(t, mapped, 1)
	require.Equal(t, uiv1.SessionEntry_BranchSummary_case, mapped[0].WhichEntry())
	require.Equal(t, summary.Summary, mapped[0].GetBranchSummary().GetSummary())
	require.Equal(t, summary.FirstEntryID, mapped[0].GetBranchSummary().GetFirstEntryId())
	require.Equal(t, summary.LastEntryID, mapped[0].GetBranchSummary().GetLastEntryId())
	require.Equal(t, "provider", mapped[0].GetBranchSummary().GetSource().GetModel().GetProviderId())
	require.Equal(t, "model", mapped[0].GetBranchSummary().GetSource().GetModel().GetModelId())
	require.NotNil(t, mapped[0].GetBranchSummary().GetSource().GetModel().GetUsage())
	require.NotNil(t, mapped[0].GetBranchSummary().GetEstimatedCost())
}
