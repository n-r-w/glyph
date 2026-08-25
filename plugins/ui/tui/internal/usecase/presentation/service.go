// Package presentation projects provider-neutral Host events into TUI state.
//
//nolint:exhaustruct // Projection lines intentionally set only fields used by their line kind.
package presentation

import (
	"bytes"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

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
		state.ActiveModel = make(map[int]presentationdomain.ActiveModelContent)
	}
	if state.ActiveToolCalls == nil {
		state.ActiveToolCalls = make(map[string]presentationdomain.ToolCallState)
	}
	if state.ActiveTools == nil {
		state.ActiveTools = make(map[string]string)
	}

	switch event.Kind {
	case presentationdomain.EventInitialization:
		state.Availability = event.Availability
		state.Startup = append(state.Startup, event.Startup...)
		state.Models = cloneModels(event.Models)
		state.ModelSelection = event.ModelSelection
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
		content := state.ActiveModel[event.Position]
		if event.ModelContentKind != presentationdomain.ModelContentUnspecified {
			content.Kind = event.ModelContentKind
		}
		content.Text += event.Text
		state.ActiveModel[event.Position] = content
	case presentationdomain.EventModelEnd:
		state.Transcript = appendFinalModelContent(state.Transcript, event.ModelResponseContent)
		clear(state.ActiveModel)
		if event.Status != "tool_use" {
			clear(state.ActiveToolCalls)
		} else {
			for callID, call := range state.ActiveToolCalls {
				if call.Provisional {
					delete(state.ActiveToolCalls, callID)
				}
			}
		}
	case presentationdomain.EventToolCallPreview, presentationdomain.EventToolCallFinal:
		if event.ToolCall.CallID != "" {
			state.ActiveToolCalls[event.ToolCall.CallID] = cloneToolCall(event.ToolCall)
		}
	case presentationdomain.EventToolStarted:
		if call, ok := state.ActiveToolCalls[event.ToolCallID]; ok && !call.Provisional {
			arguments, _ := json.Marshal(call.Arguments)
			state.Transcript = append(state.Transcript, presentationdomain.Line{
				Kind: presentationdomain.LineToolStatus, ToolName: call.Name,
				Status: "arguments", Text: string(arguments),
			})
			delete(state.ActiveToolCalls, event.ToolCallID)
		}
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
			Kind: kind, ToolName: toolName(state, event), Text: toolResultLineText(event),
			ToolResultContents: cloneToolResultContents(event.ToolResultContents),
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
	case presentationdomain.EventModelSelectionChanged:
		state.ModelSelection = event.ModelSelection
	case presentationdomain.EventUnspecified:
	}

	return state
}

// appendFinalModelContent keeps visible model content blocks distinct in the transcript.
func appendFinalModelContent(
	transcript []presentationdomain.Line,
	content []presentationdomain.ModelResponseContent,
) []presentationdomain.Line {
	for _, item := range content {
		kind := presentationdomain.LineUnspecified
		switch item.Kind {
		case presentationdomain.ModelContentText:
			kind = presentationdomain.LineModel
		case presentationdomain.ModelContentRefusal:
			kind = presentationdomain.LineRefusal
		case presentationdomain.ModelContentReasoning:
			kind = presentationdomain.LineReasoning
		case presentationdomain.ModelContentUnspecified:
		}
		if kind != presentationdomain.LineUnspecified && item.Text != "" {
			transcript = append(transcript, presentationdomain.Line{Kind: kind, Text: item.Text})
		}
	}
	return transcript
}

// cloneState isolates mutable maps and slices before applying one event.
func cloneState(state presentationdomain.State) presentationdomain.State {
	state.Startup = slices.Clone(state.Startup)
	state.Transcript = slices.Clone(state.Transcript)
	state.Models = cloneModels(state.Models)
	state.ActiveModel = maps.Clone(state.ActiveModel)
	state.ActiveToolCalls = maps.Clone(state.ActiveToolCalls)
	for callID, call := range state.ActiveToolCalls {
		state.ActiveToolCalls[callID] = cloneToolCall(call)
	}
	state.ActiveTools = maps.Clone(state.ActiveTools)

	return state
}

// cloneModels isolates configured reasoning slices from incoming events.
func cloneModels(models []presentationdomain.ConfiguredModel) []presentationdomain.ConfiguredModel {
	cloned := slices.Clone(models)
	for index := range cloned {
		cloned[index].Reasoning.Choices = slices.Clone(cloned[index].Reasoning.Choices)
	}
	return cloned
}

// toolResultLineText preserves legacy event text only when no typed blocks exist.
func toolResultLineText(event presentationdomain.Event) string {
	if len(event.ToolResultContents) == 0 {
		return event.Text
	}
	return toolResultText(event.ToolResultContents)
}

// toolResultText creates a readable transcript for text-only terminal rendering.
func toolResultText(contents []presentationdomain.ToolResultContent) string {
	parts := lo.Map(contents, func(content presentationdomain.ToolResultContent, _ int) string {
		if content.MediaType != "" {
			return "[image: " + content.MediaType + "]"
		}
		return content.Text
	})
	return strings.Join(parts, "\n")
}

// cloneToolResultContents isolates mutable image bytes in presentation state.
func cloneToolResultContents(contents []presentationdomain.ToolResultContent) []presentationdomain.ToolResultContent {
	if contents == nil {
		return nil
	}
	cloned := slices.Clone(contents)
	for index := range cloned {
		cloned[index].Data = bytes.Clone(cloned[index].Data)
	}
	return cloned
}

func cloneToolCall(call presentationdomain.ToolCallState) presentationdomain.ToolCallState {
	call.Fields = slices.Clone(call.Fields)
	call.Arguments = maps.Clone(call.Arguments)
	return call
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
