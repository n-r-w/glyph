package presentation

import (
	"encoding/json/v2"
	"slices"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"
)

// Apply returns a copy of the state with one Host event applied.
func (state projection) Apply(event event) projection {
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
func (state *projection) applyLifecycleEvent(event event) bool {
	switch event.Kind {
	case eventInitialization:
		state.applyInitialization(event)
	case eventAvailability:
		if event.Availability.IsSome() {
			state.Availability = event.Availability
		}
	case eventTurnStarted:
		state.Settled = mo.Some(false)
	case eventAgentSettled:
		state.Settled = mo.Some(true)
	case eventModelSelectionChanged:
		if event.ModelSelection.IsSome() {
			state.ModelSelection = event.ModelSelection
		}
	case eventUnspecified, eventTurnEnded:
	case eventUserSubmitted, eventModelDelta, eventModelEnd, eventToolCallPreview, eventToolCallFinal,
		eventToolStarted, eventToolProgress, eventToolOutput, eventToolEnded, eventToolResult,
		eventAuthorization, eventInformation, eventError,
		eventSessionList, eventSessionChanged, eventSessionInformation,
		eventSessionTree, eventSessionTreeNavigationProgress, eventSessionTreeNavigation, eventTreeOperationFailed,
		eventSessionForked, eventSessionCloned, eventEntryLabelSet, eventSessionEntryAdded:
		return false
	}
	return true
}

// applyTextEvent applies user, authorization, information, and error text.
func (state *projection) applyTextEvent(event event) bool {
	switch event.Kind {
	case eventUserSubmitted:
		if event.Text.IsSome() {
			state.Transcript = append(state.Transcript, NewTextLine(LineUser, event.Text))
		}
	case eventAuthorization:
		if event.Text.IsSome() {
			state.AuthorizationURL = event.Text
		}
	case eventInformation:
		if event.Text.IsSome() {
			state.Transcript = append(state.Transcript, NewTextLine(LineInformation, event.Text))
		}
	case eventError:
		state.applyError(event)
	case eventUnspecified, eventInitialization, eventAvailability, eventTurnStarted,
		eventModelDelta, eventModelEnd, eventToolCallPreview, eventToolCallFinal,
		eventToolStarted, eventToolProgress, eventToolOutput, eventToolEnded, eventToolResult, eventTurnEnded,
		eventAgentSettled, eventModelSelectionChanged, eventSessionList, eventSessionChanged, eventSessionInformation,
		eventSessionTree, eventSessionTreeNavigationProgress, eventSessionTreeNavigation, eventTreeOperationFailed,
		eventSessionForked, eventSessionCloned, eventEntryLabelSet, eventSessionEntryAdded:
		return false
	}
	return true
}

// applyModelEvent applies model content and tool-call declaration events.
func (state *projection) applyModelEvent(event event) bool {
	switch event.Kind {
	case eventModelDelta:
		state.applyModelDelta(event)
	case eventModelEnd:
		state.applyModelEnd(event)
	case eventToolCallPreview, eventToolCallFinal:
		if call, present := event.ToolCall.Get(); present && call.CallID != "" {
			state.ActiveToolCalls[call.CallID] = call.Clone()
		}
	case eventUnspecified, eventInitialization, eventUserSubmitted, eventAvailability, eventTurnStarted,
		eventToolStarted, eventToolProgress, eventToolOutput, eventToolEnded, eventToolResult, eventTurnEnded,
		eventAgentSettled, eventAuthorization, eventInformation, eventError, eventModelSelectionChanged,
		eventSessionList, eventSessionChanged, eventSessionInformation,
		eventSessionTree, eventSessionTreeNavigationProgress, eventSessionTreeNavigation, eventTreeOperationFailed,
		eventSessionForked, eventSessionCloned, eventEntryLabelSet, eventSessionEntryAdded:
		return false
	}
	return true
}

// applyToolEvent applies tool execution events.
func (state *projection) applyToolEvent(event event) bool {
	switch event.Kind {
	case eventToolStarted:
		state.applyToolStarted(event)
	case eventToolProgress:
		state.applyToolProgress(event)
	case eventToolOutput:
		state.applyToolOutput(event)
	case eventToolEnded:
		state.applyToolEnded(event)
	case eventToolResult:
		state.applyToolResult(event)
	case eventUnspecified, eventInitialization, eventUserSubmitted, eventAvailability, eventTurnStarted,
		eventModelDelta, eventModelEnd, eventToolCallPreview, eventToolCallFinal, eventTurnEnded,
		eventAgentSettled, eventAuthorization, eventInformation, eventError, eventModelSelectionChanged,
		eventSessionList, eventSessionChanged, eventSessionInformation,
		eventSessionTree, eventSessionTreeNavigationProgress, eventSessionTreeNavigation, eventTreeOperationFailed,
		eventSessionForked, eventSessionCloned, eventEntryLabelSet, eventSessionEntryAdded:
		return false
	}
	return true
}

// applyInitialization applies one complete startup snapshot.
func (state *projection) applyInitialization(event event) {
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
func (state *projection) applyError(event event) {
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
func (state *projection) applySessionEvent(event event) bool {
	switch event.Kind {
	case eventSessionList:
		state.Sessions = slices.Clone(event.Sessions)
	case eventSessionChanged:
		if event.SessionInfo.IsSome() {
			state.SessionInfo = event.SessionInfo
			state.Transcript = cloneLines(event.RestoredTranscript)
			state.ActiveModel = make(map[int]ActiveModelContent)
			state.ActiveToolCalls = make(map[string]ToolCallState)
			state.ActiveTools = make(map[string]string)
		}
	case eventSessionInformation:
		if event.SessionInfo.IsSome() {
			state.SessionInfo = event.SessionInfo
		}
	case eventUnspecified, eventInitialization, eventUserSubmitted, eventAvailability, eventTurnStarted,
		eventModelDelta, eventModelEnd, eventToolCallPreview, eventToolCallFinal, eventToolStarted,
		eventToolProgress, eventToolOutput, eventToolEnded, eventToolResult, eventTurnEnded, eventAgentSettled,
		eventAuthorization, eventInformation, eventError, eventModelSelectionChanged,
		eventSessionTree, eventSessionTreeNavigationProgress, eventSessionTreeNavigation, eventTreeOperationFailed,
		eventSessionForked, eventSessionCloned, eventEntryLabelSet, eventSessionEntryAdded:
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
func (state *projection) applyModelDelta(event event) {
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
func (state *projection) applyModelEnd(event event) {
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
func (state *projection) applyToolStarted(event event) {
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
func (state *projection) applyToolProgress(event event) {
	if event.Status.IsNone() {
		return
	}
	state.Transcript = append(state.Transcript, Line{
		Kind: LineToolStatus, ToolName: state.ToolName(event), Status: event.Status, Text: event.Text,
		Contents: mo.None[[]Content](),
	})
}

// applyToolOutput appends present output to its selected stream.
func (state *projection) applyToolOutput(event event) {
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
func (state *projection) applyToolEnded(event event) {
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
func (state *projection) applyToolResult(event event) {
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
func (state *projection) appendFinalModelContent(content []ModelResponseContent) {
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
