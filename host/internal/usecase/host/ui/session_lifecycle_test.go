package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestSessionLifecycleCommandsSendTypedFrames verifies each session command emits its typed UI frame.
func TestSessionLifecycleCommandsSendTypedFrames(t *testing.T) {
	t.Parallel()

	// Arrange create, list, resume, and information commands with expected frames.
	info := testSessionInfo("stored")
	statistics := session.Statistics{
		UserMessages: 0, ModelResponses: 0, ToolCalls: 0, ToolResults: 0, TotalMessages: 0,
		TokenUsage: mo.Some(session.TokenUsage{}), EstimatedCost: mo.None[session.EstimatedCost](), CostBreakdown: nil,
	}
	summary := session.Summary{Info: info, FirstUserText: mo.None[string](), TotalMessages: 0}
	tests := []struct {
		name          string
		command       domainui.Command
		expectedKind  domainui.FrameKind
		expectControl func(*MockSessionControl)
		assertFrame   func(*testing.T, domainui.Frame)
	}{
		{
			name:         "create",
			command:      testSessionCommand(domainui.CommandCreateSession, mo.None[string](), mo.None[string]()),
			expectedKind: domainui.FrameSessionChanged,
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Create(gomock.Any()).Return(session.Replacement{Info: info, Entries: nil}, nil)
			},
			assertFrame: func(t *testing.T, frame domainui.Frame) { assert.Equal(t, info, frame.SessionInfo.MustGet()) },
		},
		{
			name:         "list",
			command:      testSessionCommand(domainui.CommandListSessions, mo.None[string](), mo.None[string]()),
			expectedKind: domainui.FrameSessionList,
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().List(gomock.Any()).Return([]session.Summary{summary}, nil)
			},
			assertFrame: func(t *testing.T, frame domainui.Frame) {
				assert.Equal(t, []session.Summary{summary}, frame.Sessions)
			},
		},
		{
			name:         "resume",
			command:      testSessionCommand(domainui.CommandResumeSession, mo.Some("stored"), mo.None[string]()),
			expectedKind: domainui.FrameSessionChanged,
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Resume(gomock.Any(), session.ID("stored")).Return(
					session.Replacement{Info: info, Entries: nil}, nil,
				)
			},
			assertFrame: func(t *testing.T, frame domainui.Frame) { assert.Equal(t, info, frame.SessionInfo.MustGet()) },
		},
		{
			name:         "information",
			command:      testSessionCommand(domainui.CommandGetSessionInfo, mo.None[string](), mo.None[string]()),
			expectedKind: domainui.FrameSessionInformation,
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Information().Return(session.InformationSnapshot{
					Info: info, Statistics: statistics,
				})
			},
			assertFrame: func(t *testing.T, frame domainui.Frame) {
				assert.Equal(t, info, frame.SessionInfo.MustGet())
				actual, present := frame.SessionStatistics.Get()
				assert.True(t, present)
				assert.Equal(t, statistics, actual)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			control := NewMockSessionControl(controller)
			test.expectControl(control)
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				assert.Equal(t, test.expectedKind, frame.Kind)
				test.assertFrame(t, frame)
				return nil
			})
			// Act by applying the session command.
			handled, err := NewSession(
				channel,
				nil,
				nil,
				nil,
				control,
				nil,
			).applySessionCommand(t.Context(), test.command)
			// Assert the command is handled and sends the expected typed frame.
			require.NoError(t, err)
			assert.True(t, handled)
		})
	}
}

