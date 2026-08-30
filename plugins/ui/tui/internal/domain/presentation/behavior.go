package presentation

import (
	"bytes"
	"maps"
	"slices"

	"github.com/samber/mo"
)

// SelectionAllowed reports whether model selection can start in this availability state.
func (availability Availability) SelectionAllowed() bool {
	return availability != AvailabilityChecking && availability != AvailabilityAuthenticating
}

// Clone returns a deep copy of the configured model.
func (configured ConfiguredModel) Clone() ConfiguredModel {
	configured.Reasoning.Choices = slices.Clone(configured.Reasoning.Choices)
	return configured
}

// Clone returns a deep copy of the content block.
func (content Content) Clone() Content {
	content.Data = content.Data.MapValue(bytes.Clone)
	return content
}

// Clone returns a deep copy of the presentation line.
func (line Line) Clone() Line {
	line.Contents = line.Contents.MapValue(func(contents []Content) []Content {
		cloned := slices.Clone(contents)
		for index := range cloned {
			cloned[index] = cloned[index].Clone()
		}
		return cloned
	})
	return line
}

// Clone returns a deep copy of the argument field.
func (field ToolCallField) Clone() ToolCallField {
	field.Value = field.Value.MapValue(cloneJSONValue)
	return field
}

// Clone returns a deep copy of the tool-call state.
func (call ToolCallState) Clone() ToolCallState {
	call.Fields = slices.Clone(call.Fields)
	for index := range call.Fields {
		call.Fields[index] = call.Fields[index].Clone()
	}
	call.Arguments = cloneJSONMap(call.Arguments)
	return call
}

// Clone returns a deep copy of the presentation state.
func (state State) Clone() State {
	state.Startup = cloneLines(state.Startup)
	state.Transcript = cloneLines(state.Transcript)
	state.Models = slices.Clone(state.Models)
	for index := range state.Models {
		state.Models[index] = state.Models[index].Clone()
	}
	state.Sessions = slices.Clone(state.Sessions)
	state.ActiveModel = maps.Clone(state.ActiveModel)
	state.ActiveToolCalls = maps.Clone(state.ActiveToolCalls)
	for callID, call := range state.ActiveToolCalls {
		state.ActiveToolCalls[callID] = call.Clone()
	}
	state.ActiveTools = maps.Clone(state.ActiveTools)
	return state
}

// ToolName resolves the event tool name from explicit and active state.
func (state State) ToolName(event Event) mo.Option[string] {
	if event.ToolName.IsSome() {
		return event.ToolName
	}
	if callID, present := event.ToolCallID.Get(); present {
		name, found := state.ActiveTools[callID]
		if found {
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

// cloneLines returns a deep copy of presentation lines.
func cloneLines(lines []Line) []Line {
	cloned := slices.Clone(lines)
	for index := range cloned {
		cloned[index] = cloned[index].Clone()
	}
	return cloned
}

// cloneJSONMap returns a deep copy of one JSON object.
func cloneJSONMap(source map[string]any) map[string]any {
	if source == nil {
		return nil
	}
	cloned := maps.Clone(source)
	for key, value := range cloned {
		cloned[key] = cloneJSONValue(value)
	}
	return cloned
}

// cloneJSONValue returns a deep copy of JSON containers and byte slices.
func cloneJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return cloneJSONMap(typed)
	case []any:
		cloned := slices.Clone(typed)
		for index := range cloned {
			cloned[index] = cloneJSONValue(cloned[index])
		}
		return cloned
	case []byte:
		return bytes.Clone(typed)
	default:
		return typed
	}
}
