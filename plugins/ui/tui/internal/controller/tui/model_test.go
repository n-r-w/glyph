//nolint:exhaustruct // Tests set only fields relevant to each key or presentation event.
package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestModelEditsUnicodeSingleLineInput verifies rune-safe cursor movement and deletion.
func TestModelEditsUnicodeSingleLineInput(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "hé🙂"}))
	assert.Equal(t, []rune("hé🙂"), model.input)
	assert.Equal(t, 3, model.cursor)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	assert.Equal(t, []rune("h🙂"), model.input)
	assert.Equal(t, 1, model.cursor)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDelete}))
	assert.Equal(t, []rune("h"), model.input)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "\n界\r"}))
	assert.Equal(t, []rune("h界"), model.input)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyHome}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "前"}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnd}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "後"}))
	assert.Equal(t, "前h界後", string(model.input))
	assert.Equal(t, 4, model.cursor)
}

// TestModelSubmitsOnlyWhileIdleAndClearsAfterSuccessfulEmission verifies input gating and acknowledgement.
func TestModelSubmitsOnlyWhileIdleAndClearsAfterSuccessfulEmission(t *testing.T) {
	t.Parallel()

	var commands []presentationdomain.Command
	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: " request "}))

	next, command := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = next.(Model)
	require.NotNil(t, command)
	assert.Equal(t, " request ", string(model.input))
	assert.True(t, model.emitting)

	model = updateModel(t, model, command())
	assert.Equal(t, []presentationdomain.Command{{Kind: presentationdomain.CommandSubmit, Text: "request"}}, commands)
	assert.Empty(t, model.input)
	assert.Zero(t, model.cursor)
	assert.False(t, model.emitting)
	assert.Contains(t, model.View().Content, "user: request")

	_, emptyCommand := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	assert.Nil(t, emptyCommand)

	model = updateModel(t, model, presentationdomain.Event{Kind: presentationdomain.EventAvailability, Availability: presentationdomain.AvailabilityRunning})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "blocked"}))
	assert.Empty(t, model.input)
}

// TestModelRetainsInputAndShowsErrorWhenEmissionFails verifies failed delivery remains recoverable.
func TestModelRetainsInputAndShowsErrorWhenEmissionFails(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityIdle, func(presentationdomain.Command) error {
		return errors.New("stream closed")
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "retry me"}))
	next, command := model.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	model = next.(Model)
	model = updateModel(t, model, command())

	assert.Equal(t, "retry me", string(model.input))
	assert.False(t, model.emitting)
	assert.Contains(t, model.View().Content, "[error] Could not send command: stream closed")
}

// TestModelSelectionShortcutsRespectAuthenticationAvailability verifies authentication gates only selection.
func TestModelSelectionShortcutsRespectAuthenticationAvailability(t *testing.T) {
	t.Parallel()

	keys := []tea.Key{
		{Code: 'l', Mod: tea.ModCtrl},
		{Code: 'p', Mod: tea.ModCtrl},
		{Code: 'p', Mod: tea.ModShift | tea.ModCtrl},
		{Code: tea.KeyTab, Mod: tea.ModShift},
	}
	for _, availability := range []presentationdomain.Availability{
		presentationdomain.AvailabilityChecking,
		presentationdomain.AvailabilityAuthenticating,
	} {
		for _, key := range keys {
			model := newSelectionTestModel(t, availability, nil)
			next, command := model.Update(tea.KeyPressMsg(key))
			updated := next.(Model)
			assert.Nil(t, command)
			assert.False(t, updated.selectorOpen)
		}
	}
	for _, availability := range []presentationdomain.Availability{
		presentationdomain.AvailabilityIdle,
		presentationdomain.AvailabilityRunning,
		presentationdomain.AvailabilityAuthenticationFailed,
	} {
		for _, key := range keys {
			model := newSelectionTestModel(t, availability, nil)
			next, command := model.Update(tea.KeyPressMsg(key))
			updated := next.(Model)
			if key.Code == 'l' {
				assert.True(t, updated.selectorOpen)
				assert.Nil(t, command)
			} else {
				require.NotNil(t, command)
			}
		}
	}
}