// TestSessionChangedReportsInvalidStoredModelProjection verifies invalid restored content stops frame delivery.
func TestSessionChangedReportsInvalidStoredModelProjection(t *testing.T) {
	t.Parallel()

	// Arrange a restored refusal that illegally contains provider context.
	channel := NewMockChannel(gomock.NewController(t))
	channel.EXPECT().Send(gomock.Any()).Return(nil).AnyTimes()
	response := model.Response{
		Content: []model.Content{{
			Kind: model.ContentRefusal, Text: mo.Some("refusal"), Final: true,
			ProviderContext: mo.Some(model.ProviderContext{
				Source: model.ProviderContextSource{
					ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.None[string](),
				},
				Payload: []byte("opaque"),
			}),
			ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}

	// Act by preparing and sending the SessionChanged frame.
	err := NewSession(channel, nil, nil, nil, nil, nil).sendSessionChanged(session.Replacement{
		Info: testSessionInfo("stored"),
		Entries: []session.Entry{
			{
				ParentID:      mo.None[string](),
				ID:            "model",
				CreatedAt:     time.Unix(1, 0),
				Information:   mo.None[session.Information](),
				User:          mo.None[session.UserMessage](),
				Model:         mo.Some(response),
				ToolResult:    mo.None[session.ToolResult](),
				Extension:     mo.None[session.ExtensionEnvelope](),
				EstimatedCost: mo.None[session.EstimatedCost](),
				BranchSummary: mo.None[session.
					BranchSummaryEntry](),
			},
		},
	})

	// Assert mapping fails before a partial frame can be delivered.
	require.Error(t, err)
}

// TestSessionReplacementFrameUsesOneCommittedSnapshot verifies an overlapping replacement cannot mix session identity
// and entries.
func TestSessionReplacementFrameUsesOneCommittedSnapshot(t *testing.T) {
	t.Parallel()

	// Arrange overlapping session replacements with different committed snapshots.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	control := NewMockSessionControl(controller)
	infoA := testSessionInfo("session-a")
	infoB := testSessionInfo("session-b")
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	entryB := session.Entry{
		ParentID:      mo.None[string](),
		ID:            "entry-b",
		CreatedAt:     createdAt,
		Information:   mo.None[session.Information](),
		User:          mo.Some(model.TextMessage("session-b-text")),
		Model:         mo.None[session.ModelResponse](),
		ToolResult:    mo.None[session.ToolResult](),
		Extension:     mo.None[session.ExtensionEnvelope](),
		EstimatedCost: mo.None[session.EstimatedCost](),
		BranchSummary: mo.None[session.
			BranchSummaryEntry](),
	}
	firstReplacementReady := make(chan struct{})
	releaseFirstReplacement := make(chan struct{})
	control.EXPECT().Create(gomock.Any()).DoAndReturn(func(context.Context) (session.Replacement, error) {
		close(firstReplacementReady)
		<-releaseFirstReplacement
		return session.Replacement{Info: infoA, Entries: nil}, nil
	})
	control.EXPECT().
		Create(gomock.Any()).
		Return(session.Replacement{Info: infoB, Entries: []session.Entry{entryB}}, nil)
	frameSent := make(chan domainui.Frame, 1)
	channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
		frameSent <- frame
		return nil
	})
	// Act by blocking the first replacement while a second replacement completes.
	done := make(chan error, 1)
	go func() {
		_, err := NewSession(channel, nil, nil, nil, control, nil).applySessionCommand(
			t.Context(), testSessionCommand(domainui.CommandCreateSession, mo.None[string](), mo.None[string]()),
		)
		done <- err
	}()
	<-firstReplacementReady
	_, err := control.Create(t.Context())
	require.NoError(t, err)
	close(releaseFirstReplacement)
	require.NoError(t, <-done)
	frame := <-frameSent
	// Assert the emitted frame contains only the first operation committed snapshot.
	require.Equal(t, infoA, frame.SessionInfo.MustGet())
	require.Empty(t, frame.SessionEntries)
}

