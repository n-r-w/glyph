// Package ui defines provider-neutral Host UI lifecycle models.
package ui

import "github.com/n-r-w/glyph/host/internal/domain/tool"

// ContentSeverity identifies startup content importance.
type ContentSeverity uint8

const (
	// ContentSeverityInformation identifies normal startup content.
	ContentSeverityInformation ContentSeverity = iota + 1
	// ContentSeverityError identifies one startup failure.
	ContentSeverityError
	// ContentSeverityWarning identifies one non-fatal automatic exclusion.
	ContentSeverityWarning
)

// Availability identifies whether the Host can accept a user request.
type Availability uint8

const (
	// AvailabilityCheckingAuthentication blocks input during the startup credential check.
	AvailabilityCheckingAuthentication Availability = iota + 1
	// AvailabilityAuthenticating blocks input during browser OAuth.
	AvailabilityAuthenticating
	// AvailabilityAuthenticationFailed permits only explicit authentication retry.
	AvailabilityAuthenticationFailed
	// AvailabilityIdle permits one user request.
	AvailabilityIdle
	// AvailabilityRunning permits stop or quit but rejects another request.
	AvailabilityRunning
)

// LifecycleType identifies one ordered Host or Agent lifecycle transition.
type LifecycleType uint8

const (
	// LifecycleAgentStart starts one agent run.
	LifecycleAgentStart LifecycleType = iota + 1
	// LifecycleTurnStart starts one model turn.
	LifecycleTurnStart
	// LifecycleMessageStart starts one model message.
	LifecycleMessageStart
	// LifecycleModelContentStart starts one typed model text block.
	LifecycleModelContentStart
	// LifecycleModelTextDelta carries one model text fragment.
	LifecycleModelTextDelta
	// LifecycleModelContentEnd finalizes one typed model text block.
	LifecycleModelContentEnd
	// LifecycleToolCallStart starts one provisional function call.
	LifecycleToolCallStart
	// LifecycleToolCallDelta replaces one provisional function-call preview.
	LifecycleToolCallDelta
	// LifecycleToolCallEnd carries exact final function-call arguments.
	LifecycleToolCallEnd
	// LifecycleMessageEnd finalizes one model message.
	LifecycleMessageEnd
	// LifecycleToolExecutionStart starts one tool call.
	LifecycleToolExecutionStart
	// LifecycleToolExecutionUpdate carries one tool progress fragment.
	LifecycleToolExecutionUpdate
	// LifecycleToolExecutionEnd finalizes one tool execution.
	LifecycleToolExecutionEnd
	// LifecycleToolResult carries one model-visible tool result.
	LifecycleToolResult
	// LifecycleTurnEnd finalizes one model turn.
	LifecycleTurnEnd
	// LifecycleAgentEnd finalizes Agent Core work.
	LifecycleAgentEnd
	// LifecycleAgentSettled marks Host recipient completion and idle settlement.
	LifecycleAgentSettled
	// LifecycleAvailabilityChanged updates task or authentication availability.
	LifecycleAvailabilityChanged
)

// ProgressChannel identifies one tool progress fragment channel.
type ProgressChannel uint8

const (
	// ProgressChannelStatus carries status text.
	ProgressChannelStatus ProgressChannel = iota + 1
	// ProgressChannelStdout carries standard output.
	ProgressChannelStdout
	// ProgressChannelStderr carries standard error.
	ProgressChannelStderr
)

// Candidate identifies one executable UI plugin candidate.
type Candidate struct {
	ID   string
	Path string
}

// Directory identifies the effective UI catalog directory.
type Directory struct {
	Path string
}

// Discovery is one complete valid UI catalog.
type Discovery struct {
	Candidates []Candidate
}

// Capabilities contains immutable startup behavior for one UI plugin.
type Capabilities struct {
	ControlsTerminal bool
}

// StartupContent carries one initialization information or error item.
type StartupContent struct {
	Severity ContentSeverity
	Text     string
}

// ExtensionAvailability identifies one available extension and its tool names.
type ExtensionAvailability struct {
	PluginID string
	Path     string
	Tools    []string
}

