package tui

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// newSelectionTestModel builds a model with configured selections and deterministic presentation behavior.
func newSelectionTestModel(t *testing.T, availability presentationdomain.Availability, emit Emit) Model {
	t.Helper()
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Availability:       mo.Some(availability),
		Models: []presentationdomain.ConfiguredModel{
			{
				ProviderID: "openai-codex",
				ModelID:    "gpt",
				Reasoning: testReasoning(
					presentationdomain.ReasoningChoiceLow,
					presentationdomain.ReasoningChoiceHigh,
				),
			},
			{
				ProviderID: "openrouter",
				ModelID:    "sonnet",
				Reasoning:  testReasoning(presentationdomain.ReasoningChoiceOff),
			},
		},
		ModelSelection: mo.Some(presentationdomain.ModelSelection{
			ProviderID:      "openai-codex",
			ModelID:         "gpt",
			ReasoningChoice: presentationdomain.ReasoningChoiceLow,
		}),
		Startup:              nil,
		Extensions:           nil,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
	}, service.Apply, emit)
	model.state.Transcript = []presentationdomain.Line{{
		Kind:     presentationdomain.LineModel,
		Text:     mo.Some("existing"),
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	}}
	return model
}

// BenchmarkModelVisibleBodyLines measures viewport rendering as transcript history grows.
func BenchmarkModelVisibleBodyLines(b *testing.B) {
	for _, transcriptSize := range []int{100, 10_000, 100_000} {
		b.Run(fmt.Sprintf("transcript_%d", transcriptSize), func(b *testing.B) {
			// Arrange a fixed viewport and a transcript of the requested size.
			model := newTestModel(b, presentationdomain.AvailabilityIdle, nil)
			model.width = 120
			model.height = 40
			model.state.Transcript = make([]presentationdomain.Line, transcriptSize)
			for index := range model.state.Transcript {
				model.state.Transcript[index] = presentationdomain.Line{
					Kind:     presentationdomain.LineInformation,
					ToolName: mo.None[string](),
					Status:   mo.None[string](),
					Text:     mo.Some(fmt.Sprintf("transcript line %d with representative content", index)),
					Contents: mo.None[[]presentationdomain.Content](),
				}
			}

			// Act by rendering the same viewport repeatedly.
			b.ReportAllocs()
			var lines []string
			for b.Loop() {
				lines = model.visibleBodyLines(0)
			}

			// Assert the benchmark exercised visible output.
			require.NotEmpty(b, lines)
		})
	}
}

func newTestModel(t testing.TB, availability presentationdomain.Availability, emit Emit) Model {
	t.Helper()
	service := presentationusecase.New()
	if emit == nil {
		emit = func(presentationdomain.Command) error { return nil }
	}
	return NewModel(presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventInitialization,
		Availability:         mo.Some(availability),
		Startup:              nil,
		Extensions:           nil,
		Position:             mo.None[int](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
		Text:                 mo.None[string](),
		Contents:             mo.None[[]presentationdomain.Content](),
		ErrorText:            mo.None[string](),
		ExitCode:             mo.None[int](),
		Failure:              mo.None[bool](),
		ToolCall:             mo.None[presentationdomain.ToolCallState](),
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
		TreeEvent:            mo.None[presentationdomain.TreeEvent](),
	}, service.Apply, emit)
}

// testKey builds one unmodified Bubble Tea key for controller tests.
func testKey(code rune) tea.Key {
	return tea.Key{
		Code: code, Text: "", Mod: 0, ShiftedCode: 0, BaseCode: 0, IsRepeat: false,
	}
}

// updateModel applies one Bubble Tea message and requires the concrete model result.
func updateModel(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	next, _ := model.Update(message)
	return next.(Model)
}

// executeCommand applies one key and executes its emitted acknowledgement command.
func executeCommand(t *testing.T, model Model, key tea.KeyPressMsg) Model {
	t.Helper()
	next, command := model.Update(key)
	model = next.(Model)
	require.NotNil(t, command)
	return updateModel(t, model, command())
}

func testReasoning(choices ...presentationdomain.ReasoningChoice) presentationdomain.ReasoningCapabilities {
	return presentationdomain.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
