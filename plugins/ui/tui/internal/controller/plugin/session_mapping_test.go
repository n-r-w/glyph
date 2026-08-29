package plugin

import (
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestRestoredTerminalFailuresRemainVisible verifies aborted and failed entries restore as visible error lines.
func TestRestoredTerminalFailuresRemainVisible(t *testing.T) {
	t.Parallel()

	// Arrange restored model entries for both terminal failure outcomes and one safe message.
	for _, outcome := range []string{"aborted", "failed"} {
		t.Run(outcome, func(t *testing.T) {
			t.Parallel()
			errorMessage := "safe terminal failure"
			entry := new(uiv1.SessionEntry)
			entry.SetId("model-entry")
			entry.SetCreatedTime(timestamppb.Now())
			entry.SetModel(uiv1.ModelResponse_builder{
				Text: new(""), Outcome: &outcome, ErrorMessage: &errorMessage,
				Provider: nil, Model: nil, ResponseId: nil, Usage: nil,
				Diagnostics: nil, Content: nil, ResponseModel: nil,
			}.Build())

			// Act by mapping the stored entry into restored transcript lines.
			lines, err := mapRestoredTranscript([]*uiv1.SessionEntry{entry})

			// Assert the terminal failure remains one visible error line with the safe text.
			require.NoError(t, err)
			require.Len(t, lines, 1)
			assert.Equal(t, presentationdomain.LineError, lines[0].Kind)
			assert.Equal(t, mo.Some(errorMessage), lines[0].Text)
		})
	}
}

// TestSessionChangedMapsOrderedRestoredTranscript verifies restored session entries retain transcript order.
func TestSessionChangedMapsOrderedRestoredTranscript(t *testing.T) {
	t.Parallel()

	// Arrange a session-changed request with user, model, tool-call, and tool-result entries.
	createdAt := timestamppb.New(time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC))
	id := "stored"
	workingDirectory := "/project"
	userText := "prior-user"
	modelText := "prior-model"
	callID := "call-1"
	toolName := "read"
	toolResultText := "tool-result"
	//nolint:exhaustruct_v5 // OpenRequest_builder sets only the active SessionChanged field.
	request := uiv1.OpenRequest_builder{SessionChanged: uiv1.SessionChanged_builder{
		Info: uiv1.SessionInfo_builder{
			Id: &id, Name: nil, WorkingDirectory: &workingDirectory, StoragePath: nil,
			CreatedTime: createdAt, UpdateTime: createdAt,
		}.Build(),
		Entries: []*uiv1.SessionEntry{
			//nolint:exhaustruct_v5 // SessionEntry_builder sets only the active User field.
			uiv1.SessionEntry_builder{
				Id: &id, CreatedTime: createdAt, Model: nil,
				User: uiv1.UserMessage_builder{Content: []*uiv1.UserContent{
					uiv1.UserContent_builder{Text: &userText}.Build(),
				}}.Build(),
			}.Build(),
			//nolint:exhaustruct_v5 // SessionEntry_builder sets only the active Model field.
			uiv1.SessionEntry_builder{
				Id: &id, CreatedTime: createdAt, User: nil,
				Model: uiv1.ModelResponse_builder{
					Text: nil, Outcome: nil, ErrorMessage: nil, Provider: nil, Model: nil,
					ResponseId: nil, Usage: nil, Diagnostics: nil, ResponseModel: nil,
					Content: []*uiv1.ModelResponseContent{
						uiv1.ModelResponseContent_builder{
							Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_TEXT), Text: &modelText, ToolCall: nil,
						}.Build(),
						uiv1.ModelResponseContent_builder{
							Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_UNSPECIFIED), Text: nil,
							ToolCall: uiv1.FinalToolCall_builder{
								CallId: &callID, Name: &toolName, Position: new(int32(1)),
								Arguments: &structpb.Struct{Fields: map[string]*structpb.Value{
									"path": structpb.NewStringValue("input.txt"),
								}},
							}.Build(),
						}.Build(),
					},
				}.Build(),
			}.Build(),
			//nolint:exhaustruct_v5 // SessionEntry_builder sets only the active ToolResult field.
			uiv1.SessionEntry_builder{
				Id: &id, CreatedTime: createdAt, User: nil, Model: nil,
				ToolResult: uiv1.ToolResult_builder{
					CallId: &callID, ToolName: &toolName, IsError: new(false),
					Contents: []*uiv1.ToolResultContent{
						uiv1.ToolResultContent_builder{Text: &toolResultText}.Build(),
					},
				}.Build(),
			}.Build(),
		},
	}.Build()}.Build()

	// Act by mapping the restored session request.
	event, err := mapRequest(request)
	// Assert the event preserves transcript order and public content.
	require.NoError(t, err)
	require.Empty(t, event.Startup)
	require.Len(t, event.RestoredTranscript, 4)
	assert.Equal(t, presentationdomain.LineUser, event.RestoredTranscript[0].Kind)
	assert.Equal(t, mo.Some(userText), event.RestoredTranscript[0].Text)
	assert.Equal(t, presentationdomain.LineModel, event.RestoredTranscript[1].Kind)
	assert.Equal(t, mo.Some(modelText), event.RestoredTranscript[1].Text)
	assert.Equal(t, presentationdomain.LineToolStatus, event.RestoredTranscript[2].Kind)
	assert.Equal(t, mo.Some(toolName), event.RestoredTranscript[2].ToolName)
	assert.Equal(t, mo.Some("arguments"), event.RestoredTranscript[2].Status)
	assert.JSONEq(t, `{"path":"input.txt"}`, event.RestoredTranscript[2].Text.MustGet())
	assert.Equal(t, presentationdomain.LineToolDone, event.RestoredTranscript[3].Kind)
	assert.Equal(t, mo.Some(toolName), event.RestoredTranscript[3].ToolName)
	assert.Equal(t, mo.Some(toolResultText), event.RestoredTranscript[3].Text)
}

