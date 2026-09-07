package plugin

import "github.com/samber/mo"

// AgentKind identifies a model or tool lifecycle update.
type AgentKind uint8

const (
	// AgentUnspecified identifies a missing lifecycle update.
	AgentUnspecified AgentKind = iota
	// AgentTurnStarted starts provisional turn state.
	AgentTurnStarted
	// AgentModelDelta carries one streamed model fragment.
	AgentModelDelta
	// AgentModelEnd carries complete model content.
	AgentModelEnd
	// AgentToolCallPreview carries provisional tool arguments.
	AgentToolCallPreview
	// AgentToolCallFinal carries complete tool arguments.
	AgentToolCallFinal
	// AgentToolStarted begins tool execution.
	AgentToolStarted
	// AgentToolProgress carries a tool status update.
	AgentToolProgress
	// AgentToolOutput carries tool stream text.
	AgentToolOutput
	// AgentToolEnded records execution completion.
	AgentToolEnded
	// AgentToolResult carries the complete tool result.
	AgentToolResult
	// AgentTurnEnded records turn completion.
	AgentTurnEnded
)

// AgentUpdate carries validated lifecycle data without session or initialization payloads.
type AgentUpdate struct {
	// Kind identifies the lifecycle meaning.
	Kind AgentKind
	// Position identifies a streamed model block.
	Position mo.Option[int]
	// ModelContentKind identifies a streamed block's content.
	ModelContentKind mo.Option[ModelContentKind]
	// ModelResponseContent contains completed model blocks.
	ModelResponseContent []ModelResponseContent
	// ToolCallID identifies the tool invocation.
	ToolCallID mo.Option[string]
	// ToolName identifies the executing tool.
	ToolName mo.Option[string]
	// Status preserves a reported lifecycle status.
	Status mo.Option[string]
	// Stream identifies tool output.
	Stream mo.Option[OutputStream]
	// Text contains the reported text fragment.
	Text mo.Option[string]
	// Contents contains complete tool result content.
	Contents mo.Option[[]Content]
	// ErrorText retains an optional lifecycle diagnostic.
	ErrorText mo.Option[string]
	// ExitCode retains an optional command exit code.
	ExitCode mo.Option[int]
	// Failure identifies unsuccessful completion when present.
	Failure mo.Option[bool]
	// ToolCall contains validated tool arguments and identity.
	ToolCall mo.Option[ToolCallState]
}

// TextKind identifies the source meaning of a text update.
type TextKind uint8

const (
	// TextInformation carries a Host information message.
	TextInformation TextKind = iota
	// TextAuthorization carries an authorization URL.
	TextAuthorization
	// TextError carries a connection diagnostic.
	TextError
)

// FailureCodePersistence identifies source-classified history-persistence failures.
const FailureCodePersistence = "PERSISTENCE_UNAVAILABLE"

// TextUpdate carries a text input without choosing where the application displays it.
type TextUpdate struct {
	// Kind identifies the source message meaning.
	Kind TextKind
	// Text is the complete diagnostic or URL.
	Text string
	// FailureCode retains the source category for diagnostics and is empty for other text.
	FailureCode string
}

// SessionKind identifies a validated session result.
type SessionKind uint8

const (
	// SessionListed carries stored-session choices.
	SessionListed SessionKind = iota
	// SessionChanged carries confirmed session replacement.
	SessionChanged
	// SessionInformation carries active-session metadata and accounting.
	SessionInformation
)

// SessionUpdate carries one session result without agent or tree-operation fields.
type SessionUpdate struct {
	// Kind identifies the completed session query or mutation.
	Kind SessionKind
	// Info contains the committed session metadata when present.
	Info mo.Option[SessionInfo]
	// Sessions contains stored-session choices.
	Sessions []SessionSummary
	// Transcript contains restored branch entries.
	Transcript []Transcript
	// Statistics contains active-session accounting when present.
	Statistics mo.Option[SessionStatistics]
}

// TreeKind identifies tree progress or a completed tree operation.
type TreeKind uint8

const (
	// TreeSnapshot carries a query result.
	TreeSnapshot TreeKind = iota
	// TreeNavigationProgress carries a committed branch before terminal metadata.
	TreeNavigationProgress
	// TreeNavigationCompleted carries terminal navigation metadata.
	TreeNavigationCompleted
	// TreeForked carries a confirmed fork.
	TreeForked
	// TreeCloned carries a confirmed clone.
	TreeCloned
	// TreeLabelSet carries a committed label mutation.
	TreeLabelSet
	// TreeEntryAdded carries a committed extension entry.
	TreeEntryAdded
)

// TreeUpdate carries validated tree facts without failure-display or interaction state.
type TreeUpdate struct {
	// Kind identifies the tree operation stage.
	Kind TreeKind
	// Tree contains a committed tree when supplied by Host.
	Tree mo.Option[SessionTree]
	// NavigationStatus distinguishes committed and canceled navigation.
	NavigationStatus TreeNavigationStatus
	// SessionInfo identifies a confirmed replacement session.
	SessionInfo mo.Option[SessionInfo]
	// Transcript contains a committed active branch or added visible entry.
	Transcript []Transcript
	// NextInput preserves terminal editor metadata exactly.
	NextInput mo.Option[string]
	// Issues contains ordered operation diagnostics.
	Issues []OperationIssue
	// AddedEntry contains a committed connection-event entry.
	AddedEntry mo.Option[TreeEntry]
}
