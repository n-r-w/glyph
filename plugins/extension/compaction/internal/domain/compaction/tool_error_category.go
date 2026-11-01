package compaction

// ToolErrorCategory identifies the Host error category of a failed tool result; the plugin only passes the value
// through and attaches no meaning to it.
type ToolErrorCategory string

const (
	// ToolErrorUnavailableTool identifies a call of a tool that was not available.
	ToolErrorUnavailableTool ToolErrorCategory = "unavailable_tool"
	// ToolErrorInvalidArguments identifies a call whose arguments did not match the tool input schema.
	ToolErrorInvalidArguments ToolErrorCategory = "invalid_arguments"
	// ToolErrorFailure identifies an error that the tool reported or that Host created from an execution error.
	ToolErrorFailure ToolErrorCategory = "tool_failure"
	// ToolErrorPolicyRejection identifies a call that an extension rejected before the tool ran.
	ToolErrorPolicyRejection ToolErrorCategory = "policy_rejection"
	// ToolErrorInvalid identifies an unrecognized category received from Host.
	ToolErrorInvalid ToolErrorCategory = "invalid"
)
