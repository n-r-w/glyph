package presentation

import (
	"encoding/json/v2"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"
)

// Apply returns a copy of the state with one Host event applied.
func (state State) Apply(event Event) State {
	state = state.Clone()
	if state.ActiveModel == nil {
		state.ActiveModel = make(map[int]ActiveModelContent)
	}
	if state.ActiveToolCalls == nil {
		state.ActiveToolCalls = make(map[string]ToolCallState)
	}
	if state.ActiveTools == nil {
		state.ActiveTools = make(map[string]string)
	}
	if state.applySessionEvent(event) || state.applyLifecycleEvent(event) ||
		state.applyTextEvent(event) || state.applyModelEvent(event) {
		return state
	}
	state.applyToolEvent(event)
	return state
}

// applyLifecycleEvent applies availability and lifecycle state changes.
func (state *State) applyLifecycleEvent(event Event) bool {
	switch event.Kind {
	case EventInitialization:
		state.applyInitialization(event)
	case EventAvailability:
		if event.Availability.IsSome() {
			state.Availability = event.Availability
		}
	case EventTurnStarted:
		state.Settled = mo.Some(false)
	case EventAgentSettled:
		state.Settled = mo.Some(true)
	case EventModelSelectionChanged:
		if event.ModelSelection.IsSome() {
			state.ModelSelection = event.ModelSelection
		}
	case EventUnspecified, EventTurnEnded:
	case EventUserSubmitted, EventModelDelta, EventModelEnd, EventToolCallPreview, EventToolCallFinal,
		EventToolStarted, EventToolProgress, EventToolOutput, EventToolEnded, EventToolResult,
		EventAuthorization, EventInformation, EventError,
		EventSessionList, EventSessionChanged, EventSessionInformation,
		EventSessionTree, EventSessionTreeNavigation, EventTreeOperationFailed,
		EventSessionForked, EventSessionCloned, EventEntryLabelSet:
		return false
	}
	return true
}

// applyTextEvent applies user, authorization, information, and error text.
func (state *State) applyTextEvent(event Event) bool {
	switch event.Kind {
	case EventUserSubmitted:
		if event.Text.IsSome() {
			state.Transcript = append(state.Transcript, NewTextLine(LineUser, event.Text))
		}
	case EventAuthorization:
		if event.Text.IsSome() {
			state.AuthorizationURL = event.Text
		}
	case EventInformation:
		if event.Text.IsSome() {
			state.Transcript = append(state.Transcript, NewTextLine(LineInformation, event.Text))
		}
	case EventError:
		state.applyError(event)
	case EventUnspecified, EventInitialization, EventAvailability, EventTurnStarted,
		EventModelDelta, EventModelEnd, EventToolCallPreview, EventToolCallFinal,
		EventToolStarted, EventToolProgress, EventToolOutput, EventToolEnded, EventToolResult, EventTurnEnded,
		EventAgentSettled, EventModelSelectionChanged, EventSessionList, EventSessionChanged, EventSessionInformation,
		EventSessionTree, EventSessionTreeNavigation, EventTreeOperationFailed,
		EventSessionForked, EventSessionCloned, EventEntryLabelSet:
		return false
	}
	return true
}

// applyModelEvent applies model content and tool-call declaration events.
func (state *State) applyModelEvent(event Event) bool {
	switch event.Kind {
	case EventModelDelta:
		state.applyModelDelta(event)
	case EventModelEnd:
		state.applyModelEnd(event)
	case EventToolCallPreview, EventToolCallFinal:
		if call, present := event.ToolCall.Get(); present && call.CallID != "" {
			state.ActiveToolCalls[call.CallID] = call.Clone()
		}
	case EventUnspecified, EventInitialization, EventUserSubmitted, EventAvailability, EventTurnStarted,
		EventToolStarted, EventToolProgress, EventToolOutput, EventToolEnded, EventToolResult, EventTurnEnded,
		EventAgentSettled, EventAuthorization, EventInformation, EventError, EventModelSelectionChanged,
		EventSessionList, EventSessionChanged, EventSessionInformation,
		EventSessionTree, EventSessionTreeNavigation, EventTreeOperationFailed,
		EventSessionForked, EventSessionCloned, EventEntryLabelSet:
		return false
	}
	return true
}

