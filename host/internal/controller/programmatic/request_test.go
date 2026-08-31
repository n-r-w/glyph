//go:build !integration

package programmatic

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	domainsession "github.com/n-r-w/glyph/host/internal/domain/session"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapOpenRequestPreservesSessionCommands verifies every session command retains its kind and optional arguments.
func TestMapOpenRequestPreservesSessionCommands(t *testing.T) {
	t.Parallel()

	// Arrange every session request variant and its expected transport-independent command.
	tests := []struct {
		name     string
		set      func(*programmaticv1.OpenRequest)
		expected Command
	}{
		{
			name: "create", set: func(request *programmaticv1.OpenRequest) {
				request.SetCreateSession(new(programmaticv1.CreateSession))
			},
			expected: sessionCommand("create", CommandCreateSession),
		},
		{
			name: "list", set: func(request *programmaticv1.OpenRequest) {
				request.SetListSessions(new(programmaticv1.ListSessions))
			},
			expected: sessionCommand("list", CommandListSessions),
		},
		{
			name: "resume", set: func(request *programmaticv1.OpenRequest) {
				request.SetResumeSession(programmaticv1.ResumeSession_builder{SessionId: new("stored")}.Build())
			},
			expected: func() Command {
				command := sessionCommand("resume", CommandResumeSession)
				command.SessionID = mo.Some(domainsession.ID("stored"))
				return command
			}(),
		},
		{
			name: "name", set: func(request *programmaticv1.OpenRequest) {
				request.SetSetSessionName(programmaticv1.SetSessionName_builder{Name: new("")}.Build())
			},
			expected: func() Command {
				command := sessionCommand("name", CommandSetSessionName)
				command.SessionName = mo.Some("")
				return command
			}(),
		},
		{
			name: "information", set: func(request *programmaticv1.OpenRequest) {
				request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
			},
			expected: sessionCommand("information", CommandGetSessionInfo),
		},
		{
			name: "tree", set: func(request *programmaticv1.OpenRequest) {
				request.SetGetSessionTree(new(programmaticv1.GetSessionTree))
			},
			expected: sessionCommand("tree", CommandGetSessionTree),
		},
		{
			name: "navigate", set: func(request *programmaticv1.OpenRequest) {
				request.SetNavigateSessionTree(programmaticv1.NavigateSessionTree_builder{
					TargetEntryId: new(
						"entry",
					),
					SummaryMode: new(programmaticv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY),
					CustomFocus: nil,
				}.Build())
			},
			expected: func() Command {
				command := sessionCommand("navigate", CommandNavigateSessionTree)
				command.TargetEntryID = mo.Some("entry")
				return command
			}(),
		},
		{
			name: "fork", set: func(request *programmaticv1.OpenRequest) {
				request.SetForkSession(programmaticv1.ForkSession_builder{TargetEntryId: new("entry")}.Build())
			},
			expected: func() Command {
				command := sessionCommand("fork", CommandForkSession)
				command.TargetEntryID = mo.Some("entry")
				return command
			}(),
		},
		{
			name: "clone", set: func(request *programmaticv1.OpenRequest) {
				request.SetCloneSession(new(programmaticv1.CloneSession))
			},
			expected: sessionCommand("clone", CommandCloneSession),
		},
		{
			name: "label", set: func(request *programmaticv1.OpenRequest) {
				request.SetSetEntryLabel(
					programmaticv1.SetEntryLabel_builder{TargetEntryId: new("entry"), Label: new("")}.Build(),
				)
			},
			expected: func() Command {
				command := sessionCommand("label", CommandSetEntryLabel)
				command.TargetEntryID = mo.Some("entry")
				command.EntryLabel = mo.Some("")
				return command
			}(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := new(programmaticv1.OpenRequest)
			request.SetCorrelationId(test.name)
			test.set(request)

			// Act by mapping the protobuf request into the controller command.
			actual, err := mapOpenRequest(request)

			// Assert the correlation ID, command kind, and optional argument match the case.
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func sessionCommand(correlationID string, kind CommandKind) Command {
	return Command{
		CorrelationID:   correlationID,
		Kind:            kind,
		UserText:        mo.None[string](),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[domainsession.ID](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}
}

// testCommand creates a payload-free correlated command.
func testCommand(correlationID string, kind CommandKind) Command {
	return Command{
		CorrelationID:   correlationID,
		Kind:            kind,
		UserText:        mo.None[string](),
		ProviderID:      mo.None[model.ProviderID](),
		ModelID:         mo.None[model.ID](),
		ReasoningChoice: mo.None[model.ReasoningChoice](),
		SessionID:       mo.None[domainsession.ID](),
		SessionName:     mo.None[string](),
		TargetEntryID:   mo.None[string](),
		SummaryMode:     SummaryModeNoSummary,
		CustomFocus:     mo.None[string](),
		EntryLabel:      mo.None[string](),
	}
}

// TestMapOpenRequestPreservesEveryCommand verifies the complete request oneof mapping.
//
//nolint:dupl // Similar table rows intentionally prove different command payload values.
func TestMapOpenRequestPreservesEveryCommand(t *testing.T) {
	t.Parallel()

	// Arrange every supported request variant and its transport-independent command.
	tests := map[string]struct {
		request *programmaticv1.OpenRequest
		want    Command
	}{
		"missing command": {
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries:     nil,
				GetSessionStats:       nil,
				CorrelationId:         new("missing"),
				UserRequest:           nil,
				Abort:                 nil,
				GetRunState:           nil,
				GetMessages:           nil,
				GetModels:             nil,
				SelectModel:           nil,
				SelectReasoningChoice: nil,
				CreateSession:         nil,
				ListSessions:          nil,
				ResumeSession:         nil,
				SetSessionName:        nil,
				GetSessionInfo:        nil,
				GetSessionTree:        nil,
				NavigateSessionTree:   nil,
				ForkSession:           nil,
				CloneSession:          nil,
				SetEntryLabel:         nil,
			}.Build(),
			want: testCommand("missing", CommandUnspecified),
		},
		"user request": {
			//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries: nil,
				CorrelationId:     new("user"),
				UserRequest: programmaticv1.UserRequest_builder{
					Text: new("  exact text  "),
				}.Build(),
				CreateSession:       nil,
				ListSessions:        nil,
				ResumeSession:       nil,
				SetSessionName:      nil,
				GetSessionInfo:      nil,
				GetSessionTree:      nil,
				NavigateSessionTree: nil,
				ForkSession:         nil,
				CloneSession:        nil,
				SetEntryLabel:       nil,
			}.Build(),
			want: Command{
				CorrelationID:   "user",
				Kind:            CommandUserRequest,
				UserText:        mo.Some("  exact text  "),
				ProviderID:      mo.None[model.ProviderID](),
				ModelID:         mo.None[model.ID](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       mo.None[domainsession.ID](),
				SessionName:     mo.None[string](),
				TargetEntryID:   mo.None[string](),
				SummaryMode:     SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			},
		},
		"invalid user request": {
			//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active UserRequest field.
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries: nil,
				CorrelationId:     new("invalid"),
				UserRequest: programmaticv1.UserRequest_builder{
					Text: new(" \t\n"),
				}.Build(),
				CreateSession:       nil,
				ListSessions:        nil,
				ResumeSession:       nil,
				SetSessionName:      nil,
				GetSessionInfo:      nil,
				GetSessionTree:      nil,
				NavigateSessionTree: nil,
				ForkSession:         nil,
				CloneSession:        nil,
				SetEntryLabel:       nil,
			}.Build(),
			want: Command{
				CorrelationID:   "invalid",
				Kind:            CommandUserRequest,
				UserText:        mo.Some(" \t\n"),
				ProviderID:      mo.None[model.ProviderID](),
				ModelID:         mo.None[model.ID](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       mo.None[domainsession.ID](),
				SessionName:     mo.None[string](),
				TargetEntryID:   mo.None[string](),
				SummaryMode:     SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			},
		},
		"abort": {
			//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active Abort field.
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries:   nil,
				CorrelationId:       new("abort"),
				Abort:               programmaticv1.Abort_builder{}.Build(),
				CreateSession:       nil,
				ListSessions:        nil,
				ResumeSession:       nil,
				SetSessionName:      nil,
				GetSessionInfo:      nil,
				GetSessionTree:      nil,
				NavigateSessionTree: nil,
				ForkSession:         nil,
				CloneSession:        nil,
				SetEntryLabel:       nil,
			}.Build(),
			want: testCommand("abort", CommandAbort),
		},
		"get run state": {
			//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active GetRunState field.
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries:   nil,
				CorrelationId:       new("state"),
				GetRunState:         programmaticv1.GetRunState_builder{}.Build(),
				CreateSession:       nil,
				ListSessions:        nil,
				ResumeSession:       nil,
				SetSessionName:      nil,
				GetSessionInfo:      nil,
				GetSessionTree:      nil,
				NavigateSessionTree: nil,
				ForkSession:         nil,
				CloneSession:        nil,
				SetEntryLabel:       nil,
			}.Build(),
			want: testCommand("state", CommandGetRunState),
		},
		"get messages": {
			//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active GetMessages field.
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries:   nil,
				CorrelationId:       new("messages"),
				GetMessages:         programmaticv1.GetMessages_builder{}.Build(),
				CreateSession:       nil,
				ListSessions:        nil,
				ResumeSession:       nil,
				SetSessionName:      nil,
				GetSessionInfo:      nil,
				GetSessionTree:      nil,
				NavigateSessionTree: nil,
				ForkSession:         nil,
				CloneSession:        nil,
				SetEntryLabel:       nil,
			}.Build(),
			want: testCommand("messages", CommandGetMessages),
		},
		"get models": {
			//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active GetModels field.
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries:   nil,
				CorrelationId:       new("models"),
				GetModels:           programmaticv1.GetModels_builder{}.Build(),
				CreateSession:       nil,
				ListSessions:        nil,
				ResumeSession:       nil,
				SetSessionName:      nil,
				GetSessionInfo:      nil,
				GetSessionTree:      nil,
				NavigateSessionTree: nil,
				ForkSession:         nil,
				CloneSession:        nil,
				SetEntryLabel:       nil,
			}.Build(),
			want: testCommand("models", CommandGetModels),
		},
		"select model": {
			//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active SelectModel field.
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries: nil,
				CorrelationId:     new("select-model"),
				SelectModel: programmaticv1.SelectModel_builder{
					ProviderId: new("provider"),
					ModelId:    new("model"),
				}.Build(),
				CreateSession:       nil,
				ListSessions:        nil,
				ResumeSession:       nil,
				SetSessionName:      nil,
				GetSessionInfo:      nil,
				GetSessionTree:      nil,
				NavigateSessionTree: nil,
				ForkSession:         nil,
				CloneSession:        nil,
				SetEntryLabel:       nil,
			}.Build(),
			want: Command{
				CorrelationID:   "select-model",
				Kind:            CommandSelectModel,
				ProviderID:      mo.Some(model.ProviderID("provider")),
				ModelID:         mo.Some(model.ID("model")),
				UserText:        mo.None[string](),
				ReasoningChoice: mo.None[model.ReasoningChoice](),
				SessionID:       mo.None[domainsession.ID](),
				SessionName:     mo.None[string](),
				TargetEntryID:   mo.None[string](),
				SummaryMode:     SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			},
		},
		"select reasoning": {
			//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active SelectReasoningChoice field.
			request: programmaticv1.OpenRequest_builder{
				GetSessionEntries: nil,
				CorrelationId:     new("select-reasoning"),
				SelectReasoningChoice: programmaticv1.SelectReasoningChoice_builder{
					Choice: programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX.Enum(),
				}.Build(),
				CreateSession:       nil,
				ListSessions:        nil,
				ResumeSession:       nil,
				SetSessionName:      nil,
				GetSessionInfo:      nil,
				GetSessionTree:      nil,
				NavigateSessionTree: nil,
				ForkSession:         nil,
				CloneSession:        nil,
				SetEntryLabel:       nil,
			}.Build(),
			want: Command{
				CorrelationID:   "select-reasoning",
				Kind:            CommandSelectReasoningChoice,
				ReasoningChoice: mo.Some(model.ReasoningChoiceMax),
				UserText:        mo.None[string](),
				ProviderID:      mo.None[model.ProviderID](),
				ModelID:         mo.None[model.ID](),
				SessionID:       mo.None[domainsession.ID](),
				SessionName:     mo.None[string](),
				TargetEntryID:   mo.None[string](),
				SummaryMode:     SummaryModeNoSummary,
				CustomFocus:     mo.None[string](),
				EntryLabel:      mo.None[string](),
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Act by mapping the selected request variant.
			got, err := mapOpenRequest(test.request)
			// Assert the exact command and optional arguments are preserved.
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

// TestMapOpenRequestRequiresSelectedScalarPresence verifies omission is rejected at the public boundary.
func TestMapOpenRequestRequiresSelectedScalarPresence(t *testing.T) {
	t.Parallel()

	user := testOpenRequest("user")
	user.SetUserRequest(programmaticv1.UserRequest_builder{Text: nil}.Build())
	provider := testOpenRequest("provider")
	provider.SetSelectModel(programmaticv1.SelectModel_builder{
		ProviderId: nil,
		ModelId:    new("model"),
	}.Build())
	modelID := testOpenRequest("model")
	modelID.SetSelectModel(programmaticv1.SelectModel_builder{
		ProviderId: new("provider"),
		ModelId:    nil,
	}.Build())
	reasoning := testOpenRequest("reasoning")
	reasoning.SetSelectReasoningChoice(programmaticv1.SelectReasoningChoice_builder{Choice: nil}.Build())

	for name, request := range map[string]*programmaticv1.OpenRequest{
		"user text":        user,
		"provider ID":      provider,
		"model ID":         modelID,
		"reasoning choice": reasoning,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := mapOpenRequest(request)
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		})
	}
}

// TestMapOpenRequestPreservesPresentZeroScalars verifies presence is not inferred from scalar values.
func TestMapOpenRequestPreservesPresentZeroScalars(t *testing.T) {
	t.Parallel()

	userRequest := testOpenRequest("user")
	userRequest.SetUserRequest(programmaticv1.UserRequest_builder{Text: new("")}.Build())
	user, err := mapOpenRequest(userRequest)
	require.NoError(t, err)
	assert.Equal(t, mo.Some(""), user.UserText)

	modelRequest := testOpenRequest("model")
	modelRequest.SetSelectModel(programmaticv1.SelectModel_builder{
		ProviderId: new(""),
		ModelId:    new(""),
	}.Build())
	selection, err := mapOpenRequest(modelRequest)
	require.NoError(t, err)
	assert.Equal(t, mo.Some(model.ProviderID("")), selection.ProviderID)
	assert.Equal(t, mo.Some(model.ID("")), selection.ModelID)

	reasoningRequest := testOpenRequest("reasoning")
	reasoningRequest.SetSelectReasoningChoice(programmaticv1.SelectReasoningChoice_builder{
		Choice: programmaticv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED.Enum(),
	}.Build())
	reasoning, err := mapOpenRequest(reasoningRequest)
	require.NoError(t, err)
	reasoningChoice, present := reasoning.ReasoningChoice.Get()
	assert.True(t, present)
	assert.Empty(t, reasoningChoice)
}

// TestMapOpenRequestMapsReasoningChoices verifies every transport reasoning value.
func TestMapOpenRequestMapsReasoningChoices(t *testing.T) {
	t.Parallel()

	tests := map[programmaticv1.ReasoningChoice]string{
		programmaticv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED: "",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF:         "off",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_ON:          "on",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_MINIMAL:     "minimal",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_LOW:         "low",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_MEDIUM:      "medium",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH:        "high",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_XHIGH:       "xhigh",
		programmaticv1.ReasoningChoice_REASONING_CHOICE_MAX:         "max",
		programmaticv1.ReasoningChoice(99):                          "",
	}
	for choice, want := range tests {
		//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active SelectReasoningChoice field.
		request := programmaticv1.OpenRequest_builder{
			GetSessionEntries: nil,
			CorrelationId:     new(choice.String()),
			SelectReasoningChoice: programmaticv1.SelectReasoningChoice_builder{
				Choice: choice.Enum(),
			}.Build(),
			CreateSession:       nil,
			ListSessions:        nil,
			ResumeSession:       nil,
			SetSessionName:      nil,
			GetSessionInfo:      nil,
			GetSessionTree:      nil,
			NavigateSessionTree: nil,
			ForkSession:         nil,
			CloneSession:        nil,
			SetEntryLabel:       nil,
		}.Build()
		got, err := mapOpenRequest(request)
		require.NoError(t, err)
		assert.Equal(t, want, string(got.ReasoningChoice.OrEmpty()))
	}
}

// testOpenRequest builds an empty correlated request for selected-variant tests.
func testOpenRequest(correlationID string) *programmaticv1.OpenRequest {
	return programmaticv1.OpenRequest_builder{
		GetSessionEntries:     nil,
		GetSessionStats:       nil,
		CorrelationId:         new(correlationID),
		UserRequest:           nil,
		Abort:                 nil,
		GetRunState:           nil,
		GetMessages:           nil,
		GetModels:             nil,
		SelectModel:           nil,
		SelectReasoningChoice: nil,
		CreateSession:         nil,
		ListSessions:          nil,
		ResumeSession:         nil,
		SetSessionName:        nil,
		GetSessionInfo:        nil, GetSessionTree: nil, NavigateSessionTree: nil, ForkSession: nil,

		// TestMapOpenRequestRejectsTerminalFrames verifies uncorrelated and malformed frame handling.
		CloneSession: nil, SetEntryLabel: nil,
	}.Build()
}

func TestMapOpenRequestRejectsTerminalFrames(t *testing.T) {
	t.Parallel()

	//nolint:exhaustruct_v5 // programmaticv1.OpenRequest_builder sets only the active Abort field.
	request := programmaticv1.OpenRequest_builder{
		GetSessionEntries:   nil,
		Abort:               programmaticv1.Abort_builder{}.Build(),
		CreateSession:       nil,
		ListSessions:        nil,
		ResumeSession:       nil,
		SetSessionName:      nil,
		GetSessionInfo:      nil,
		GetSessionTree:      nil,
		NavigateSessionTree: nil,
		ForkSession:         nil,
		CloneSession:        nil,
		SetEntryLabel:       nil,
	}.Build()
	_, err := mapOpenRequest(request)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	_, err = mapOpenRequest(nil)
	require.Error(t, err)
	_, isStatus := status.FromError(err)
	assert.False(t, isStatus)
}