// Initialization is the first Host frame sent to a selected UI.
type Initialization struct {
	SelectedUIID   string
	StartupContent []StartupContent
	Extensions     []ExtensionAvailability
	Availability   Availability
}

// ModelContentType identifies one model content transition.
type ModelContentType uint8

const (
	// ModelContentStart starts one content block.
	ModelContentStart ModelContentType = iota + 1
	// ModelContentTextDelta appends one text-bearing fragment.
	ModelContentTextDelta
	// ModelContentEnd finalizes one content block.
	ModelContentEnd
)

// ModelContentKind identifies public model text or hidden reasoning content.
type ModelContentKind uint8

const (
	// ModelContentKindText contains visible model text.
	ModelContentKindText ModelContentKind = iota + 1
	// ModelContentKindRefusal contains visible model refusal text.
	ModelContentKindRefusal
	// ModelContentKindReasoning contains hidden reasoning text.
	ModelContentKindReasoning
)

// ModelContent carries one typed model content transition.
type ModelContent struct {
	Type     ModelContentType
	Kind     ModelContentKind
	Position int
	Text     string
}

// ModelResponseContent carries one ordered finalized model block.
type ModelResponseContent struct {
	Kind ModelContentKind
	Text string
}

// ModelUsage carries provider-reported token accounting.
type ModelUsage struct {
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	CacheWriteTokens  int64
	ReasoningTokens   int64
	TotalTokens       int64
}

// ModelDiagnostic carries safe typed provider diagnostics.
type ModelDiagnostic struct {
	Code    string
	Message string
}

// ModelResponse carries typed terminal model data.
type ModelResponse struct {
	Text          string
	Outcome       string
	ErrorMessage  string
	Provider      string
	Model         string
	ResponseModel *string
	ResponseID    string
	Content       []ModelResponseContent
	Usage         ModelUsage
	Diagnostics   []ModelDiagnostic
}

// ToolCallPreviewField carries one complete value or exact scalar prefix.
type ToolCallPreviewField struct {
	Name     string
	Value    any
	Prefix   string
	Complete bool
}

// ToolCallPreview carries transient function-call state.
type ToolCallPreview struct {
	CallID      string
	Name        string
	Position    int
	Provisional bool
	Fields      []ToolCallPreviewField
}

// FinalToolCall carries exact terminal arguments.
type FinalToolCall struct {
	CallID    string
	Name      string
	Position  int
	Arguments map[string]any
}

// Lifecycle carries one explicit provider-neutral lifecycle event.
type Lifecycle struct {
	Type               LifecycleType
	RunID              string
	Text               string
	ToolResultContents []tool.ResultContent
	ModelContent       ModelContent
	ModelResponse      ModelResponse
	ToolCallPreview    ToolCallPreview
	FinalToolCall      FinalToolCall
	ToolCallID         string
	ToolName           string
	ProgressChannel    ProgressChannel
	IsError            bool
	Outcome            string
	ErrorMessage       string
	Availability       Availability
}

// FrameKind identifies one Host-to-UI frame payload.
type FrameKind uint8

const (
	// FrameInitialization carries the one startup state.
	FrameInitialization FrameKind = iota + 1
	// FrameLifecycle carries one lifecycle transition.
	FrameLifecycle
	// FrameAuthorization carries one browser OAuth URL.
	FrameAuthorization
	// FrameInformation carries one user-visible notification.
	FrameInformation
	// FrameError carries one safe user-visible failure.
	FrameError
)

// Frame carries exactly one Host-to-UI payload.
type Frame struct {
	Kind                FrameKind
	Initialization      Initialization
	Lifecycle           Lifecycle
	AuthorizationURL    string
	Text                string
	RetryAuthentication bool
}

// CommandKind identifies one UI-to-Host command.
type CommandKind uint8

const (
	// CommandSubmit starts one user request while idle.
	CommandSubmit CommandKind = iota + 1
	// CommandStop cancels the active run.
	CommandStop
	// CommandRetryAuthentication retries OAuth after failure.
	CommandRetryAuthentication
	// CommandQuit terminates the UI session.
	CommandQuit
)

// Command carries exactly one UI-to-Host command.
type Command struct {
	Kind CommandKind
	Text string
}