// applyToolEvent applies tool execution events.
func (state *State) applyToolEvent(event Event) bool {
	switch event.Kind {
	case EventToolStarted:
		state.applyToolStarted(event)
	case EventToolProgress:
		state.applyToolProgress(event)
	case EventToolOutput:
		state.applyToolOutput(event)
	case EventToolEnded:
		state.applyToolEnded(event)
	case EventToolResult:
		state.applyToolResult(event)
	case EventUnspecified, EventInitialization, EventUserSubmitted, EventAvailability, EventTurnStarted,
		EventModelDelta, EventModelEnd, EventToolCallPreview, EventToolCallFinal, EventTurnEnded,
		EventAgentSettled, EventAuthorization, EventInformation, EventError, EventModelSelectionChanged,
		EventSessionList, EventSessionChanged, EventSessionInformation,
		EventSessionTree, EventSessionTreeNavigation, EventTreeOperationFailed,
		EventSessionForked, EventSessionCloned, EventEntryLabelSet:
		return false
	}
	return true
}

// applyInitialization applies one complete startup snapshot.
func (state *State) applyInitialization(event Event) {
	if event.Availability.IsSome() {
		state.Availability = event.Availability
	}
	state.Startup = append(state.Startup, cloneLines(event.Startup)...)
	state.Models = cloneModels(event.Models)
	if event.ModelSelection.IsSome() {
		state.ModelSelection = event.ModelSelection
	}
	if event.SessionInfo.IsSome() {
		state.SessionInfo = event.SessionInfo
	}
}

// applyError removes unconfirmed turn state before rendering a terminal persistence failure.
func (state *State) applyError(event Event) {
	if event.Availability.IsSome() {
		state.Availability = event.Availability
	}
	if event.Text.IsNone() {
		return
	}
	if strings.HasPrefix(event.Text.OrEmpty(), "session persistence failed") {
		clear(state.ActiveModel)
		clear(state.ActiveToolCalls)
		clear(state.ActiveTools)
	}
	state.Transcript = append(state.Transcript, NewTextLine(LineError, event.Text))
}

// applySessionEvent applies session-owned state and reports whether the event was handled.
func (state *State) applySessionEvent(event Event) bool {
	switch event.Kind {
	case EventSessionList:
		state.Sessions = slices.Clone(event.Sessions)
	case EventSessionChanged:
		if event.SessionInfo.IsSome() {
			state.SessionInfo = event.SessionInfo
			state.Transcript = cloneLines(event.RestoredTranscript)
			state.ActiveModel = make(map[int]ActiveModelContent)
			state.ActiveToolCalls = make(map[string]ToolCallState)
			state.ActiveTools = make(map[string]string)
		}
	case EventSessionInformation:
		if event.SessionInfo.IsSome() {
			state.SessionInfo = event.SessionInfo
		}
	case EventUnspecified, EventInitialization, EventUserSubmitted, EventAvailability, EventTurnStarted,
		EventModelDelta, EventModelEnd, EventToolCallPreview, EventToolCallFinal, EventToolStarted,
		EventToolProgress, EventToolOutput, EventToolEnded, EventToolResult, EventTurnEnded, EventAgentSettled,
		EventAuthorization, EventInformation, EventError, EventModelSelectionChanged,
		EventSessionTree, EventSessionTreeNavigation, EventTreeOperationFailed,
		EventSessionForked, EventSessionCloned, EventEntryLabelSet:
		return false
	default:
		return false
	}
	return true
}

// NewTextLine creates one text line without tool payloads.
func NewTextLine(kind LineKind, text mo.Option[string]) Line {
	return Line{
		Kind: kind, ToolName: mo.None[string](), Status: mo.None[string](), Text: text,
		Contents: mo.None[[]Content](),
	}
}

// applyModelDelta merges present model content fields at one position.
func (state *State) applyModelDelta(event Event) {
	position, positionPresent := event.Position.Get()
	kind, kindPresent := event.ModelContentKind.Get()
	text, textPresent := event.Text.Get()
	if !positionPresent || !kindPresent && !textPresent {
		return
	}
	content := state.ActiveModel[position]
	if kindPresent {
		content.Kind = mo.Some(kind)
	}
	if textPresent {
		content.Text = mo.Some(content.Text.OrEmpty() + text)
	}
	state.ActiveModel[position] = content
}

