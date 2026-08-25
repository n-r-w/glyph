// Package presentation defines process-local state derived from Host UI frames.
package presentation

// Availability controls which user commands the presentation may emit.
type Availability uint8

const (
	// AvailabilityUnspecified represents a missing Host availability value.
	AvailabilityUnspecified Availability = iota
	// AvailabilityChecking means the Host is checking stored authentication.
	AvailabilityChecking
	// AvailabilityAuthenticating means browser authentication is in progress.
	AvailabilityAuthenticating
	// AvailabilityAuthenticationFailed allows a manual authentication retry.
	AvailabilityAuthenticationFailed
	// AvailabilityIdle allows a new request.
	AvailabilityIdle
	// AvailabilityRunning allows a stop request.
	AvailabilityRunning
)

// ReasoningLevel identifies one configured reasoning level.
type ReasoningLevel uint8

const (
	// ReasoningLevelUnspecified represents a missing reasoning level.
	ReasoningLevelUnspecified ReasoningLevel = iota
	// ReasoningLevelNone disables reasoning effort.
	ReasoningLevelNone
	// ReasoningLevelMinimal requests minimal reasoning effort.
	ReasoningLevelMinimal
	// ReasoningLevelLow requests low reasoning effort.
	ReasoningLevelLow
	// ReasoningLevelMedium requests medium reasoning effort.
	ReasoningLevelMedium
	// ReasoningLevelHigh requests high reasoning effort.
	ReasoningLevelHigh
	// ReasoningLevelXHigh requests extra-high reasoning effort.
	ReasoningLevelXHigh
	// ReasoningLevelMax requests maximum reasoning effort.
	ReasoningLevelMax
)

// ConfiguredModel identifies one selectable model and its reasoning order.
type ConfiguredModel struct {
	ProviderID      string
	ModelID         string
	ReasoningLevels []ReasoningLevel
}

// ModelSelection identifies the Host-confirmed active selection.
type ModelSelection struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel ReasoningLevel
}

// EventKind identifies one provider-neutral Host presentation event.
type EventKind uint8

const (
	// EventUnspecified represents a missing presentation event kind.
	EventUnspecified EventKind = iota
	// EventInitialization carries the first Host frame.
	EventInitialization
	// EventUserSubmitted records a successfully sent user request.
	EventUserSubmitted
	// EventAvailability changes command availability.
	EventAvailability
	// EventTurnStarted marks a new active turn.
	EventTurnStarted
	// EventModelDelta appends incremental model text.
	EventModelDelta
	// EventModelEnd settles one model text position.
	EventModelEnd
	// EventToolCallPreview replaces one provisional function-call preview.
	EventToolCallPreview
	// EventToolCallFinal replaces one preview with exact final arguments.
	EventToolCallFinal
	// EventToolStarted records tool identity.
	EventToolStarted
	// EventToolProgress records tool status text.
	EventToolProgress
	// EventToolOutput records tool output text.
	EventToolOutput
	// EventToolEnded records execution completion.
	EventToolEnded
	// EventToolResult records the terminal tool result.
	EventToolResult
	// EventTurnEnded records a terminal turn failure when present.
	EventTurnEnded
	// EventAgentSettled marks the agent run as settled.
	EventAgentSettled
	// EventAuthorization presents a browser authorization URL.
	EventAuthorization
	// EventInformation presents safe informational text.
	EventInformation
	// EventError presents safe error text.
	EventError
	// EventModelSelectionChanged confirms one committed selection.
	EventModelSelectionChanged
)

// ModelContentKind identifies one visible model content block.
type ModelContentKind uint8

const (
	// ModelContentUnspecified represents a missing model content kind.
	ModelContentUnspecified ModelContentKind = iota
	// ModelContentText contains ordinary model text.
	ModelContentText
	// ModelContentRefusal contains model refusal text.
	ModelContentRefusal
)

// ModelResponseContent carries one finalized visible model content block.
type ModelResponseContent struct {
	Kind ModelContentKind
	Text string
}

