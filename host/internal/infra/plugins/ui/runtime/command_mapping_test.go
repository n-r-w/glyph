//go:build !integration

package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
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
	require.Equal(t, domainui.CommandNavigateSessionTree, command.Kind)
	require.Equal(t, mo.Some("entry"), command.TargetEntryID)
	require.Equal(t, domainui.SummaryModeNoSummary, command.SummaryMode)
	require.True(t, command.CustomFocus.IsNone())
}

// TestMapSessionCommands verifies UI session commands retain their kind and optional session values.
func TestMapSessionCommands(t *testing.T) {
	t.Parallel()

	// Arrange every UI session response and its expected domain command.
	tests := []struct {
		name     string
		response *uiv1.UIRequest
		expected domainui.Command
	}{
		{name: "create", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetCreateSession(new(uiv1.CreateSessionCommand))
			return value
		}(), expected: newCommand(domainui.CommandCreateSession, mo.None[string]())},
		{name: "list", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetListSessions(new(uiv1.ListSessionsCommand))
			return value
		}(), expected: newCommand(domainui.CommandListSessions, mo.None[string]())},
		{name: "information", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetGetSessionInfo(new(uiv1.GetSessionInfoCommand))
			return value
		}(), expected: newCommand(domainui.CommandGetSessionInfo, mo.None[string]())},
		{name: "resume", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetResumeSession(uiv1.ResumeSessionCommand_builder{SessionId: new("stored")}.Build())
			return value
		}(), expected: func() domainui.Command {
			value := newCommand(domainui.CommandResumeSession, mo.None[string]())
			value.SessionID = mo.Some("stored")
			return value
		}()},
		{name: "name", response: func() *uiv1.UIRequest {
			value := new(uiv1.UIRequest)
			value.SetSetSessionName(uiv1.SetSessionNameCommand_builder{Name: new("named")}.Build())
			return value
		}(), expected: func() domainui.Command {
			value := newCommand(domainui.CommandSetSessionName, mo.None[string]())
			value.SessionName = mo.Some("named")
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange the case-specific UI request and expected domain command.
			// Act by mapping the protobuf response into a domain command.
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
		"submit text": {
			responseIndex: 0,
			clear:         func(response *uiv1.OpenResponse) { response.GetRequest().GetSubmit().ClearText() },
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

// TestMapCommandPreservesPresentEmptySubmit verifies an explicit empty string remains active.
func TestMapCommandPreservesPresentEmptySubmit(t *testing.T) {
	t.Parallel()
	// Arrange response for mapCommand to verify an explicit empty string remains active.

	response := runtimeCommandResponses("", "openrouter", "sonnet")[0]
	// Act by invoking mapCommand to exercise an explicit empty string remains active.
	command, err := mapCommand(response)
	// Assert an explicit empty string remains active.
	require.NoError(t, err)
	assert.Equal(t, mo.Some(""), command.Text)
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
		uiv1.UIRequest_builder{Submit: uiv1.SubmitCommand_builder{Text: new(text)}.Build()}.Build(),
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

// TestMapInitializationPreservesWarningAndExtensionPath verifies public UI diagnostics mapping.
func TestMapInitializationPreservesWarningAndExtensionPath(t *testing.T) {
	t.Parallel()
	// Arrange the inline payload for mapInitialization to verify public UI diagnostics mapping.

	// Act by invoking mapInitialization to exercise public UI diagnostics mapping.
	mapped, err := mapInitialization(domainui.Initialization{
		SelectedUIID: "ui",
		StartupContent: []domainui.StartupContent{{
			Severity: domainui.ContentSeverityWarning,
			Text:     "excluded optional UI",
		}},
		Extensions: []domainui.ExtensionAvailability{{
			PluginID: "tools",
			Path:     "/plugins/tools",
			Tools:    []string{"read"},
		}},
		Availability: domainui.AvailabilityCheckingAuthentication,
		Models: []domainui.ConfiguredModel{{
			ProviderID: "openrouter",
			ModelID:    "sonnet",
			Reasoning:  testUIReasoningCapabilities(domainui.ReasoningChoiceOff, domainui.ReasoningChoiceXHigh),
		}},
		ModelSelection: mo.Some(domainui.ModelSelection{
			ProviderID:      "openrouter",
			ModelID:         "sonnet",
			ReasoningChoice: domainui.ReasoningChoiceXHigh,
		}),
		SessionInfo: session.Info{},
	})

	// Assert public UI diagnostics mapping.
	require.NoError(t, err)
	require.Len(t, mapped.GetStartupContent(), 1)
	assert.Equal(t, uiv1.ContentSeverity_CONTENT_SEVERITY_WARNING, mapped.GetStartupContent()[0].GetSeverity())
	require.Len(t, mapped.GetExtensions(), 1)
	assert.Equal(t, "/plugins/tools", mapped.GetExtensions()[0].GetPath())
	require.Len(t, mapped.GetModels(), 1)
	assert.Equal(t, "openrouter", mapped.GetModels()[0].GetProviderId())
	assert.Equal(t, []uiv1.ReasoningChoice{
		uiv1.ReasoningChoice_REASONING_CHOICE_OFF,
		uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH,
	}, mapped.GetModels()[0].GetReasoning().GetChoices())
	assert.Equal(t, uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH, mapped.GetModelSelection().GetReasoningChoice())
}

// TestReasoningMappingsCoverEveryValue verifies the closed UI reasoning contract.
func TestReasoningMappingsCoverEveryValue(t *testing.T) {
	t.Parallel()
	// Arrange values for the reasoning choice mappers to verify the closed UI reasoning contract.

	values := []struct {
		domain domainui.ReasoningChoice
		proto  uiv1.ReasoningChoice
	}{
		{domainui.ReasoningChoiceOff, uiv1.ReasoningChoice_REASONING_CHOICE_OFF},
		{domainui.ReasoningChoiceMinimal, uiv1.ReasoningChoice_REASONING_CHOICE_MINIMAL},
		{domainui.ReasoningChoiceLow, uiv1.ReasoningChoice_REASONING_CHOICE_LOW},
		{domainui.ReasoningChoiceMedium, uiv1.ReasoningChoice_REASONING_CHOICE_MEDIUM},
		{domainui.ReasoningChoiceHigh, uiv1.ReasoningChoice_REASONING_CHOICE_HIGH},
		{domainui.ReasoningChoiceXHigh, uiv1.ReasoningChoice_REASONING_CHOICE_XHIGH},
		{domainui.ReasoningChoiceMax, uiv1.ReasoningChoice_REASONING_CHOICE_MAX},
	}
	for _, value := range values {
		// Act by invoking the reasoning choice mappers to exercise the closed UI reasoning contract.
		assert.Equal(t, value.proto, mapReasoningChoice(value.domain))
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
