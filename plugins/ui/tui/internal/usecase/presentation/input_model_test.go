//go:build !integration

package presentation

import (
	"testing"

	inputcontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// newSelectionTestModel builds a model with configured selections and deterministic presentation behavior.
func newSelectionTestModel(t *testing.T, availability Availability, emit func(Command) error) *Service {
	t.Helper()
	model := newTestApplication(event{
		RestoredTranscript: nil,
		Kind:               eventInitialization,
		Availability:       mo.Some(availability),
		Models: []ConfiguredModel{
			{
				ProviderID: "openai-codex",
				ModelID:    "gpt",
				Reasoning: testReasoning(
					ReasoningChoiceLow,
					ReasoningChoiceHigh,
				),
			},
			{
				ProviderID: "openrouter",
				ModelID:    "sonnet",
				Reasoning:  testReasoning(ReasoningChoiceOff),
			},
		},
		ModelSelection: mo.Some(ModelSelection{
			ProviderID:      "openai-codex",
			ModelID:         "gpt",
			ReasoningChoice: ReasoningChoiceLow,
		}),
		Startup:              nil,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	}, testMockHost(t, emit))
	model.model.state.Transcript = []Line{{
		Kind:     LineModel,
		Text:     mo.Some("existing"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]Content](),
	}}
	return model
}

// newTestModel creates an initialized model with an isolated command sender.
func newTestModel(t testing.TB, availability Availability, emit func(Command) error) *Service {
	t.Helper()
	if emit == nil {
		emit = func(Command) error { return nil }
	}
	return newTestApplication(event{
		RestoredTranscript:   nil,
		Kind:                 eventInitialization,
		Availability:         mo.Some(availability),
		Startup:              nil,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[ModelSelection](),
		SessionInfo:          mo.None[SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[SessionStatistics](),
		treeEvent:            mo.None[treeEvent](),
	}, testMockHost(t, emit))
}

// testMockHost routes test commands through the production consumer's generated mock.
func testMockHost(t testing.TB, emit func(Command) error) *MockHost {
	t.Helper()
	sender := NewMockHost(gomock.NewController(t))
	if emit == nil {
		emit = func(Command) error { return nil }
	}
	sender.EXPECT().Send(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().DoAndReturn(
		func(_ string, command Command, _ string) error { return emit(command) },
	)
	return sender
}

// testKey builds one unmodified Bubble Tea key for controller tests.
func testKey(code rune) inputcontroller.Key {
	return inputcontroller.Key{
		Code: code, Text: "", Mod: 0,
	}
}

// updateModel applies one Bubble Tea message and requires the concrete model result.
func updateModel(t *testing.T, model *Service, message any) *Service {
	t.Helper()
	next, _ := updateApplication(model, message)
	return next
}

// executeCommand applies one key and executes its emitted acknowledgement command.
func executeCommand(t *testing.T, model *Service, key inputcontroller.Key) *Service {
	t.Helper()
	next, command := updateApplication(model, key)
	model = next
	require.NotNil(t, command)
	return updateModel(t, model, command.Execute())
}

// testReasoning creates supported reasoning choices with the last choice selected by default.
func testReasoning(choices ...ReasoningChoice) ReasoningCapabilities {
	return ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}

// newTestApplication creates the real state owner with isolated output and runtime ports.
func newTestApplication(initial event, host *MockHost) *Service {
	display := NewMockDisplay(host.ctrl)
	display.EXPECT().Publish(gomock.Any()).AnyTimes()
	service := New(host, display, NewMockRuntime(host.ctrl))
	service.model = newInteraction(initial)
	service.model.projectionChanged = true
	service.publish()
	return service
}

// updateApplication applies a private transition or a decoded input at the application boundary.
func updateApplication(service *Service, message any) (*Service, inputcontroller.Work) {
	switch message := message.(type) {
	case inputcontroller.Key:
		return service, service.Key(message)
	case inputcontroller.Result:
		service.Complete(message)
	case event:
		service.model = service.model.applyEvent(message)
		service.model.projectionChanged = true
		service.publish()
	}
	return service, nil
}
