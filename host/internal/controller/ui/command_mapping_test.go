//go:build !integration

package ui

import (
	"testing"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapTreeCommandsPreservesNavigationFields verifies tree commands retain target, mode, and custom focus.
func TestMapTreeCommandsPreservesNavigationFields(t *testing.T) {
	t.Parallel()

	// Arrange a no-summary navigation command with one exact target.
	request := new(uiv1.UIRequest)
	request.SetNavigateSessionTree(uiv1.NavigateSessionTreeCommand_builder{
		TargetEntryId: new("entry"), SummaryMode: new(uiv1.SummaryMode_SUMMARY_MODE_NO_SUMMARY), CustomFocus: nil,
	}.Build())

	// Act by mapping the public UI command.
	command, err := mapCommand(operationResponse(request))

	// Assert all navigation fields retain their exact semantic values.
	require.NoError(t, err)
	require.Equal(t, CommandNavigateSessionTree, command.Kind)
	require.Equal(t, mo.Some("entry"), command.TargetEntryID)
	require.Equal(t, SummaryModeNoSummary, command.SummaryMode)
	require.True(t, command.CustomFocus.IsNone())
}

// TestMapSessionCommands verifies UI session commands retain their kind and optional session values.
func TestMapSessionCommands(t *testing.T) {
	t.Parallel()

	// Arrange every UI session response and its expected controller command.
	tests := []struct {
		name     string
		response *uiv1.UIRequest
		expected Command
	}{
		{name: "create", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetCreateSession(new(uiv1.CreateSessionCommand))
			return value
		}(), expected: newCommand(CommandCreateSession)},
		{name: "list", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetListSessions(new(uiv1.ListSessionsCommand))
			return value
		}(), expected: newCommand(CommandListSessions)},
		{name: "information", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetGetSessionInfo(new(uiv1.GetSessionInfoCommand))
			return value
		}(), expected: newCommand(CommandGetSessionInfo)},
		{name: "resume", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetResumeSession(uiv1.ResumeSessionCommand_builder{SessionId: new("stored")}.Build())
			return value
		}(), expected: func() Command {
			value := newCommand(CommandResumeSession)
			value.SessionID = mo.Some("stored")
			return value
		}()},
		{name: "name", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetSetSessionName(uiv1.SetSessionNameCommand_builder{Name: new("named")}.Build())
			return value
		}(), expected: func() Command {
			value := newCommand(CommandSetSessionName)
			value.SessionName = mo.Some("named")
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange the case-specific UI request and expected controller command.
			// Act by mapping the protobuf response into a controller command.
			actual, err := mapCommand(operationResponse(test.response))

			// Assert the command kind and optional session value match the case.
			require.NoError(t, err)
			test.expected.OperationID = "operation"
			assert.Equal(t, test.expected, actual)
		})
	}
}

// TestMapCommandRequiresSelectedScalarPresence verifies each selected request rejects an absent required scalar.
func TestMapCommandRequiresSelectedScalarPresence(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		responseIndex int
		clear         func(*uiv1.OpenResponse)
	}{
		"submit content": {
			responseIndex: 0,
			clear:         func(response *uiv1.OpenResponse) { response.GetRequest().GetSubmit().SetContent(nil) },
		},
		"provider ID": {
			responseIndex: 2,
			clear:         func(response *uiv1.OpenResponse) { response.GetRequest().GetSelectModel().ClearProviderId() },
		},
		"model ID": {
			responseIndex: 2,
			clear:         func(response *uiv1.OpenResponse) { response.GetRequest().GetSelectModel().ClearModelId() },
		},
		"reasoning choice": {
			responseIndex: 3,
			clear: func(response *uiv1.OpenResponse) {
				response.GetRequest().GetSelectReasoningChoice().ClearChoice()
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange response for mapCommand to verify map command requires selected scalar presence.

			response := runtimeCommandResponses("request", "openrouter", "sonnet")[test.responseIndex]
			test.clear(response)
			// Act by invoking mapCommand to exercise map command requires selected scalar presence.
			_, err := mapCommand(response)
			// Assert map command requires selected scalar presence.
			require.Error(t, err)
		})
	}
}

