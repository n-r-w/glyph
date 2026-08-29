// Package presentation projects provider-neutral Host events into TUI state.
package presentation

import (
	"bytes"
	"encoding/json/v2"
	"maps"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

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
	if applySessionEvent(&state, event) {
		return state
	}

	switch event.Kind {
	case presentationdomain.EventInitialization:
		if event.Availability.IsSome() {
			state.Availability = event.Availability
		}
		state.Startup = append(state.Startup, event.Startup...)
		state.Models = cloneModels(event.Models)
		if event.ModelSelection.IsSome() {
			state.ModelSelection = event.ModelSelection
		}
		if event.SessionInfo.IsSome() {
			state.SessionInfo = event.SessionInfo
		}
	case presentationdomain.EventUserSubmitted:
		if event.Text.IsSome() {
			state.Transcript = append(state.Transcript, textLine(presentationdomain.LineUser, event.Text))
		}
	case presentationdomain.EventAvailability:
		if event.Availability.IsSome() {
			state.Availability = event.Availability
		}
	case presentationdomain.EventTurnStarted:
		state.Settled = mo.Some(false)
	case presentationdomain.EventModelDelta:
		applyModelDelta(&state, event)
	case presentationdomain.EventModelEnd:
		applyModelEnd(&state, event)
	case presentationdomain.EventToolCallPreview, presentationdomain.EventToolCallFinal:
		if call, ok := event.ToolCall.Get(); ok && call.CallID != "" {
			state.ActiveToolCalls[call.CallID] = cloneToolCall(call)
		}
	case presentationdomain.EventToolStarted:
		applyToolStarted(&state, event)
	case presentationdomain.EventToolProgress:
		applyToolProgress(&state, event)
	case presentationdomain.EventToolOutput:
		applyToolOutput(&state, event)
	case presentationdomain.EventToolEnded:
		applyToolEnded(&state, event)
	case presentationdomain.EventToolResult:
		applyToolResult(&state, event)
	case presentationdomain.EventTurnEnded:
	case presentationdomain.EventAgentSettled:
		state.Settled = mo.Some(true)
	case presentationdomain.EventAuthorization:
		if event.Text.IsSome() {
			state.AuthorizationURL = event.Text
		}
	case presentationdomain.EventInformation:
		if event.Text.IsSome() {
			state.Transcript = append(state.Transcript, textLine(presentationdomain.LineInformation, event.Text))
		}
	case presentationdomain.EventError:
		applyError(&state, event)
	case presentationdomain.EventModelSelectionChanged:
		if event.ModelSelection.IsSome() {
			state.ModelSelection = event.ModelSelection
		}
	case presentationdomain.EventSessionList, presentationdomain.EventSessionChanged,
		presentationdomain.EventSessionInformation:
	case presentationdomain.EventUnspecified:
	}

	return state
}

// applyError removes unconfirmed turn state before rendering a terminal persistence failure.
func applyError(state *presentationdomain.State, event presentationdomain.Event) {
	if event.Availability.IsSome() {
		state.Availability = event.Availability
	}
	if event.Text.IsNone() {
		return
	}
	if strings.HasPrefix(event.Text.OrEmpty(), "session persistence failed") {
		// Failed terminal persistence discards only unconfirmed model and tool presentation state.
		clear(state.ActiveModel)
		clear(state.ActiveToolCalls)
		clear(state.ActiveTools)
	}
	state.Transcript = append(state.Transcript, textLine(presentationdomain.LineError, event.Text))
}

// applySessionEvent replaces transcript-owned state when the active session changes.
func applySessionEvent(state *presentationdomain.State, event presentationdomain.Event) bool {
	switch event.Kind {
	case presentationdomain.EventSessionList:
		state.Sessions = append([]presentationdomain.SessionSummary(nil), event.Sessions...)
	case presentationdomain.EventSessionChanged:
		if event.SessionInfo.IsSome() {
			state.SessionInfo = event.SessionInfo
			// Transcript ownership changes atomically with the confirmed active session.
			state.Transcript = cloneLines(event.RestoredTranscript)
			state.ActiveModel = make(map[int]presentationdomain.ActiveModelContent)
			state.ActiveToolCalls = make(map[string]presentationdomain.ToolCallState)
			state.ActiveTools = make(map[string]string)
		}
	case presentationdomain.EventSessionInformation:
		if event.SessionInfo.IsSome() {
			state.SessionInfo = event.SessionInfo
		}
	case presentationdomain.EventUnspecified, presentationdomain.EventInitialization,
		presentationdomain.EventUserSubmitted, presentationdomain.EventAvailability,
		presentationdomain.EventTurnStarted, presentationdomain.EventModelDelta,
		presentationdomain.EventModelEnd, presentationdomain.EventToolCallPreview,
		presentationdomain.EventToolCallFinal, presentationdomain.EventToolStarted,
		presentationdomain.EventToolProgress, presentationdomain.EventToolOutput,
		presentationdomain.EventToolEnded, presentationdomain.EventToolResult,
		presentationdomain.EventTurnEnded, presentationdomain.EventAgentSettled,
		presentationdomain.EventAuthorization, presentationdomain.EventInformation,
		presentationdomain.EventError, presentationdomain.EventModelSelectionChanged:
		return false
	default:
		return false
	}
	return true
}

