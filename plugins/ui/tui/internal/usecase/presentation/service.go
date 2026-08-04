// Package presentation projects provider-neutral Host events into TUI state.
//
//nolint:exhaustruct // Projection lines intentionally set only fields used by their line kind.
package presentation

import presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"

// Service applies Host presentation events without owning authoritative history.
type Service struct{}

// New creates a presentation projection service.
func New() *Service {
	return &Service{}
}

// Apply returns an updated presentation projection.
//
//nolint:gocyclo // The explicit flat switch mirrors the finite presentation event kinds.
func (*Service) Apply(state presentationdomain.State, event presentationdomain.Event) presentationdomain.State {
	state = cloneState(state)
	if state.ActiveModel == nil {
		state.ActiveModel = make(map[int]string)
	}
	if state.ActiveTools == nil {
		state.ActiveTools = make(map[string]string)
	}

	switch event.Kind {
	case presentationdomain.EventInitialization:
		state.Availability = event.Availability
		state.Startup = append(state.Startup, event.Startup...)
	case presentationdomain.EventUserSubmitted:
		state.Transcript = append(state.Transcript, presentationdomain.Line{
			Kind: presentationdomain.LineUser,
			Text: event.Text,
		})
	case presentationdomain.EventAvailability:
		state.Availability = event.Availability
	case presentationdomain.EventTurnStarted:
		state.Settled = false
	case presentationdomain.EventModelDelta:
		state.ActiveModel[event.Position] += event.Text
	case presentationdomain.EventModelEnd:
		if event.Text != "" {
			state.Transcript = append(state.Transcript, presentationdomain.Line{
				Kind: presentationdomain.LineModel,
				Text: event.Text,
			})
		}
		clear(state.ActiveModel)
	case presentationdomain.EventToolStarted:
		if event.ToolCallID != "" {
			state.ActiveTools[event.ToolCallID] = event.ToolName
		}
		state.Transcript = append(state.Transcript, presentationdomain.Line{
			Kind:     presentationdomain.LineToolStatus,
			ToolName: event.ToolName,
			Status:   event.Status,
			Text:     event.Text,
		})
	case presentationdomain.EventToolProgress:
		state.Transcript = append(state.Transcript, presentationdomain.Line{
			Kind:     presentationdomain.LineToolStatus,
			ToolName: toolName(state, event),
			Status:   event.Status,
			Text:     event.Text,
		})
	case presentationdomain.EventToolOutput:
		kind := presentationdomain.LineToolStdout
		if event.Stream == presentationdomain.OutputStderr {
			kind = presentationdomain.LineToolStderr
		}
		state.Transcript = append(state.Transcript, presentationdomain.Line{
			Kind:     kind,
			ToolName: toolName(state, event),
			Text:     event.Text,
		})
	case presentationdomain.EventToolEnded:
		kind := presentationdomain.LineToolDone
		if event.Failure {
			kind = presentationdomain.LineToolError
		}
		state.Transcript = append(state.Transcript, presentationdomain.Line{
			Kind:     kind,
			ToolName: event.ToolName,
			Status:   event.Status,
		})
	case presentationdomain.EventToolResult:
		kind := presentationdomain.LineToolDone
		if event.Failure || event.ExitCode != 0 {
			kind = presentationdomain.LineToolError
		}
		state.Transcript = append(state.Transcript, presentationdomain.Line{
			Kind:     kind,
			ToolName: toolName(state, event),
			Text:     event.Text,
		})
		delete(state.ActiveTools, event.ToolCallID)
	case presentationdomain.EventTurnEnded:
	case presentationdomain.EventAgentSettled:
		state.Settled = true
	case presentationdomain.EventAuthorization:
		state.AuthorizationURL = event.Text
	case presentationdomain.EventInformation:
		state.Transcript = append(state.Transcript, presentationdomain.Line{
			Kind: presentationdomain.LineInformation,
			Text: event.Text,
		})
	case presentationdomain.EventError:
		if event.Availability != presentationdomain.AvailabilityUnspecified {
			state.Availability = event.Availability
		}
		state.Transcript = append(state.Transcript, presentationdomain.Line{
			Kind: presentationdomain.LineError,
			Text: event.Text,
		})
	case presentationdomain.EventUnspecified:
	}

	return state
}

// cloneState isolates mutable maps and slices before applying one event.
func cloneState(state presentationdomain.State) presentationdomain.State {
	state.Startup = append([]presentationdomain.Line(nil), state.Startup...)
	state.Transcript = append([]presentationdomain.Line(nil), state.Transcript...)
	activeModel := make(map[int]string, len(state.ActiveModel))
	for position, text := range state.ActiveModel {
		activeModel[position] = text
	}
	state.ActiveModel = activeModel
	activeTools := make(map[string]string, len(state.ActiveTools))
	for callID, name := range state.ActiveTools {
		activeTools[callID] = name
	}
	state.ActiveTools = activeTools

	return state
}

// toolName resolves progress and result ownership from the active call when needed.
func toolName(state presentationdomain.State, event presentationdomain.Event) string {
	if event.ToolName != "" {
		return event.ToolName
	}
	if name := state.ActiveTools[event.ToolCallID]; name != "" {
		return name
	}
	if event.ToolCallID == "" && len(state.ActiveTools) == 1 {
		for _, name := range state.ActiveTools {
			return name
		}
	}
	return ""
}