// TestSessionChangedFrameProjectsCompletePublicContentWithoutPrivateData verifies restored UI frames exclude private
// data.
func TestSessionChangedFrameProjectsCompletePublicContentWithoutPrivateData(t *testing.T) {
	t.Parallel()

	// Arrange public user, model, and tool content plus a private extension entry.
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	call := model.ToolCall{ID: "call", Name: "read", Arguments: map[string]any{"path": "input.txt"}}
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentReasoning, Text: mo.Some("visible reasoning"), Final: true,
				ProviderContext: mo.Some(model.ProviderContext{
					Source: model.ProviderContextSource{
						ProviderID: "provider", API: "responses", Model: "model", CompatibilityKey: mo.Some("key"),
					},
					Payload: []byte{1, 2, 3},
				}),
				ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentRefusal, Text: mo.Some("visible refusal"), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
			},
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(call),
			},
		},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.Some(model.ID("response-model")), ResponseID: mo.Some("response-id"),
		Usage: mo.Some(model.Usage{}), Diagnostics: []model.Diagnostic{{Code: "notice", Message: "safe diagnostic"}},
	}
	result := agent.ToolResult{
		CallID: call.ID, ToolName: call.Name, IsError: false,
		Contents: []tool.ResultContent{
			{Kind: tool.ResultContentText, Text: mo.Some("result"), Image: mo.None[tool.ResultImage]()},
			{
				Kind:  tool.ResultContentImage,
				Text:  mo.None[string](),
				Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{7, 8, 9}}),
			},
		},
	}
	user := model.Message{Content: []model.InputContent{
		{Kind: model.InputContentText, Text: mo.Some("before"), MediaType: mo.None[string](), Data: mo.None[[]byte]()},
		{
			Kind:      model.InputContentImage,
			Text:      mo.None[string](),
			MediaType: mo.Some("image/png"),
			Data:      mo.Some([]byte{4, 5, 6}),
		},
	}}
	// Act by creating the confirmed replacement frame.
	frame, err := sessionChangedFrame(testSessionInfo("stored"), []session.Entry{
		{
			ParentID:      mo.None[string](),
			ID:            "user-entry",
			CreatedAt:     createdAt,
			Information:   mo.None[session.Information](),
			User:          mo.Some(user),
			Model:         mo.None[session.ModelResponse](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension:     mo.None[session.ExtensionEnvelope](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			BranchSummary: mo.None[session.
				BranchSummaryEntry](),
		},
		{
			ParentID:      mo.None[string](),
			ID:            "model-entry",
			CreatedAt:     createdAt.Add(time.Second),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.Some(response),
			ToolResult:    mo.None[session.ToolResult](),
			Extension:     mo.None[session.ExtensionEnvelope](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			BranchSummary: mo.None[session.
				BranchSummaryEntry](),
		},
		{
			ParentID:      mo.None[string](),
			ID:            "tool-entry",
			CreatedAt:     createdAt.Add(2 * time.Second),
			Information:   mo.None[session.Information](),
			User:          mo.None[session.UserMessage](),
			Model:         mo.None[session.ModelResponse](),
			ToolResult:    mo.Some(result),
			Extension:     mo.None[session.ExtensionEnvelope](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			BranchSummary: mo.None[session.
				BranchSummaryEntry](),
		},
		{
			ParentID: mo.None[string](), ID: "extension-entry", CreatedAt: createdAt.Add(3 * time.Second),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
			Extension: mo.Some(session.ExtensionEnvelope{
				ExtensionID: "example", EntryType: "private", Data: []byte(`{"private":true}`),
			}),
			EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.
					BranchSummaryEntry](),
		},
	})

	// Assert public content remains ordered and private data is excluded.
	require.NoError(t, err)
	require.Len(t, frame.SessionEntries, 3)
	require.True(t, frame.SessionEntries[0].User.IsPresent())
	require.Equal(t, user, frame.SessionEntries[0].User.MustGet())
	publicResponse := frame.SessionEntries[1].Model.MustGet()
	require.Equal(t, mo.Some("tool_use"), publicResponse.Outcome)
	require.Equal(t, mo.Some("response-id"), publicResponse.ResponseID)
	require.True(t, publicResponse.Usage.IsPresent())
	require.Equal(
		t,
		[]domainui.ModelDiagnostic{{Code: "notice", Message: "safe diagnostic"}},
		publicResponse.Diagnostics,
	)
	require.Len(t, publicResponse.Content, 3)
	require.Equal(t, "visible reasoning", publicResponse.Content[0].Text)
	require.Equal(t, "visible refusal", publicResponse.Content[1].Text)
	require.Equal(t, call.ID, publicResponse.Content[2].ToolCall.MustGet().CallID)
	require.Equal(t, 2, publicResponse.Content[2].ToolCall.MustGet().Position)
	require.Equal(t, domainui.SessionEntryToolResult, frame.SessionEntries[2].Kind)
	require.Equal(t, result, frame.SessionEntries[2].ToolResult.MustGet())
}

// TestSessionLifecycleRejectionsPreserveInformationContext verifies Host UI exposes operation and error details.
func TestSessionLifecycleRejectionsPreserveInformationContext(t *testing.T) {
	t.Parallel()

	// Arrange session command failures and their exact public information text.
	tests := []struct {
		name          string
		command       domainui.Command
		expectedText  string
		expectControl func(*MockSessionControl)
	}{
		{
			name:         "resume requires id",
			command:      testSessionCommand(domainui.CommandResumeSession, mo.None[string](), mo.None[string]()),
			expectedText: "A session ID is required.", expectControl: func(*MockSessionControl) {},
		},
		{
			name:         "name requires value",
			command:      testSessionCommand(domainui.CommandSetSessionName, mo.None[string](), mo.None[string]()),
			expectedText: "A session name is required.", expectControl: func(*MockSessionControl) {},
		},
		{
			name:         "create busy",
			command:      testSessionCommand(domainui.CommandCreateSession, mo.None[string](), mo.None[string]()),
			expectedText: "Session replacement is unavailable: another operation is active",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Create(gomock.Any()).Return(session.Replacement{}, session.ErrBusy)
			},
		},
		{
			name:         "create internal failure",
			command:      testSessionCommand(domainui.CommandCreateSession, mo.None[string](), mo.None[string]()),
			expectedText: "Session replacement is unavailable: create session unique failure",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().Create(gomock.Any()).Return(
					session.Replacement{}, errors.New("create session unique failure"),
				)
			},
		},
		{
			name:         "resume not found",
			command:      testSessionCommand(domainui.CommandResumeSession, mo.Some("missing"), mo.None[string]()),
			expectedText: "Session replacement is unavailable: file does not exist",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().
					Resume(gomock.Any(), session.ID("missing")).
					Return(session.Replacement{}, os.ErrNotExist)
			},
		},
		{
			name:         "resume persistence unavailable",
			command:      testSessionCommand(domainui.CommandResumeSession, mo.Some("stored"), mo.None[string]()),
			expectedText: "Session replacement is unavailable: session persistence failed",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().
					Resume(gomock.Any(), session.ID("stored")).
					Return(session.Replacement{}, session.ErrPersistenceUnavailable)
			},
		},
		{
			name:         "list failure",
			command:      testSessionCommand(domainui.CommandListSessions, mo.None[string](), mo.None[string]()),
			expectedText: "Sessions are unavailable: sensitive storage failure",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().List(gomock.Any()).Return(nil, errors.New("sensitive storage failure"))
			},
		},
		{
			name: "persistence unavailable",
			command: testSessionCommand(
				domainui.CommandSetSessionName,
				mo.None[string](),
				mo.Some("secret session name"),
			),
			expectedText: "Session naming is unavailable: session persistence failed: " +
				"/secret/path provider-context extension-json disk failed",
			expectControl: func(control *MockSessionControl) {
				control.EXPECT().SetName(gomock.Any(), "secret session name").Return(
					session.Info{},
					fmt.Errorf(
						"%w: /secret/path provider-context extension-json disk failed",
						session.ErrPersistenceUnavailable,
					),
				)
			},
		},
	}

	// Act by applying every session command through the Host UI session.
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			controller := gomock.NewController(t)
			channel := NewMockChannel(controller)
			control := NewMockSessionControl(controller)
			test.expectControl(control)
			channel.EXPECT().Send(gomock.Any()).DoAndReturn(func(frame domainui.Frame) error {
				// Assert each frame contains the operation context and original error.
				assert.Equal(t, domainui.FrameInformation, frame.Kind)
				assert.Equal(t, test.expectedText, frame.Text.MustGet())
				return nil
			})
			handled, err := NewSession(
				channel,
				nil,
				nil,
				nil,
				control,
				nil,
			).applySessionCommand(t.Context(), test.command)
			require.NoError(t, err)
			assert.True(t, handled)
		})
	}
}