// textLine creates one text variant without activating tool payloads.
func textLine(kind presentationdomain.LineKind, text mo.Option[string]) presentationdomain.Line {
	return presentationdomain.Line{
		Kind:     kind,
		ToolName: mo.None[string](),
		Status:   mo.None[string](),
		Text:     text,
		Contents: mo.None[[]presentationdomain.Content](),
	}
}

// applyModelDelta merges only present model content fields at one position.
func applyModelDelta(state *presentationdomain.State, event presentationdomain.Event) {
	position, positionOK := event.Position.Get()
	kind, kindOK := event.ModelContentKind.Get()
	text, textOK := event.Text.Get()
	if !positionOK || !kindOK && !textOK {
		return
	}
	content := state.ActiveModel[position]
	if kindOK {
		content.Kind = mo.Some(kind)
	}
	if textOK {
		current, currentOK := content.Text.Get()
		if !currentOK {
			current = ""
		}
		content.Text = mo.Some(current + text)
	}
	state.ActiveModel[position] = content
}

// applyModelEnd finalizes visible content and removes obsolete call previews.
func applyModelEnd(state *presentationdomain.State, event presentationdomain.Event) {
	state.Transcript = appendFinalModelContent(state.Transcript, event.ModelResponseContent)
	clear(state.ActiveModel)
	status, statusOK := event.Status.Get()
	if !statusOK || status != "tool_use" {
		clear(state.ActiveToolCalls)
		return
	}
	for callID, call := range state.ActiveToolCalls {
		if call.Provisional {
			delete(state.ActiveToolCalls, callID)
		}
	}
}

// applyToolStarted records validated tool identity and finalized arguments.
func applyToolStarted(state *presentationdomain.State, event presentationdomain.Event) {
	callID, callIDOK := event.ToolCallID.Get()
	name, nameOK := event.ToolName.Get()
	if !nameOK || event.Status.IsNone() {
		return
	}
	if callIDOK {
		if call, ok := state.ActiveToolCalls[callID]; ok && !call.Provisional {
			arguments, _ := json.Marshal(call.Arguments)
			state.Transcript = append(state.Transcript, presentationdomain.Line{
				Kind:     presentationdomain.LineToolStatus,
				ToolName: mo.Some(call.Name),
				Status:   mo.Some("arguments"),
				Text:     mo.Some(string(arguments)),
				Contents: mo.None[[]presentationdomain.Content](),
			})
			delete(state.ActiveToolCalls, callID)
		}
		state.ActiveTools[callID] = name
	}
	state.Transcript = append(state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineToolStatus,
		ToolName: mo.Some(name),
		Status:   event.Status,
		Text:     event.Text,
		Contents: mo.None[[]presentationdomain.Content](),
	})
}

// applyToolProgress appends a status line only when status is present.
func applyToolProgress(state *presentationdomain.State, event presentationdomain.Event) {
	if event.Status.IsNone() {
		return
	}
	state.Transcript = append(state.Transcript, presentationdomain.Line{
		Kind:     presentationdomain.LineToolStatus,
		ToolName: toolName(*state, event),
		Status:   event.Status,
		Text:     event.Text,
		Contents: mo.None[[]presentationdomain.Content](),
	})
}

// applyToolOutput appends present output to its selected stream.
func applyToolOutput(state *presentationdomain.State, event presentationdomain.Event) {
	stream, streamOK := event.Stream.Get()
	if !streamOK || event.Text.IsNone() {
		return
	}
	kind := presentationdomain.LineToolStdout
	if stream == presentationdomain.OutputStderr {
		kind = presentationdomain.LineToolStderr
	}
	state.Transcript = append(state.Transcript, presentationdomain.Line{
		Kind:     kind,
		ToolName: toolName(*state, event),
		Status:   mo.None[string](),
		Text:     event.Text,
		Contents: mo.None[[]presentationdomain.Content](),
	})
}

// applyToolEnded appends completion only when status and failure are present.
func applyToolEnded(state *presentationdomain.State, event presentationdomain.Event) {
	failure, failureOK := event.Failure.Get()
	if !failureOK || event.Status.IsNone() {
		return
	}
	kind := presentationdomain.LineToolDone
	if failure {
		kind = presentationdomain.LineToolError
	}
	state.Transcript = append(state.Transcript, presentationdomain.Line{
		Kind:     kind,
		ToolName: event.ToolName,
		Status:   event.Status,
		Text:     mo.None[string](),
		Contents: mo.None[[]presentationdomain.Content](),
	})
}

