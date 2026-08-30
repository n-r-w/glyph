package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
)

// TestModelRendersWarningAndExtensionIdentityPath verifies startup warning and path visibility.
func TestModelRendersWarningAndExtensionIdentityPath(t *testing.T) {
	t.Parallel()

	// Arrange startup warnings and extension identity paths.
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{
			{
				Kind:     presentationdomain.LineWarning,
				Text:     mo.Some("excluded UI optional at /plugins/ui/optional"),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]presentationdomain.Content](),
			},
			{
				Kind:     presentationdomain.LineInformation,
				Text:     mo.Some("UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read"),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]presentationdomain.Content](),
			},
		},
		Extensions: []presentationdomain.Extension{{
			ID:    "glyph-tools",
			Path:  "/plugins/extension/glyph-tools",
			Tools: []string{"read"},
		}},
		Availability:         mo.Some(presentationdomain.AvailabilityIdle),
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
	}, service.Apply, func(presentationdomain.Command) error { return nil })

	// Act by rendering the initialized model.
	view := model.View().Content
	// Assert the view shows each warning and extension path once.
	assert.Contains(t, view, "[warning] excluded UI optional at /plugins/ui/optional")
	assert.Contains(t, view, "[info] UI glyph-tui; extension glyph-tools at /plugins/extension/glyph-tools: read")
	assert.Equal(t, 1, strings.Count(view, "glyph-tools at /plugins/extension/glyph-tools"))
}

// TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize verifies complete view composition.
func TestModelRendersStartupTranscriptActiveOutputAuthorizationAndResize(t *testing.T) {
	t.Parallel()

	// Arrange startup content, transcript, active output, authorization, and window events.
	service := presentationusecase.New()
	model := NewModel(presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventInitialization,
		Startup: []presentationdomain.Line{{
			Kind:     presentationdomain.LineInformation,
			Text:     mo.Some("Glyph session initialized."),
			ToolName: mo.None[string](),
			Status:   mo.None[string](),
			Contents: mo.None[[]presentationdomain.Content](),
		}},
		Availability:         mo.Some(presentationdomain.AvailabilityIdle),
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
	}, service.Apply, func(presentationdomain.Command) error { return nil })
	// Act by applying lifecycle updates and resizing the terminal.
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 presentationdomain.EventInformation,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		Text:                 mo.Some("Ready."),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
	}))
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 presentationdomain.EventModelDelta,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.Some(1),
		Text:                 mo.Some("Working"),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
	}))
	model = updateModel(t, model, testEvent(testEventPayload{
		Kind:                 presentationdomain.EventAuthorization,
		Availability:         mo.None[presentationdomain.Availability](),
		Position:             mo.None[int](),
		Text:                 mo.Some("https://example.test/oauth"),
		ModelResponseContent: nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
	}))
	model = updateModel(t, model, tea.WindowSizeMsg{
		Width:  100,
		Height: 40,
	})
	model = updateModel(t, model, tea.KeyPressMsg(tea.Key{
		Text:        "hello",
		Mod:         0,
		Code:        0,
		ShiftedCode: 0,
		BaseCode:    0,
		IsRepeat:    false,
	}))

	// Assert the alternate-screen view contains each projected state and fits the new size.
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

	// Arrange streamed model text and a terminal response at another position.
	model := newTestModel(t, presentationdomain.AvailabilityRunning, nil)
	// Act by applying the terminal response after streamed text.
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(0),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
	})
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript:   nil,
		Kind:                 presentationdomain.EventModelDelta,
		Position:             mo.Some(1),
		Text:                 mo.Some("complete answer"),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
		ModelContentKind:     mo.None[presentationdomain.ModelContentKind](),
		ModelResponseContent: nil,
		ToolCallID:           mo.None[string](),
		ToolName:             mo.None[string](),
		Status:               mo.None[string](),
		Stream:               mo.None[presentationdomain.OutputStream](),
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
	})
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventModelEnd,
		Position:           mo.None[int](),
		ModelResponseContent: []presentationdomain.ModelResponseContent{{
			Kind: presentationdomain.ModelContentText,
			Text: mo.Some("complete answer"),
		}},
		Startup:           nil,
		Extensions:        nil,
		Availability:      mo.None[presentationdomain.Availability](),
		ModelContentKind:  mo.None[presentationdomain.ModelContentKind](),
		ToolCallID:        mo.None[string](),
		ToolName:          mo.None[string](),
		Status:            mo.None[string](),
		Stream:            mo.None[presentationdomain.OutputStream](),
		Text:              mo.None[string](),
		Contents:          mo.None[[]presentationdomain.Content](),
		ErrorText:         mo.None[string](),
		ExitCode:          mo.None[int](),
		Failure:           mo.None[bool](),
		ToolCall:          mo.None[presentationdomain.ToolCallState](),
		Models:            nil,
		ModelSelection:    mo.None[presentationdomain.ModelSelection](),
		SessionInfo:       mo.None[presentationdomain.SessionInfo](),
		Sessions:          nil,
		SessionStatistics: mo.None[presentationdomain.SessionStatistics](),
	})

	// Assert the model clears active fragments and renders the completed text once.
	assert.Empty(t, model.state.ActiveModel)
	assert.Equal(t, 1, strings.Count(model.View().Content, "complete answer"))
}

// TestModelRendersProvisionalToolCallNameFieldsAndPrefix verifies provisional complete and prefix fields remain
// visible.
func TestModelRendersProvisionalToolCallNameFieldsAndPrefix(t *testing.T) {
	t.Parallel()

	// Arrange a provisional tool call with complete and prefix fields.
	model := newTestModel(
		t,
		presentationdomain.AvailabilityRunning,
		func(presentationdomain.Command) error { return nil },
	)
	// Act by applying and rendering the provisional call.
	model = updateModel(t, model, presentationdomain.Event{
		RestoredTranscript: nil,
		Kind:               presentationdomain.EventToolCallPreview,
		ToolCall: mo.Some(presentationdomain.ToolCallState{
			CallID:      "call-1",
			Name:        "read",
			Position:    1,
			Provisional: true,
			Fields: []presentationdomain.ToolCallField{
				{
					Name:   "path",
					Value:  mo.Some[any]("file.txt"),
					Prefix: mo.None[string](),
				},
				{
					Name:   "query",
					Prefix: mo.Some("hel"),
					Value:  mo.None[any](),
				},
			},
			Arguments: nil,
		}),
		Startup:              nil,
		Extensions:           nil,
		Availability:         mo.None[presentationdomain.Availability](),
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
		Models:               nil,
		ModelSelection:       mo.None[presentationdomain.ModelSelection](),
		SessionInfo:          mo.None[presentationdomain.SessionInfo](),
		Sessions:             nil,
		SessionStatistics:    mo.None[presentationdomain.SessionStatistics](),
	})

	view := model.View().Content
	// Assert the view includes the call name, complete field, and prefix field.
	assert.Contains(t, view, "[tool:call] read (provisional)")
	assert.Contains(t, view, `path="file.txt"`)
	assert.Contains(t, view, "query=hel")
	assert.NotContains(t, view, `{"path"`)
}