// ActiveModelContent carries one streaming visible model content block.
type ActiveModelContent struct {
	Kind ModelContentKind
	Text string
}

// OutputStream identifies readable tool output without exposing tool internals.
type OutputStream uint8

const (
	// OutputUnspecified represents a missing tool output stream.
	OutputUnspecified OutputStream = iota
	// OutputStdout identifies standard tool output.
	OutputStdout
	// OutputStderr identifies tool error output.
	OutputStderr
)

// Extension describes startup information received from the Host.
type Extension struct {
	ID    string
	Path  string
	Tools []string
}

// ToolResultContent is one terminal tool result block received from the Host.
type ToolResultContent struct {
	Text      string
	MediaType string
	Data      []byte
}

// Event contains the fields used by one presentation update.
type Event struct {
	Kind                 EventKind
	Startup              []Line
	Extensions           []Extension
	Availability         Availability
	Position             int
	ModelContentKind     ModelContentKind
	ModelResponseContent []ModelResponseContent
	ToolCallID           string
	ToolName             string
	Status               string
	Stream               OutputStream
	Text                 string
	ToolResultContents   []ToolResultContent
	ErrorText            string
	ExitCode             int
	Failure              bool
	ToolCall             ToolCallState
	Models               []ConfiguredModel
	ModelSelection       ModelSelection
}

// LineKind controls the plain prefix used to render one transcript line.
type LineKind uint8

const (
	// LineUnspecified represents a missing transcript line kind.
	LineUnspecified LineKind = iota
	// LineInformation renders safe informational text.
	LineInformation
	// LineError renders safe error text.
	LineError
	// LineWarning renders non-fatal startup exclusions.
	LineWarning
	// LineUser renders submitted user text.
	LineUser
	// LineModel renders model text.
	LineModel
	// LineRefusal renders model refusal text.
	LineRefusal
	// LineToolStatus renders tool status text.
	LineToolStatus
	// LineToolStdout renders standard tool output.
	LineToolStdout
	// LineToolStderr renders tool error output.
	LineToolStderr
	// LineToolDone renders successful tool completion.
	LineToolDone
	// LineToolError renders failed tool completion.
	LineToolError
)

// Line is one readable startup or transcript entry.
type Line struct {
	Kind               LineKind
	ToolName           string
	Status             string
	Text               string
	ToolResultContents []ToolResultContent
}

// ToolCallField is one rendered argument field.
type ToolCallField struct {
	Name     string
	Value    any
	Prefix   string
	Complete bool
}

// ToolCallState is one transient or finalized function call.
type ToolCallState struct {
	CallID      string
	Name        string
	Position    int
	Provisional bool
	Fields      []ToolCallField
	Arguments   map[string]any
}

// State is the TUI-owned projection of provider-neutral Host frames.
type State struct {
	Startup          []Line
	Transcript       []Line
	ActiveModel      map[int]ActiveModelContent
	ActiveToolCalls  map[string]ToolCallState
	ActiveTools      map[string]string
	Availability     Availability
	AuthorizationURL string
	Settled          bool
	Models           []ConfiguredModel
	ModelSelection   ModelSelection
}

// CommandKind identifies one accepted command sent to the Host.
type CommandKind uint8

const (
	// CommandUnspecified represents a missing UI command kind.
	CommandUnspecified CommandKind = iota
	// CommandSubmit sends one user request.
	CommandSubmit
	// CommandStop requests cancellation of the active run.
	CommandStop
	// CommandRetryAuthentication requests a new authentication attempt.
	CommandRetryAuthentication
	// CommandQuit requests UI-mode termination.
	CommandQuit
	// CommandSelectModel requests one configured model.
	CommandSelectModel
	// CommandSelectReasoningLevel requests one reasoning level.
	CommandSelectReasoningLevel
)

// Command is one user request emitted through the UI stream.
type Command struct {
	Kind           CommandKind
	Text           string
	ProviderID     string
	ModelID        string
	ReasoningLevel ReasoningLevel
}