// TestSessionNameAndQueriesRemainAvailableDuringActiveRun verifies session queries include statistics without stopping
// a run.
func TestSessionNameAndQueriesRemainAvailableDuringActiveRun(t *testing.T) {
	t.Parallel()

	// Arrange busy replacements and successful name, list, information, and statistics queries.
	controller := gomock.NewController(t)
	channel := NewMockChannel(controller)
	control := NewMockSessionControl(controller)
	info := testSessionInfo("active")
	control.EXPECT().Create(gomock.Any()).Return(session.Replacement{}, session.ErrBusy)
	control.EXPECT().Resume(gomock.Any(), session.ID("stored")).Return(session.Replacement{}, session.ErrBusy)
	control.EXPECT().SetName(gomock.Any(), "renamed").Return(info, nil)
	control.EXPECT().List(gomock.Any()).Return([]session.Summary{}, nil)
	control.EXPECT().Information().Return(session.InformationSnapshot{
		Info: info,
		Statistics: session.Statistics{
			UserMessages:   0,
			ModelResponses: 0,
			ToolCalls:      0,
			ToolResults:    0,
			TotalMessages:  0,
			TokenUsage: mo.Some(
				session.TokenUsage{},
			),
			EstimatedCost: mo.None[session.EstimatedCost](),
			CostBreakdown: nil,
		},
	}).Times(2)
	channel.EXPECT().Send(gomock.Any()).Times(5)
	usecase := NewSession(channel, nil, nil, nil, control, nil)
	cancel := func() {}
	commands := []domainui.Command{
		testSessionCommand(domainui.CommandCreateSession, mo.None[string](), mo.None[string]()),
		testSessionCommand(domainui.CommandResumeSession, mo.Some("stored"), mo.None[string]()),
		testSessionCommand(domainui.CommandSetSessionName, mo.None[string](), mo.Some("renamed")),
		testSessionCommand(domainui.CommandListSessions, mo.None[string](), mo.None[string]()),
		testSessionCommand(domainui.CommandGetSessionInfo, mo.None[string](), mo.None[string]()),
	}
	// Act by applying all session commands while run availability is active.
	for _, command := range commands {
		availability, activeCancel, activeKind, err := usecase.applyCommand(
			t.Context(), domainui.AvailabilityRunning, cancel, operationRun, command, make(chan operationResult),
		)
		// Assert each command preserves active run state and remains handled.
		require.NoError(t, err)
		assert.Equal(t, domainui.AvailabilityRunning, availability)
		assert.Equal(t, operationRun, activeKind)
		assert.NotNil(t, activeCancel)
	}
}

func testSessionCommand(kind domainui.CommandKind, id, name mo.Option[string]) domainui.Command {
	return domainui.Command{
		Kind:            kind,
		Text:            mo.None[string](),
		ProviderID:      mo.None[string](),
		ModelID:         mo.None[string](),
		ReasoningChoice: mo.None[domainui.ReasoningChoice](),
		SessionID:       id,
		SessionName:     name,
		TargetEntryID:   mo.None[string](),
		SummaryMode:     domainui.SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}
}

func testSessionInfo(id session.ID) session.Info {
	return session.Info{
		ID: id, Name: mo.Some("named"), WorkingDirectory: "/project", StoragePath: mo.Some("/sessions/stored.jsonl"),
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
	}
}
