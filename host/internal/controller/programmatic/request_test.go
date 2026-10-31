//go:build !integration

package programmatic

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestMapOpenRequestMapsEveryOperationKind verifies exhaustive ControllerRequest mapping.
func TestMapOpenRequestMapsEveryOperationKind(t *testing.T) {
	t.Parallel()

	// Arrange one valid payload for each Host-prepared operation kind.
	tests := map[string]struct {
		set  func(*programmaticv1.ControllerRequest)
		kind CommandKind
	}{
		"user request": {set: func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.UserRequest)
			payload.SetContent([]*programmaticv1.UserContent{
				programmaticv1.UserContent_builder{Text: new("request"), Image: nil}.Build(),
			})
			request.SetUserRequest(payload)
		}, kind: CommandUserRequest},
		"run state": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetGetRunState(new(programmaticv1.GetRunState))
		}, kind: CommandGetRunState},
		"messages": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetGetMessages(new(programmaticv1.GetMessages))
		}, kind: CommandGetMessages},
		"models": {
			set:  func(request *programmaticv1.ControllerRequest) { request.SetGetModels(new(programmaticv1.GetModels)) },
			kind: CommandGetModels,
		},
		"select model": {set: func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.SelectModel)
			payload.SetProviderId("provider")
			payload.SetModelId("model")
			request.SetSelectModel(payload)
		}, kind: CommandSelectModel},
		"select reasoning": {set: func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.SelectReasoningChoice)
			payload.SetChoice(programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH)
			request.SetSelectReasoningChoice(payload)
		}, kind: CommandSelectReasoningChoice},
		"create session": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetCreateSession(new(programmaticv1.CreateSession))
		}, kind: CommandCreateSession},
		"list sessions": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetListSessions(new(programmaticv1.ListSessions))
		}, kind: CommandListSessions},
		"resume session": {set: func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.ResumeSession)
			payload.SetSessionId("session")
			request.SetResumeSession(payload)
		}, kind: CommandResumeSession},
		"set session name": {set: func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.SetSessionName)
			payload.SetName("name")
			request.SetSetSessionName(payload)
		}, kind: CommandSetSessionName},
		"session info": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetGetSessionInfo(new(programmaticv1.GetSessionInfo))
		}, kind: CommandGetSessionInfo},
		"session entries": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetGetSessionEntries(new(programmaticv1.GetSessionEntries))
		}, kind: CommandGetSessionEntries},
		"session stats": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetGetSessionStats(new(programmaticv1.GetSessionStats))
		}, kind: CommandGetSessionStats},
		"session tree": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetGetSessionTree(new(programmaticv1.GetSessionTree))
		}, kind: CommandGetSessionTree},
		"navigate tree": {set: func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.NavigateSessionTree)
			payload.SetTargetEntryId("entry")
			payload.SetSummaryMode(programmaticv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY)
			request.SetNavigateSessionTree(payload)
		}, kind: CommandNavigateSessionTree},
		"fork session": {set: func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.ForkSession)
			payload.SetTargetEntryId("entry")
			request.SetForkSession(payload)
		}, kind: CommandForkSession},
		"clone session": {set: func(request *programmaticv1.ControllerRequest) {
			request.SetCloneSession(new(programmaticv1.CloneSession))
		}, kind: CommandCloneSession},
		"set entry label": {set: func(request *programmaticv1.ControllerRequest) {
			payload := new(programmaticv1.SetEntryLabel)
			payload.SetTargetEntryId("entry")
			payload.SetLabel("label")
			request.SetSetEntryLabel(payload)
		}, kind: CommandSetEntryLabel},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := new(programmaticv1.OpenRequest)
			request.SetOperationId("operation")
			payload := new(programmaticv1.ControllerRequest)
			test.set(payload)
			request.SetRequest(payload)

			// Act by mapping the request envelope.
			command, err := mapOpenRequest(request)

			// Assert the operation identifier and kind.
			require.NoError(t, err)
			assert.Equal(t, "operation", command.OperationID)
			assert.Equal(t, test.kind, command.Kind)
		})
	}
}

// TestMapOpenRequestPreservesAndRejectsInvalidSessionMutationPayloads verifies bounded payload admission.
func TestMapOpenRequestPreservesAndRejectsInvalidSessionMutationPayloads(t *testing.T) {
	t.Parallel()

	// Arrange invalid values whose raw payload must remain available to domain execution.
	tests := map[string]struct {
		set       func(*programmaticv1.ControllerRequest)
		assertRaw func(*testing.T, Command)
	}{
		"whitespace session name": {
			set: func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.SetSessionName)
				payload.SetName(" \t\n ")
				request.SetSetSessionName(payload)
			},
			assertRaw: func(t *testing.T, command Command) {
				t.Helper()
				assert.Equal(t, " \t\n ", command.SessionName.OrEmpty())
			},
		},
		"whitespace custom focus": {
			set: func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.NavigateSessionTree)
				payload.SetTargetEntryId("entry")
				payload.SetSummaryMode(programmaticv1.SummaryMode_SUMMARY_MODE_SUMMARIZE_WITH_CUSTOM_PROMPT)
				payload.SetCustomFocus(" \t\n ")
				request.SetNavigateSessionTree(payload)
			},
			assertRaw: func(t *testing.T, command Command) {
				t.Helper()
				assert.Equal(t, " \t\n ", command.CustomFocus.OrEmpty())
			},
		},
		"unspecified summary mode": {
			set: func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.NavigateSessionTree)
				payload.SetTargetEntryId("entry")
				payload.SetSummaryMode(programmaticv1.SummaryMode_SUMMARY_MODE_UNSPECIFIED)
				request.SetNavigateSessionTree(payload)
			},
			assertRaw: nil,
		},
		"unknown summary mode": {
			set: func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.NavigateSessionTree)
				payload.SetTargetEntryId("entry")
				payload.SetSummaryMode(programmaticv1.SummaryMode(99))
				request.SetNavigateSessionTree(payload)
			},
			assertRaw: nil,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			request := testRequest("invalid", test.set)

			// Act by mapping without normalizing the payload.
			command, err := mapOpenRequest(request)

			// Assert raw mapping and the closed pre-acceptance rejection.
			if test.assertRaw != nil {
				test.assertRaw(t, command)
			}
			assert.False(t, command.Valid())
			code, rejected := rejectionCode(err)
			require.True(t, rejected)
			assert.Equal(t, RejectionCodeInvalidArgument, code)
		})
	}
}