// TestModelSingleSelectionCyclesEmitNothing verifies redundant selection commands are suppressed.
func TestModelSingleSelectionCyclesEmitNothing(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	for _, key := range []tea.Key{
		{Code: 'p', Mod: tea.ModCtrl},
		{Code: 'p', Mod: tea.ModShift | tea.ModCtrl},
		{Code: tea.KeyTab, Mod: tea.ModShift},
	} {
		model := NewModel(presentationdomain.Event{
			Kind:         presentationdomain.EventInitialization,
			Availability: presentationdomain.AvailabilityIdle,
			Models: []presentationdomain.ConfiguredModel{{
				ProviderID: "openai-codex", ModelID: "gpt",
				ReasoningLevels: []presentationdomain.ReasoningLevel{presentationdomain.ReasoningLevelHigh},
			}},
			ModelSelection: presentationdomain.ModelSelection{
				ProviderID: "openai-codex", ModelID: "gpt", ReasoningLevel: presentationdomain.ReasoningLevelHigh,
			},
		}, service.Apply, func(presentationdomain.Command) error {
			t.Fatal("redundant selection command emitted")
			return nil
		})

		_, command := model.Update(tea.KeyPressMsg(key))
		assert.Nil(t, command)
	}
}

// TestModelSelectorConfirmsAndCancelsWithoutChangingDraftOrTranscript verifies modal behavior.
func TestModelSelectorConfirmsAndCancelsWithoutChangingDraftOrTranscript(t *testing.T) {
	t.Parallel()

	var commands []presentationdomain.Command
	model := newSelectionTestModel(t, presentationdomain.AvailabilityIdle, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model.input = []rune("draft")
	model.cursor = len(model.input)
	originalTranscript := append([]presentationdomain.Line(nil), model.state.Transcript...)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'l', Mod: tea.ModCtrl}))
	assert.True(t, model.selectorOpen)
	assert.Contains(t, model.View().Content, "openai-codex / gpt")
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	assert.False(t, model.selectorOpen)
	assert.Equal(t, []presentationdomain.Command{{
		Kind: presentationdomain.CommandSelectModel, ProviderID: "openrouter", ModelID: "sonnet",
	}}, commands)
	assert.Equal(t, "draft", string(model.input))
	assert.Equal(t, originalTranscript, model.state.Transcript)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'l', Mod: tea.ModCtrl}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEscape}))
	assert.False(t, model.selectorOpen)
	assert.Len(t, commands, 1)
	assert.Equal(t, "draft", string(model.input))
	assert.Equal(t, originalTranscript, model.state.Transcript)
}

// TestModelSelectorFitsTerminalAndKeepsEveryRowReachable verifies constrained selector rendering.
func TestModelSelectorFitsTerminalAndKeepsEveryRowReachable(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	models := make([]presentationdomain.ConfiguredModel, 8)
	for index := range models {
		models[index] = presentationdomain.ConfiguredModel{
			ProviderID: "provider", ModelID: fmt.Sprintf("model-%d", index),
			ReasoningLevels: []presentationdomain.ReasoningLevel{presentationdomain.ReasoningLevelHigh},
		}
	}
	model := NewModel(presentationdomain.Event{
		Kind: presentationdomain.EventInitialization, Availability: presentationdomain.AvailabilityIdle,
		Models: models,
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID: "provider", ModelID: "model-0", ReasoningLevel: presentationdomain.ReasoningLevelHigh,
		},
	}, service.Apply, nil)
	model.height = 10
	model.input = []rune("draft")
	model.cursor = len(model.input)
	model.state.Transcript = []presentationdomain.Line{
		{Kind: presentationdomain.LineModel, Text: "first"},
		{Kind: presentationdomain.LineModel, Text: "second"},
	}
	originalTranscript := append([]presentationdomain.Line(nil), model.state.Transcript...)

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: 'l', Mod: tea.ModCtrl}))
	view := model.View().Content
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), model.height)
	assert.Contains(t, view, "> provider / model-0")
	assert.Contains(t, view, "Up/Down navigate | Enter confirm | Escape cancel")
	assert.NotContains(t, view, "provider / model-7")

	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	assert.Contains(t, model.View().Content, "> provider / model-7")
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	assert.Contains(t, model.View().Content, "> provider / model-0")
	for range 7 {
		model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	}
	view = model.View().Content
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), model.height)
	assert.Contains(t, view, "> provider / model-7")
	assert.Equal(t, "draft", string(model.input))
	assert.Equal(t, originalTranscript, model.state.Transcript)
}

