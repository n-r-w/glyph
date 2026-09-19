package programmatic

import (
	"context"
	"time"

	controller "github.com/n-r-w/glyph/host/internal/controller/programmatic"
	"github.com/n-r-w/glyph/internal/operation"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=programmatic

// RetryControl owns runtime retry enablement and one atomic policy projection.
type RetryControl interface {
	// SetRetryEnabled changes enablement for logical executions that start later.
	SetRetryEnabled(enabled bool)
	// RetryPolicy returns one detached effective policy snapshot.
	RetryPolicy() (enabled bool, maxRetries int64, delays []time.Duration, maxProviderDelay time.Duration)
}

// StateQuery supplies only the Core activity needed by the public run-state query.
type StateQuery interface {
	// RunActive reports running or awaiting-settlement state under the Core state lock.
	RunActive() bool
}

// RunOutput owns the active output correlation for admitted runs.
type RunOutput interface {
	// Reserve associates the public operation identifier with one prepared run.
	Reserve(operationID, runID string) bool
	// ActiveOperation returns the correlated operation identifier, or an empty string when idle.
	ActiveOperation() string
	// BindProgress attaches one admitted operation's reporter before Core execution.
	BindProgress(runID string, reporter operation.Reporter[controller.OperationProgress]) func()
	// CancelPrepared clears an association whose application work never started.
	CancelPrepared(runID string)
}

// Coordinator owns Host run identifiers, execution, and settlement.
type Coordinator interface {
	PrepareRun() (string, error)
	CancelPrepared(runID string)
	RunPrepared(ctx context.Context, runID, userText string) (agent.RunOutcome, error)
}

// ActiveSessions provides client session lifecycle operations.
type ActiveSessions interface {
	// ClientSnapshot returns canonical conversation history with client visibility applied.
	ClientSnapshot() []agent.HistoryEntry
	// CreateActive replaces active state while the caller owns the mutation gate.
	CreateActive() (session.Info, []session.Entry, error)
	// ResumeActive validates and replaces active state while the caller owns the mutation gate.
	ResumeActive(context.Context, session.ID) (session.Info, []session.Entry, error)
	// SetActiveName persists a normalized active-session name while the caller owns the mutation gate.
	SetActiveName(context.Context, string) (session.Info, error)
	// ListProgrammaticSessions returns ordered persisted-session summaries.
	ListProgrammaticSessions(context.Context) ([]StoredSession, error)
	// ActiveInfo returns the current active-session snapshot.
	ActiveInfo() session.Info
	// ActiveEntries returns immutable active-session records.
	ActiveEntries() []session.Entry
	// ActiveStatistics returns active-session counts and complete token totals.
	ActiveStatistics() session.Statistics
	// Tree returns the complete active-session tree snapshot.
	Tree() session.Tree
	// ForkActive creates a replacement while the caller owns the mutation gate.
	ForkActive(context.Context, string) (session.Info, []session.Entry, string, error)
	// CloneActive creates a copy while the caller owns the mutation gate.
	CloneActive(context.Context) (session.Info, []session.Entry, error)
	// SetLabel persists a label while the caller owns the mutation gate.
	SetLabel(context.Context, string, string) (session.Tree, error)
}

// SelectionCode identifies a model catalog selection failure.
type SelectionCode string

const (
	// SelectionNotFound reports an unknown provider and model pair.
	SelectionNotFound SelectionCode = "not_found"
	// SelectionReasoningUnsupported reports an unsupported reasoning choice.
	SelectionReasoningUnsupported SelectionCode = "reasoning_unsupported"
	// SelectionCredentialUnavailable reports unavailable selection credentials.
	SelectionCredentialUnavailable SelectionCode = "credential_unavailable" //nolint:gosec // This is an error code.
	// SelectionBusy reports occupied shared selection admission.
	SelectionBusy SelectionCode = "busy"
	// SelectionModelUnavailable reports an invalid final target.
	SelectionModelUnavailable SelectionCode = "model_unavailable"
	// SelectionExtensionRejected reports an explicit handler rejection.
	SelectionExtensionRejected SelectionCode = "extension_rejected"
	// SelectionExtensionUnavailable reports selected runtime loss.
	SelectionExtensionUnavailable SelectionCode = "extension_unavailable"
)

// SelectionFailure exposes a stable typed catalog failure.
type SelectionFailure interface {
	error
	ModelSelectionCode() string
}

// ModelCatalog provides configured models and the active selection snapshot.
type ModelCatalog interface {
	Models() []model.Descriptor
	ActiveSelection() model.Selection
}

// ModelSelectionCommandKind identifies one Programmatic selection request shape.
type ModelSelectionCommandKind uint8

const (
	// ModelSelectionCommandModel requests one provider and model target.
	ModelSelectionCommandModel ModelSelectionCommandKind = iota + 1
	// ModelSelectionCommandReasoning requests one reasoning choice for the active model.
	ModelSelectionCommandReasoning
)

// ModelSelectionCommand contains one validated Programmatic selection request.
type ModelSelectionCommand struct {
	// Kind identifies the selected request shape.
	Kind ModelSelectionCommandKind
	// Provider identifies the requested provider for a model request.
	Provider model.ProviderID
	// Model identifies the requested model for a model request.
	Model model.ID
	// ReasoningChoice identifies the requested reasoning choice for a reasoning request.
	ReasoningChoice model.ReasoningChoice
}

// ModelSelectionResult contains one shared selection execution outcome projected for Programmatic Control.
type ModelSelectionResult struct {
	// Selection is the committed selection when Committed is true.
	Selection model.Selection
	// Committed reports whether authoritative selection state was committed.
	Committed bool
	// Issues contains ordered public diagnostics for a committed result.
	Issues []ModelSelectionIssue
	// Source preserves complete diagnostic or terminal failure causes.
	Source error
}

// PreparedModelSelection owns one admitted Programmatic selection until release.
type PreparedModelSelection interface {
	// Run executes handler composition, final validation, commit, and publication.
	Run(context.Context) ModelSelectionResult
	// Release frees shared selection admission exactly once.
	Release()
}

// ModelSelection prepares all Programmatic selection changes through the shared Host owner.
type ModelSelection interface {
	// PrepareProgrammaticSelection validates and reserves one consumer-owned selection command.
	PrepareProgrammaticSelection(ModelSelectionCommand) (PreparedModelSelection, error)
}

// Gate reserves session mutations before operation acceptance.
type Gate interface {
	// TryAcquire returns a release function when the shared reservation is available.
	TryAcquire() (func(), bool)
}