// TestMapOpenRequestRejectsMalformedInput verifies malformed requests stay per-request failures.
func TestMapOpenRequestRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	// Arrange an envelope without an operation identifier or request kind.
	request := new(programmaticv1.OpenRequest)
	request.SetRequest(new(programmaticv1.ControllerRequest))

	// Act by mapping the malformed envelope.
	_, err := mapOpenRequest(request)

	// Assert the closed invalid-argument rejection.
	code, rejected := rejectionCode(err)
	require.True(t, rejected)
	assert.Equal(t, RejectionCodeInvalidArgument, code)
}

// TestRejectPreservesCategoryAndOriginalCause verifies classified preparation failures remain inspectable.
func TestRejectPreservesCategoryAndOriginalCause(t *testing.T) {
	t.Parallel()

	// Arrange one identifiable preparation failure.
	cause := errors.New("catalog lookup failed")

	// Act by classifying the rejection.
	err := Reject(RejectionCodeNotFound, cause)

	// Assert both the concrete category and original cause remain available.
	var rejection *RejectionError
	require.ErrorAs(t, err, &rejection)
	assert.Equal(t, RejectionCodeNotFound, rejection.Code())
	assert.Equal(t, cause.Error(), err.Error())
	require.ErrorIs(t, err, cause)
}

// TestMapOpenRequestMapsUserContent verifies ordered text and image blocks and the rejection of malformed input.
func TestMapOpenRequestMapsUserContent(t *testing.T) {
	t.Parallel()
	text := func(value string) *programmaticv1.UserContent {
		return programmaticv1.UserContent_builder{Text: new(value), Image: nil}.Build()
	}
	image := func(mediaType string, data []byte) *programmaticv1.UserContent {
		return programmaticv1.UserContent_builder{
			Text: nil, Image: programmaticv1.UserImage_builder{MediaType: new(mediaType), Data: data}.Build(),
		}.Build()
	}
	tests := map[string]struct {
		content []*programmaticv1.UserContent
		err     string
	}{
		"text and image": {
			content: []*programmaticv1.UserContent{
				text("look"),
				image("image/png", []byte{1, 2}),
				text("done"),
			},
			err: "",
		},
		"empty text": {
			content: []*programmaticv1.UserContent{text("")},
			err:     "programmatic user request: content 0: nonempty text is required",
		},
		"block without payload": {
			content: []*programmaticv1.UserContent{new(programmaticv1.UserContent)},
			err:     "programmatic user request: content 0: content payload is required",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one user request with the case content.
			request := testRequest("operation", func(request *programmaticv1.ControllerRequest) {
				payload := new(programmaticv1.UserRequest)
				payload.SetContent(test.content)
				request.SetUserRequest(payload)
			})

			// Act by mapping the request envelope.
			command, err := mapOpenRequest(request)

			// Assert the ordered message or the complete invalid-argument rejection.
			if test.err != "" {
				require.EqualError(t, err, test.err)
				rejection, rejected := errors.AsType[*RejectionError](err)
				require.True(t, rejected)
				assert.Equal(t, RejectionCodeInvalidArgument, rejection.Code())
				return
			}
			require.NoError(t, err)
			assert.True(t, command.Valid())
			message, present := command.UserMessage.Get()
			require.True(t, present)
			require.Len(t, message.Content, 3)
			assert.Equal(t, "look", message.Content[0].Text.OrEmpty())
			assert.Equal(t, model.InputContentImage, message.Content[1].Kind)
			assert.Equal(t, "image/png", message.Content[1].MediaType.OrEmpty())
			assert.Equal(t, []byte{1, 2}, message.Content[1].Data.OrEmpty())
			assert.Equal(t, "done", message.Content[2].Text.OrEmpty())
		})
	}
}

// TestUserRequestRejectsBlankText verifies a text block without visible text keeps its invalid-payload rejection.
func TestUserRequestRejectsBlankText(t *testing.T) {
	t.Parallel()
	// Arrange a mapped request whose only text block contains whitespace.
	command, err := mapOpenRequest(testRequest("operation", func(request *programmaticv1.ControllerRequest) {
		payload := new(programmaticv1.UserRequest)
		payload.SetContent([]*programmaticv1.UserContent{
			programmaticv1.UserContent_builder{Text: new(" \t\n "), Image: nil}.Build(),
		})
		request.SetUserRequest(payload)
	}))
	require.NoError(t, err)

	// Act by validating the command payload.
	valid := command.Valid()

	// Assert the blank text is not a valid user request.
	assert.False(t, valid)
}