// TestTypedModelCommandIsConsumedWhenOpeningSelector verifies the command does not enter transcript.
func TestTypedModelCommandIsConsumedWhenOpeningSelector(t *testing.T) {
	t.Parallel()

	model := newSelectionTestModel(t, presentationdomain.AvailabilityIdle, nil)
	originalTranscript := append([]presentationdomain.Line(nil), model.state.Transcript...)
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "/model"}))
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	assert.True(t, model.selectorOpen)
	assert.Empty(t, model.input)
	assert.Equal(t, originalTranscript, model.state.Transcript)
}

// TestModelSelectionCyclingWorksDuringRun verifies modifier data and configured wrap order.
func TestModelSelectionCyclingWorksDuringRun(t *testing.T) {
	t.Parallel()

	var commands []presentationdomain.Command
	model := newSelectionTestModel(t, presentationdomain.AvailabilityRunning, func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	})
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModCtrl}))
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{Code: 'p', Mod: tea.ModShift | tea.ModCtrl}))
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift}))

	assert.Equal(t, []presentationdomain.Command{
		{Kind: presentationdomain.CommandSelectModel, ProviderID: "openrouter", ModelID: "sonnet"},
		{Kind: presentationdomain.CommandSelectModel, ProviderID: "openrouter", ModelID: "sonnet"},
		{Kind: presentationdomain.CommandSelectReasoningLevel, ReasoningLevel: presentationdomain.ReasoningLevelHigh},
	}, commands)
	assert.Equal(t, presentationdomain.ModelSelection{
		ProviderID: "openai-codex", ModelID: "gpt", ReasoningLevel: presentationdomain.ReasoningLevelLow,
	}, model.state.ModelSelection)
	assert.Contains(t, model.View().Content, "openai-codex / gpt / low")
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventError, Text: "selection failed",
	})
	assert.Contains(t, model.View().Content, "openai-codex / gpt / low")
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelSelectionChanged,
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID: "openai-codex", ModelID: "gpt", ReasoningLevel: presentationdomain.ReasoningLevelHigh,
		},
	})
	assert.Contains(t, model.View().Content, "openai-codex / gpt / high")
}

// TestModelEmitsStopRetryAndQuitFromDocumentedKeys verifies the documented control bindings.
func TestModelEmitsStopRetryAndQuitFromDocumentedKeys(t *testing.T) {
	t.Parallel()

	var commands []presentationdomain.Command
	emit := func(command presentationdomain.Command) error {
		commands = append(commands, command)
		return nil
	}
	model := newTestModel(t, presentationdomain.AvailabilityRunning, emit)
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	model = updateModel(t, model, presentationdomain.Event{Kind: presentationdomain.EventAvailability, Availability: presentationdomain.AvailabilityAuthenticationFailed})
	model = executeCommand(t, model, tea.KeyPressMsg(tea.Key{Code: 'r', Mod: tea.ModCtrl}))

	next, command := model.Update(tea.KeyPressMsg(tea.Key{Code: 'q', Mod: tea.ModCtrl}))
	model = next.(Model)
	require.NotNil(t, command)
	message := command()
	assert.IsType(t, emissionResultMsg{}, message)
	_, quit := model.Update(message)
	require.NotNil(t, quit)
	assert.IsType(t, tea.QuitMsg{}, quit())

	assert.Equal(t, []presentationdomain.Command{
		{Kind: presentationdomain.CommandStop},
		{Kind: presentationdomain.CommandRetryAuthentication},
		{Kind: presentationdomain.CommandQuit},
	}, commands)
}