// applyModelEnd finalizes visible content and removes obsolete call previews.
func (state *State) applyModelEnd(event Event) {
	state.appendFinalModelContent(event.ModelResponseContent)
	clear(state.ActiveModel)
	status, statusPresent := event.Status.Get()
	if !statusPresent || status != "tool_use" {
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
func (state *State) applyToolStarted(event Event) {
	callID, callIDPresent := event.ToolCallID.Get()
	name, namePresent := event.ToolName.Get()
	if !namePresent || event.Status.IsNone() {
		return
	}
	if callIDPresent {
		if call, found := state.ActiveToolCalls[callID]; found && !call.Provisional {
			arguments, _ := json.Marshal(call.Arguments)
			state.Transcript = append(state.Transcript, Line{
				Kind: LineToolStatus, ToolName: mo.Some(call.Name), Status: mo.Some("arguments"),
				Text: mo.Some(string(arguments)), Contents: mo.None[[]Content](),
			})
			delete(state.ActiveToolCalls, callID)
		}
		state.ActiveTools[callID] = name
	}
	state.Transcript = append(state.Transcript, Line{
		Kind: LineToolStatus, ToolName: mo.Some(name), Status: event.Status, Text: event.Text,
		Contents: mo.None[[]Content](),
	})
}

// applyToolProgress appends a status line when status is present.
func (state *State) applyToolProgress(event Event) {
	if event.Status.IsNone() {
		return
	}
	state.Transcript = append(state.Transcript, Line{
		Kind: LineToolStatus, ToolName: state.ToolName(event), Status: event.Status, Text: event.Text,
		Contents: mo.None[[]Content](),
	})
}

// applyToolOutput appends present output to its selected stream.
func (state *State) applyToolOutput(event Event) {
	stream, streamPresent := event.Stream.Get()
	if !streamPresent || event.Text.IsNone() {
		return
	}
	kind := LineToolStdout
	if stream == OutputStderr {
		kind = LineToolStderr
	}
	state.Transcript = append(state.Transcript, Line{
		Kind: kind, ToolName: state.ToolName(event), Status: mo.None[string](), Text: event.Text,
		Contents: mo.None[[]Content](),
	})
}

// applyToolEnded appends completion when status and failure are present.
func (state *State) applyToolEnded(event Event) {
	failure, failurePresent := event.Failure.Get()
	if !failurePresent || event.Status.IsNone() {
		return
	}
	kind := LineToolDone
	if failure {
		kind = LineToolError
	}
	state.Transcript = append(state.Transcript, Line{
		Kind: kind, ToolName: event.ToolName, Status: event.Status, Text: mo.None[string](),
		Contents: mo.None[[]Content](),
	})
}

// applyToolResult appends a validated terminal result payload.
func (state *State) applyToolResult(event Event) {
	contents, contentsPresent := event.Contents.Get()
	failure, failurePresent := event.Failure.Get()
	if !contentsPresent || !failurePresent {
		return
	}
	kind := LineToolDone
	exitCode, exitCodePresent := event.ExitCode.Get()
	if failure || exitCodePresent && exitCode != 0 {
		kind = LineToolError
	}
	state.Transcript = append(state.Transcript, Line{
		Kind: kind, ToolName: state.ToolName(event), Status: mo.None[string](),
		Text: mo.Some(toolResultText(contents)), Contents: mo.Some(cloneContents(contents)),
	})
	if callID, present := event.ToolCallID.Get(); present {
		delete(state.ActiveTools, callID)
	}
}

// appendFinalModelContent appends visible final model blocks to the transcript.
func (state *State) appendFinalModelContent(content []ModelResponseContent) {
	for _, item := range content {
		kind := LineUnspecified
		switch item.Kind {
		case ModelContentText:
			kind = LineModel
		case ModelContentRefusal:
			kind = LineRefusal
		case ModelContentReasoning:
			kind = LineReasoning
		case ModelContentUnspecified:
		}
		text, present := item.Text.Get()
		if kind != LineUnspecified && present && text != "" {
			state.Transcript = append(state.Transcript, Line{
				Kind: kind, ToolName: mo.None[string](), Status: mo.None[string](), Text: mo.Some(text),
				Contents: mo.None[[]Content](),
			})
		}
	}
}

// cloneModels returns deep copies of configured models.
func cloneModels(models []ConfiguredModel) []ConfiguredModel {
	cloned := slices.Clone(models)
	for index := range cloned {
		cloned[index] = cloned[index].Clone()
	}
	return cloned
}

// cloneContents returns deep copies of content blocks.
func cloneContents(contents []Content) []Content {
	cloned := slices.Clone(contents)
	for index := range cloned {
		cloned[index] = cloned[index].Clone()
	}
	return cloned
}

// toolResultText creates readable text-only terminal output.
func toolResultText(contents []Content) string {
	parts := lo.FilterMap(contents, func(content Content, _ int) (string, bool) {
		if mediaType, present := content.MediaType.Get(); present {
			return "[image: " + mediaType + "]", true
		}
		return content.Text.Get()
	})
	return strings.Join(parts, "\n")
}
