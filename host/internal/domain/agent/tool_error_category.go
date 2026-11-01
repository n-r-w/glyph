package agent

// ToolErrorCategory identifies the Glyph kind of an error tool result from a closed set.
type ToolErrorCategory uint8

const (
	// ToolErrorUnavailableTool marks a call of a tool that is not registered or whose extension is not running.
	ToolErrorUnavailableTool ToolErrorCategory = iota + 1
	// ToolErrorInvalidArguments marks a call whose arguments do not match the tool input schema.
	ToolErrorInvalidArguments
	// ToolErrorFailure marks an error that the tool reported or that Agent Core created from an execution error.
	ToolErrorFailure
)

const (
	// toolErrorUnavailableToolName is the model-visible name of ToolErrorUnavailableTool.
	toolErrorUnavailableToolName = "unavailable_tool"
	// toolErrorInvalidArgumentsName is the model-visible name of ToolErrorInvalidArguments.
	toolErrorInvalidArgumentsName = "invalid_arguments"
	// toolErrorFailureName is the model-visible name of ToolErrorFailure.
	toolErrorFailureName = "tool_failure"
)

// Valid reports whether the category belongs to the closed set.
func (category ToolErrorCategory) Valid() bool {
	return category >= ToolErrorUnavailableTool && category <= ToolErrorFailure
}

// Name returns the stable text that tells the model which kind of error a tool result reports.
func (category ToolErrorCategory) Name() string {
	switch category {
	case ToolErrorUnavailableTool:
		return toolErrorUnavailableToolName
	case ToolErrorInvalidArguments:
		return toolErrorInvalidArgumentsName
	case ToolErrorFailure:
		return toolErrorFailureName
	default:
		return ""
	}
}