// TestSessionChangedAcceptsStoredToolResultContentStates verifies valid empty and image states restore safely.
func TestSessionChangedAcceptsStoredToolResultContentStates(t *testing.T) {
	t.Parallel()

	// Arrange the stored tool-result states accepted by persistence.
	tests := []struct {
		name             string
		contents         func() ([]*uiv1.ToolResultContent, []byte)
		expectNil        bool
		expectImageData  mo.Option[[]byte]
		expectedRendered string
	}{
		{
			name: "nil contents",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				return nil, nil
			},
			expectNil: true, expectImageData: mo.None[[]byte](), expectedRendered: "",
		},
		{
			name: "non-nil empty contents",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				return []*uiv1.ToolResultContent{}, nil
			},
			expectNil: false, expectImageData: mo.None[[]byte](), expectedRendered: "",
		},
		{
			name: "present nil image bytes",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				image := new(uiv1.ToolResultImage)
				image.SetMediaType("image/png")
				image.SetData(nil)
				content := new(uiv1.ToolResultContent)
				content.SetImage(image)
				return []*uiv1.ToolResultContent{content}, nil
			},
			expectNil: false, expectImageData: mo.Some([]byte{}),
			expectedRendered: "[image image/png, 0 bytes]",
		},
		{
			name: "present non-nil empty image bytes",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				data := []byte{}
				image := new(uiv1.ToolResultImage)
				image.SetMediaType("image/png")
				image.SetData(data)
				content := new(uiv1.ToolResultContent)
				content.SetImage(image)
				return []*uiv1.ToolResultContent{content}, data
			},
			expectNil: false, expectImageData: mo.Some([]byte{}),
			expectedRendered: "[image image/png, 0 bytes]",
		},
		{
			name: "nonempty image bytes",
			contents: func() ([]*uiv1.ToolResultContent, []byte) {
				data := []byte{1, 2, 3}
				image := new(uiv1.ToolResultImage)
				image.SetMediaType("image/png")
				image.SetData(data)
				content := new(uiv1.ToolResultContent)
				content.SetImage(image)
				return []*uiv1.ToolResultContent{content}, data
			},
			expectNil: false, expectImageData: mo.Some([]byte{1, 2, 3}),
			expectedRendered: "[image image/png, 3 bytes]",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			contents, source := test.contents()
			result := uiv1.ToolResult_builder{
				CallId: new("call"), ToolName: new("render"), Contents: contents, IsError: new(false),
			}.Build()
			entry := new(uiv1.SessionEntry)
			entry.SetToolResult(result)
			request := new(uiv1.OpenRequest)
			request.SetSessionChanged(uiv1.SessionChanged_builder{
				Info: testSessionInfo(), Entries: []*uiv1.SessionEntry{entry},
			}.Build())

			// Act by mapping a complete SessionChanged request.
			event, err := mapRequest(request)

			// Assert exact slice, option, byte ownership, and rendered text state.
			require.NoError(t, err)
			require.Len(t, event.RestoredTranscript, 1)
			line := event.RestoredTranscript[0]
			require.True(t, line.Contents.IsPresent())
			mapped := line.Contents.MustGet()
			if test.expectNil {
				assert.Nil(t, mapped)
			} else {
				assert.NotNil(t, mapped)
			}
			assert.Equal(t, mo.Some(test.expectedRendered), line.Text)
			if test.expectImageData.IsPresent() {
				require.Len(t, mapped, 1)
				assert.Equal(t, mo.Some("image/png"), mapped[0].MediaType)
				assert.Equal(t, test.expectImageData, mapped[0].Data)
				if len(source) != 0 {
					source[0] = 99
					assert.Equal(t, test.expectImageData, mapped[0].Data)
				}
			}
		})
	}
}