// TestModelRendersWarningAndExtensionIdentityPath verifies startup warning and path visibility.
func TestModelRendersWarningAndExtensionIdentityPath(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		Kind: presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{
			{Kind: presentationdomain.LineWarning, Text: "excluded UI optional at /plugins/ui/optional"},
			{Kind: presentationdomain.LineInformation, Text: "UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read"},
		},
		Extensions: []presentationdomain.Extension{{
			ID: "glyph-tools", Path: "/plugins/extension/glyph-tools", Tools: []string{"read"},
		}},
		Availability: presentationdomain.AvailabilityIdle,
	}, service.Apply, func(presentationdomain.Command) error { return nil })

	view := model.View().Content
	assert.Contains(t, view, "[warning] excluded UI optional at /plugins/ui/optional")
	assert.Contains(t, view, "[info] UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read")
	assert.Equal(t, 1, strings.Count(view, "glyph-tools at /plugins/extension/glyph-tools"))
}

// TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize verifies complete view composition.
func TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize(t *testing.T) {
	t.Parallel()

	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		Kind: presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{{
			Kind: presentationdomain.LineInformation,
			Text: "Glyph session initialized.",
		}},
		Availability: presentationdomain.AvailabilityIdle,
	}, service.Apply, func(presentationdomain.Command) error { return nil })
	model = updateModel(t, model, presentationdomain.Event{Kind: presentationdomain.EventInformation, Text: "Ready."})
	model = updateModel(t, model, presentationdomain.Event{Kind: presentationdomain.EventModelDelta, Position: 1, Text: "Working"})
	model = updateModel(t, model, presentationdomain.Event{Kind: presentationdomain.EventAuthorization, Text: "https://example.test/oauth"})
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 100, Height: 40})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "hello"}))

	view := model.View()
	assert.True(t, view.AltScreen)
	assert.Contains(t, view.Content, "Glyph session initialized.")
	assert.Contains(t, view.Content, "[info] Ready.")
	assert.Contains(t, view.Content, "assistant: Working")
	assert.Contains(t, view.Content, "Authorization: https://example.test/oauth")
	assert.Contains(t, view.Content, "Terminal: 100x40")
	assert.Contains(t, view.Content, "Request: hello|")
	assert.Contains(t, view.Content, "Ctrl+P next model | Shift+Ctrl+P previous model | Shift+Tab reasoning")
}

// TestModelEndDoesNotRenderDuplicateTextFromDifferentStreamPosition verifies terminal model replacement.
func TestModelEndDoesNotRenderDuplicateTextFromDifferentStreamPosition(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelDelta, Position: 0,
	})
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelDelta, Position: 1, Text: "complete answer",
	})
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventModelEnd, Position: 0,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{Kind: presentationdomain.ModelContentText, Text: "complete answer"}},
	})

	assert.Empty(t, model.state.ActiveModel)
	assert.Equal(t, 1, strings.Count(model.View().Content, "complete answer"))
}

func TestModelRendersProvisionalToolCallNameFieldsAndPrefix(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, func(presentationdomain.Command) error { return nil })
	model = updateModel(t, model, presentationdomain.Event{
		Kind: presentationdomain.EventToolCallPreview,
		ToolCall: presentationdomain.ToolCallState{
			CallID: "call-1", Name: "read", Position: 1, Provisional: true,
			Fields: []presentationdomain.ToolCallField{
				{Name: "path", Value: "file.txt", Complete: true},
				{Name: "query", Prefix: "hel"},
			},
		},
	})

	view := model.View().Content
	assert.Contains(t, view, "[tool:call] read (provisional)")
	assert.Contains(t, view, `path="file.txt"`)
	assert.Contains(t, view, "query=hel")
	assert.NotContains(t, view, `{"path"`)
}