// TestMapCommandSubmitContent verifies ordered text and image blocks and the rejection of each malformed block.
func TestMapCommandSubmitContent(t *testing.T) {
	t.Parallel()
	text := func(value string) *uiv1.UserContent {
		return uiv1.UserContent_builder{Text: new(value), Image: nil}.Build()
	}
	image := func(mediaType string, data []byte) *uiv1.UserContent {
		return uiv1.UserContent_builder{
			Text: nil, Image: uiv1.UserImage_builder{MediaType: new(mediaType), Data: data}.Build(),
		}.Build()
	}
	tests := map[string]struct {
		content []*uiv1.UserContent
		err     string
	}{
		"text and image": {
			content: []*uiv1.UserContent{text("look"), image("image/png", []byte{1, 2}), text("done")}, err: "",
		},
		"empty text": {
			content: []*uiv1.UserContent{text("")},
			err:     "receive UI command: submit: content 0: nonempty text is required",
		},
		"block without payload": {
			content: []*uiv1.UserContent{new(uiv1.UserContent)},
			err:     "receive UI command: submit: content 0: content payload is required",
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange one submit request with the case content.
			response := operationResponse(uiv1.UIRequest_builder{
				Submit: uiv1.SubmitCommand_builder{Content: test.content}.Build(),
			}.Build())

			// Act by mapping the protobuf response into a controller command.
			command, err := mapCommand(response)

			// Assert the ordered message or the complete admission error.
			if test.err != "" {
				require.EqualError(t, err, test.err)
				return
			}
			require.NoError(t, err)
			message, present := command.Message.Get()
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

// TestMapCommandRejectsEmptySelectedModel verifies Protobuf validation stays at the runtime boundary.
func TestMapCommandRejectsEmptySelectedModel(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name       string
		providerID string
		modelID    string
	}{
		{name: "provider", providerID: "", modelID: "sonnet"},
		{name: "model", providerID: "openrouter", modelID: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Arrange responses for mapCommand to verify Protobuf validation stays at the runtime boundary.

			responses := runtimeCommandResponses("request", test.providerID, test.modelID)
			// Act by invoking mapCommand to exercise Protobuf validation stays at the runtime boundary.
			_, err := mapCommand(responses[2])
			// Assert Protobuf validation stays at the runtime boundary.
			require.EqualError(t, err, "receive UI command: provider and model are required")
		})
	}
}

// operationResponse wraps one UI request in its correlated stream envelope.
func operationResponse(request *uiv1.UIRequest) *uiv1.OpenResponse {
	return uiv1.OpenResponse_builder{
		OperationId: new("operation"), Request: request, Event: nil, Close: nil,
	}.Build()
}

// runtimeCommandResponses creates retained UI operation request fixtures.
func runtimeCommandResponses(text string, providerID string, modelID string) []*uiv1.OpenResponse {
	requests := []*uiv1.UIRequest{
		uiv1.UIRequest_builder{
			Submit: uiv1.SubmitCommand_builder{
				Content: []*uiv1.UserContent{uiv1.UserContent_builder{Text: new(text), Image: nil}.Build()},
			}.Build(),
		}.Build(),
		uiv1.UIRequest_builder{
			RetryAuthentication: uiv1.RetryAuthenticationCommand_builder{}.Build(),
		}.Build(),
		uiv1.UIRequest_builder{SelectModel: uiv1.SelectModelCommand_builder{
			ProviderId: new(providerID), ModelId: new(modelID),
		}.Build()}.Build(),
		uiv1.UIRequest_builder{SelectReasoningChoice: uiv1.SelectReasoningChoiceCommand_builder{
			Choice: new(uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH),
		}.Build()}.Build(),
	}
	responses := make([]*uiv1.OpenResponse, 0, len(requests))
	for _, request := range requests {
		responses = append(responses, operationResponse(request))
	}
	return responses
}

// TestReasoningMappingsCoverEveryValue verifies the closed UI reasoning contract.
func TestReasoningMappingsCoverEveryValue(t *testing.T) {
	t.Parallel()
	// Arrange values for the reasoning choice mappers to verify the closed UI reasoning contract.

	values := []struct {
		domain model.ReasoningChoice
		proto  uiv1.ReasoningChoice
	}{
		{model.ReasoningChoiceOff, uiv1.ReasoningChoice_REASONING_CHOICE_OFF},
		{model.ReasoningChoiceMinimal, uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL},
		{model.ReasoningChoiceLow, uiv1.ReasoningChoice_REASONING_CHOICE_LOW},
		{model.ReasoningChoiceMedium, uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM},
		{model.ReasoningChoiceHigh, uiv1.ReasoningChoice_REASONING_CHOICE_HIGH},
		{model.ReasoningChoiceXHigh, uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH},
		{model.ReasoningChoiceMax, uiv1.ReasoningChoice_REASONING_CHOICE_MAX},
	}
	for _, value := range values {
		// Act by invoking the reasoning choice mappers to exercise the closed UI reasoning contract.
		mapped, err := mapReasoningChoiceFromProto(value.proto)
		// Assert the closed UI reasoning contract.
		require.NoError(t, err)
		assert.Equal(t, value.domain, mapped)
	}
	_, err := mapReasoningChoiceFromProto(uiv1.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED)
	require.Error(t, err)
	_, err = mapReasoningChoiceFromProto(uiv1.ReasoningChoice(99))
	require.Error(t, err)
}