// TestRestoredTranscriptMapsImagesDiagnosticsAndVisibleModelContent verifies complete public restoration.
func TestRestoredTranscriptMapsImagesDiagnosticsAndVisibleModelContent(t *testing.T) {
	t.Parallel()

	// Arrange user, model, and tool-result entries with every public content type.
	createdAt := timestamppb.Now()
	userText := "before"
	userImage := new(uiv1.UserContent)
	userImage.SetImage(uiv1.UserImage_builder{MediaType: new("image/png"), Data: []byte{1, 2, 3}}.Build())
	userEntry := new(uiv1.SessionEntry)
	userEntry.SetId("user-entry")
	userEntry.SetCreatedTime(createdAt)
	userEntry.SetUser(uiv1.UserMessage_builder{Content: []*uiv1.UserContent{
		uiv1.UserContent_builder{Text: &userText, Image: nil}.Build(), userImage,
	}}.Build())
	reasoning := "visible reasoning"
	refusal := "visible refusal"
	modelEntry := new(uiv1.SessionEntry)
	modelEntry.SetId("model-entry")
	modelEntry.SetCreatedTime(createdAt)
	modelEntry.SetModel(uiv1.ModelResponse_builder{
		Text: nil, Outcome: new("stop"), ErrorMessage: nil, Provider: nil, Model: nil,
		ResponseId: nil, Usage: nil, ResponseModel: nil,
		Diagnostics: []*uiv1.ModelDiagnostic{
			uiv1.ModelDiagnostic_builder{Code: new("notice"), Message: new("safe diagnostic")}.Build(),
		},
		Content: []*uiv1.ModelResponseContent{
			uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REASONING), Text: &reasoning, ToolCall: nil,
			}.Build(),
			uiv1.ModelResponseContent_builder{
				Kind: new(uiv1.ModelContentKind_MODEL_CONTENT_KIND_REFUSAL), Text: &refusal, ToolCall: nil,
			}.Build(),
		},
	}.Build())
	toolContent := new(uiv1.ToolResultContent)
	toolContent.SetImage(uiv1.ToolResultImage_builder{MediaType: new("image/webp"), Data: []byte{4, 5, 6}}.Build())
	toolEntry := new(uiv1.SessionEntry)
	toolEntry.SetId("tool-entry")
	toolEntry.SetCreatedTime(createdAt)
	toolEntry.SetToolResult(uiv1.ToolResult_builder{
		CallId: new("call"), ToolName: new("render"), Contents: []*uiv1.ToolResultContent{toolContent},
		IsError: new(false),
	}.Build())

	// Act by mapping the ordered restored transcript.
	lines, err := mapRestoredTranscript([]*uiv1.SessionEntry{userEntry, modelEntry, toolEntry})

	// Assert ordered line kinds, text, diagnostics, and image bytes.
	require.NoError(t, err)
	require.Len(t, lines, 5)
	require.True(t, lines[0].Contents.IsPresent())
	require.Equal(t, []byte{1, 2, 3}, lines[0].Contents.MustGet()[1].Data.MustGet())
	require.Equal(t, presentationdomain.LineReasoning, lines[1].Kind)
	require.Equal(t, mo.Some(reasoning), lines[1].Text)
	require.Equal(t, presentationdomain.LineRefusal, lines[2].Kind)
	require.Equal(t, mo.Some(refusal), lines[2].Text)
	require.Equal(t, presentationdomain.LineInformation, lines[3].Kind)
	require.Equal(t, mo.Some("notice: safe diagnostic"), lines[3].Text)
	require.Equal(t, presentationdomain.LineToolDone, lines[4].Kind)
	require.Equal(t, []byte{4, 5, 6}, lines[4].Contents.MustGet()[0].Data.MustGet())
	require.Equal(t, mo.Some("[image image/webp, 3 bytes]"), lines[4].Text)
}
