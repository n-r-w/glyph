package runtime

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"

	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
	uipb "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestMapSessionCommands verifies UI session commands retain their kind and optional session values.
func TestMapSessionCommands(t *testing.T) {
	t.Parallel()

	// Arrange every UI session response and its expected domain command.
	tests := []struct {
		name     string
		response *uipb.OpenResponse
		expected domainui.Command
	}{
		{name: "create", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetCreateSession(new(uipb.CreateSessionCommand))
			return value
		}(), expected: emptySessionCommand(domainui.CommandCreateSession)},
		{name: "list", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetListSessions(new(uipb.ListSessionsCommand))
			return value
		}(), expected: emptySessionCommand(domainui.CommandListSessions)},
		{name: "information", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetGetSessionInfo(new(uipb.GetSessionInfoCommand))
			return value
		}(), expected: emptySessionCommand(domainui.CommandGetSessionInfo)},
		{name: "resume", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetResumeSession(uipb.ResumeSessionCommand_builder{SessionId: new("stored")}.Build())
			return value
		}(), expected: func() domainui.Command {
			value := emptySessionCommand(domainui.CommandResumeSession)
			value.SessionID = mo.Some("stored")
			return value
		}()},
		{name: "name", response: func() *uipb.OpenResponse {
			value := new(uipb.OpenResponse)
			value.SetSetSessionName(uipb.SetSessionNameCommand_builder{Name: new("named")}.Build())
			return value
		}(), expected: func() domainui.Command {
			value := emptySessionCommand(domainui.CommandSetSessionName)
			value.SessionName = mo.Some("named")
			return value
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Act by mapping the protobuf response into a domain command.
			actual, err := mapCommand(test.response)

			// Assert the command kind and optional session value match the case.
			require.NoError(t, err)
			assert.Equal(t, test.expected, actual)
		})
	}
}

func TestMapCommandRequiresSelectedScalarPresence(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		responseIndex int
		clear         func(*uipb.OpenResponse)
	}{
		"submit text": {
			responseIndex: 0,
			clear:         func(response *uipb.OpenResponse) { response.GetSubmit().ClearText() },
		},
		"provider ID": {
			responseIndex: 4,
			clear:         func(response *uipb.OpenResponse) { response.GetSelectModel().ClearProviderId() },
		},
		"model ID": {
			responseIndex: 4,
			clear:         func(response *uipb.OpenResponse) { response.GetSelectModel().ClearModelId() },
		},
		"reasoning choice": {
			responseIndex: 5,
			clear: func(response *uipb.OpenResponse) {
				response.GetSelectReasoningChoice().ClearChoice()
			},
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			response := runtimeCommandResponses("request", "openrouter", "sonnet")[test.responseIndex]
			test.clear(response)
			_, err := mapCommand(response)
			require.Error(t, err)
		})
	}
}

// TestMapCommandPreservesPresentEmptySubmit verifies an explicit empty string remains active.
func TestMapCommandPreservesPresentEmptySubmit(t *testing.T) {
	t.Parallel()

	response := runtimeCommandResponses("", "openrouter", "sonnet")[0]
	command, err := mapCommand(response)
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

			responses := runtimeCommandResponses("request", test.providerID, test.modelID)
			_, err := mapCommand(responses[4])
			require.EqualError(t, err, "receive UI command: provider and model are required")
		})
	}
}

// TestMapInitializationPreservesWarningAndExtensionPath verifies public UI diagnostics mapping.
func TestMapInitializationPreservesWarningAndExtensionPath(t *testing.T) {
	t.Parallel()

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

	require.NoError(t, err)
	require.Len(t, mapped.GetStartupContent(), 1)
	assert.Equal(t, uipb.ContentSeverity_CONTENT_SEVERITY_WARNING, mapped.GetStartupContent()[0].GetSeverity())
	require.Len(t, mapped.GetExtensions(), 1)
	assert.Equal(t, "/plugins/tools", mapped.GetExtensions()[0].GetPath())
	require.Len(t, mapped.GetModels(), 1)
	assert.Equal(t, "openrouter", mapped.GetModels()[0].GetProviderId())
	assert.Equal(t, []uipb.ReasoningChoice{
		uipb.ReasoningChoice_REASONING_CHOICE_OFF,
		uipb.ReasoningChoice_REASONING_CHOICE_XHIGH,
	}, mapped.GetModels()[0].GetReasoning().GetChoices())
	assert.Equal(t, uipb.ReasoningChoice_REASONING_CHOICE_XHIGH, mapped.GetModelSelection().GetReasoningChoice())
}

// TestReasoningMappingsCoverEveryValue verifies the closed UI reasoning contract.
func TestReasoningMappingsCoverEveryValue(t *testing.T) {
	t.Parallel()

	values := []struct {
		domain domainui.ReasoningChoice
		proto  uipb.ReasoningChoice
	}{
		{domainui.ReasoningChoiceOff, uipb.ReasoningChoice_REASONING_CHOICE_OFF},
		{domainui.ReasoningChoiceMinimal, uipb.ReasoningChoice_REASONING_CHOICE_MINIMAL},
		{domainui.ReasoningChoiceLow, uipb.ReasoningChoice_REASONING_CHOICE_LOW},
		{domainui.ReasoningChoiceMedium, uipb.ReasoningChoice_REASONING_CHOICE_MEDIUM},
		{domainui.ReasoningChoiceHigh, uipb.ReasoningChoice_REASONING_CHOICE_HIGH},
		{domainui.ReasoningChoiceXHigh, uipb.ReasoningChoice_REASONING_CHOICE_XHIGH},
		{domainui.ReasoningChoiceMax, uipb.ReasoningChoice_REASONING_CHOICE_MAX},
	}
	for _, value := range values {
		assert.Equal(t, value.proto, mapReasoningChoice(value.domain))
		mapped, err := mapReasoningChoiceFromProto(value.proto)
		require.NoError(t, err)
		assert.Equal(t, value.domain, mapped)
	}
	_, err := mapReasoningChoiceFromProto(uipb.ReasoningChoice_REASONING_CHOICE_UNSPECIFIED)
	require.Error(t, err)
	_, err = mapReasoningChoiceFromProto(uipb.ReasoningChoice(99))
	require.Error(t, err)
}