// applyToolResult clones and appends a validated terminal result payload.
func applyToolResult(state *presentationdomain.State, event presentationdomain.Event) {
	contents, contentsOK := event.Contents.Get()
	failure, failureOK := event.Failure.Get()
	if !contentsOK || !failureOK {
		return
	}
	kind := presentationdomain.LineToolDone
	exitCode, exitCodeOK := event.ExitCode.Get()
	if failure || exitCodeOK && exitCode != 0 {
		kind = presentationdomain.LineToolError
	}
	state.Transcript = append(state.Transcript, presentationdomain.Line{
		Kind:     kind,
		ToolName: toolName(*state, event),
		Status:   mo.None[string](),
		Text:     mo.Some(toolResultText(contents)),
		Contents: mo.Some(cloneContents(contents)),
	})
	if callID, ok := event.ToolCallID.Get(); ok {
		delete(state.ActiveTools, callID)
	}
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
		text, ok := item.Text.Get()
		if kind != presentationdomain.LineUnspecified && ok && text != "" {
			transcript = append(transcript, presentationdomain.Line{
				Kind:     kind,
				Text:     mo.Some(text),
				ToolName: mo.None[string](),
				Status:   mo.None[string](),
				Contents: mo.None[[]presentationdomain.Content](),
			})
		}
	}
	return transcript
}

// cloneState isolates mutable maps and slices before applying one event.
func cloneState(state presentationdomain.State) presentationdomain.State {
	state.Startup = cloneLines(state.Startup)
	state.Transcript = cloneLines(state.Transcript)
	state.Models = cloneModels(state.Models)
	state.Sessions = append([]presentationdomain.SessionSummary(nil), state.Sessions...)
	state.ActiveModel = maps.Clone(state.ActiveModel)
	state.ActiveToolCalls = maps.Clone(state.ActiveToolCalls)
	for callID, call := range state.ActiveToolCalls {
		state.ActiveToolCalls[callID] = cloneToolCall(call)
	}
	state.ActiveTools = maps.Clone(state.ActiveTools)

	return state
}

// cloneLines isolates optional public content and image bytes in retained snapshots.
func cloneLines(lines []presentationdomain.Line) []presentationdomain.Line {
	cloned := slices.Clone(lines)
	for index := range cloned {
		cloned[index].Contents = cloned[index].Contents.MapValue(cloneContents)
	}
	return cloned
}

// cloneModels isolates configured reasoning slices from incoming events.
func cloneModels(models []presentationdomain.ConfiguredModel) []presentationdomain.ConfiguredModel {
	cloned := slices.Clone(models)
	for index := range cloned {
		cloned[index].Reasoning.Choices = slices.Clone(cloned[index].Reasoning.Choices)
	}
	return cloned
}

// toolResultText creates a readable transcript for text-only terminal rendering.
func toolResultText(contents []presentationdomain.Content) string {
	parts := lo.FilterMap(contents, func(content presentationdomain.Content, _ int) (string, bool) {
		if mediaType, ok := content.MediaType.Get(); ok {
			return "[image: " + mediaType + "]", true
		}
		return content.Text.Get()
	})
	return strings.Join(parts, "\n")
}

// cloneContents isolates mutable image bytes in presentation state.
func cloneContents(contents []presentationdomain.Content) []presentationdomain.Content {
	cloned := slices.Clone(contents)
	for index := range cloned {
		cloned[index].Data = cloned[index].Data.MapValue(bytes.Clone)
	}
	return cloned
}

// cloneToolCall isolates all mutable call arguments and optional field values.
func cloneToolCall(call presentationdomain.ToolCallState) presentationdomain.ToolCallState {
	call.Fields = slices.Clone(call.Fields)
	for index := range call.Fields {
		call.Fields[index].Value = call.Fields[index].Value.MapValue(cloneJSONValue)
	}
	call.Arguments = cloneArguments(call.Arguments)
	return call
}

// cloneArguments recursively isolates a JSON object.
func cloneArguments(arguments map[string]any) map[string]any {
	if arguments == nil {
		return nil
	}
	cloned := make(map[string]any, len(arguments))
	for key, value := range arguments {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

// cloneJSONValue recursively copies mutable JSON containers and byte slices.
func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneArguments(typed)
	case []any:
		cloned := slices.Clone(typed)
		for index, item := range cloned {
			cloned[index] = cloneJSONValue(item)
		}
		return cloned
	case []byte:
		return bytes.Clone(typed)
	default:
		return typed
	}
}

// toolName resolves progress and result ownership from the active call when needed.
func toolName(state presentationdomain.State, event presentationdomain.Event) mo.Option[string] {
	if event.ToolName.IsSome() {
		return event.ToolName
	}
	if callID, ok := event.ToolCallID.Get(); ok {
		if name, found := state.ActiveTools[callID]; found {
			return mo.Some(name)
		}
		return mo.None[string]()
	}
	if len(state.ActiveTools) == 1 {
		for _, name := range state.ActiveTools {
			return mo.Some(name)
		}
	}
	return mo.None[string]()
}