// TestModelKeepsEditorVisibleAndShowsLatestTranscriptWithinTerminalHeight verifies viewport truncation.
func TestModelKeepsEditorVisibleAndShowsLatestTranscriptWithinTerminalHeight(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityIdle, nil)
	for _, text := range []string{"oldest", "older", "middle", "newer", "latest"} {
		model = updateModel(t, model, presentationdomain.Event{Kind: presentationdomain.EventInformation, Text: text})
	}
	model = updateModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 7})

	view := model.View().Content
	assert.LessOrEqual(t, len(strings.Split(view, "\n")), 7)
	assert.NotContains(t, view, "oldest")
	assert.Contains(t, view, "newer")
	assert.Contains(t, view, "latest")
	assert.Contains(t, view, "Status: Idle")
	assert.Contains(t, view, "Request: |")
	assert.Contains(t, view, "Ctrl+Q quit")
	assert.Len(t, model.state.Transcript, 5)
}

// TestModelRetainsTranscriptWhenReturningToIdleForSecondTurn verifies editor reuse after settlement.
func TestModelRetainsTranscriptWhenReturningToIdleForSecondTurn(t *testing.T) {
	t.Parallel()

	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	model = updateModel(t, model, presentationdomain.Event{
		Kind:                 presentationdomain.EventModelEnd,
		ModelResponseContent: []presentationdomain.ModelResponseContent{{Kind: presentationdomain.ModelContentText, Text: "first response"}},
	})
	model = updateModel(t, model, presentationdomain.Event{Kind: presentationdomain.EventAgentSettled, Text: "completed"})
	model = updateModel(t, model, presentationdomain.Event{Kind: presentationdomain.EventAvailability, Availability: presentationdomain.AvailabilityIdle})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{Text: "second request"}))

	assert.Contains(t, model.View().Content, "assistant: first response")
	assert.Contains(t, model.View().Content, "Request: second request|")
}

// TestRenderLineDistinguishesRefusal verifies refusal text has its own terminal prefix.
func TestRenderLineDistinguishesRefusal(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "[refusal] cannot help", renderLine(presentationdomain.Line{
		Kind: presentationdomain.LineRefusal,
		Text: "cannot help",
	}))
}

// newTestModel builds a model with one deterministic presentation service.
func newSelectionTestModel(t *testing.T, availability presentationdomain.Availability, emit Emit) Model {
	t.Helper()
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		Kind: presentationdomain.EventInitialization, Availability: availability,
		Models: []presentationdomain.ConfiguredModel{
			{ProviderID: "openai-codex", ModelID: "gpt", ReasoningLevels: []presentationdomain.ReasoningLevel{
				presentationdomain.ReasoningLevelLow, presentationdomain.ReasoningLevelHigh,
			}},
			{ProviderID: "openrouter", ModelID: "sonnet", ReasoningLevels: []presentationdomain.ReasoningLevel{
				presentationdomain.ReasoningLevelNone,
			}},
		},
		ModelSelection: presentationdomain.ModelSelection{
			ProviderID: "openai-codex", ModelID: "gpt", ReasoningLevel: presentationdomain.ReasoningLevelLow,
		},
	}, service.Apply, emit)
	model.state.Transcript = []presentationdomain.Line{{Kind: presentationdomain.LineModel, Text: "existing"}}
	return model
}

func newTestModel(t *testing.T, availability presentationdomain.Availability, emit Emit) Model {
	t.Helper()
	service := presentationusecase.New()
	if emit == nil {
		emit = func(presentationdomain.Command) error { return nil }
	}
	return NewModel(presentationdomain.Event{
		Kind:         presentationdomain.EventInitialization,
		Availability: availability,
	}, service.Apply, emit)
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
