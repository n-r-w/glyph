// Package extensioncontext owns issued extension bindings and session-bound Host capabilities.
package extensioncontext

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extensioncontext

// RuntimeState supplies the currently accepted process identity.
type RuntimeState interface {
	// ContextRuntime returns the runtime instance and its availability.
	ContextRuntime(extensionID string) (instanceID string, available bool)
	// BeginContextCommit protects one final state-owner commit from runtime invalidation.
	BeginContextCommit(extensionID, runtimeID string) (release func(), err error)
}

// SessionState supplies one atomic active-session identity snapshot.
type SessionState interface {
	// ContextSession returns durable identity, project directory, and process-local incarnation.
	ContextSession() session.Identity
	// AppendExtension appends one entry only while the expected session incarnation remains active.
	AppendExtension(
		context.Context,
		session.Identity,
		session.ExtensionEnvelope,
		ContextCommitGuard,
	) (session.Entry, error)
	// AppendExtensionMessage persists and publishes one message under the bound session incarnation.
	AppendExtensionMessage(
		context.Context,
		session.Identity,
		session.ExtensionMessage,
		ContextCommitGuard,
	) (session.Entry, error)
	// ExtensionState returns one coherent filtered active-branch snapshot.
	ExtensionState(context.Context, session.Identity, string) (SessionSnapshot, error)
	// ProtectContextCommit validates and protects the expected session before runtime while commit runs.
	ProtectContextCommit(context.Context, session.Identity, ContextCommitGuard, func() error) error
}

// ContextCommitGuard acquires runtime validity across one owning session commit.
type ContextCommitGuard func() (release func(), err error)

// SessionSnapshot contains one coherent active-branch view from the session consumer boundary.
type SessionSnapshot struct {
	// SessionID identifies the active durable session read with the entries.
	SessionID session.ID
	// ActiveLeafID identifies the active leaf read with the entries.
	ActiveLeafID mo.Option[string]
	// Entries contains matching extension entries in root-first branch order.
	Entries []session.Entry
}

// CompactionSnapshot contains the captured session input for one replacement operation.
type CompactionSnapshot struct {
	// Identity identifies the session incarnation.
	Identity session.Identity
	// ActiveLeafID identifies the captured branch position.
	ActiveLeafID mo.Option[string]
	// Entries contains detached original branch entries.
	Entries []session.Entry
	// History contains the prepared model input.
	History []agent.HistoryEntry
	// Previous contains the preceding applicable replacement.
	Previous mo.Option[session.CompactionEntry]
}

// PolicyOwner identifies one registered policy invocation target.
type PolicyOwner struct {
	// ExtensionID identifies the policy process.
	ExtensionID string
	// RuntimeID identifies its captured generation.
	RuntimeID string
	// HandlerID identifies the registered policy handler.
	HandlerID string
}

// PolicyInvocation supplies the immutable input and scoped replacement authority.
type PolicyInvocation struct {
	// Purpose identifies the caller's preparation intent.
	Purpose PolicyPurpose
	// OverflowError contains the rejected provider attempt's permitted error text.
	OverflowError mo.Option[string]
	// OperationID identifies the parent replacement operation.
	OperationID string
	// LifecycleID identifies the non-authorizing parent lifetime.
	LifecycleID string
	// Snapshot contains the captured session input.
	Snapshot CompactionSnapshot
	// Input contains the captured outbound model selection, instructions, and tools.
	Input modelexecution.ProviderRequest
	// Requests contains captured request participants in registration order.
	Requests []Participant
	// Generators contains captured generator registrations.
	Generators []Participant
	// Results contains captured result capabilities.
	Results []Participant
	// Instructions contains the caller's manual guidance.
	Instructions mo.Option[string]
	// RecordState retains acquired diagnostic state independently of the durable receipt.
	RecordState func(contextcompaction.PolicyState)
}

// ConversationObservation contains the completed provider attempt and nested-work correlation.
type ConversationObservation struct {
	// Input contains the actual successful attempt request.
	Input modelexecution.ProviderRequest
	// Response contains the complete provider response.
	Response model.Response
	// LifecycleID is the correlation ID for ordinary nested work.
	LifecycleID string
}

// PolicyRuntime supplies registered policy identity and asynchronous invocation.
type PolicyRuntime interface {
	// ObserveConversation invokes the captured policy owner's completed-conversation handler.
	ObserveConversation(context.Context, PolicyOwner, extension.Context, ConversationObservation) error
	// InvokeCompactionResult invokes one captured result participant.
	InvokeCompactionResult(context.Context, Participant, extension.Context, ResultInvocation) (ResultAction, error)
	// InvokeCompactionGenerator invokes one captured generator.
	InvokeCompactionGenerator(context.Context, Participant, extension.Context, RequestInvocation) (Result, error)
	// InvokeCompactionRequest invokes one captured policy request participant.
	InvokeCompactionRequest(
		context.Context,
		Participant,
		extension.Context,
		RequestInvocation,
	) (RequestAction, error)
	// SnapshotCompactionHandlers captures registered observer generations in order.
	SnapshotCompactionHandlers() contextcompaction.HandlerSet
	// ObserveCompactionSuccess invokes one captured committed-outcome observer.
	ObserveCompactionSuccess(
		context.Context,
		contextcompaction.Handler,
		extension.Context,
		contextcompaction.OutcomeInvocation,
	) error
	// ObserveCompactionFailure invokes one captured failure-outcome observer.
	ObserveCompactionFailure(
		context.Context,
		contextcompaction.Handler,
		extension.Context,
		contextcompaction.OutcomeInvocation,
	) error
	// SnapshotCompactionPolicies captures registered policy generations.
	SnapshotCompactionPolicies() []PolicyOwner
	// InvokeCompactionPolicy invokes the captured policy handler.
	InvokeCompactionPolicy(context.Context, PolicyOwner, extension.Context, PolicyInvocation) (PolicyDisposition, error)
	// ValidateOutgoingInput asks the captured owner to accept frozen conversation input.
	ValidateOutgoingInput(context.Context, PolicyOwner, extension.Context, OutgoingInput) error
}

// CompactionSessions supplies session capture, candidate projection, and durable replacement.
type CompactionSessions interface {
	// CaptureCompaction captures one detached branch and prepared history.
	CaptureCompaction() CompactionSnapshot
	// ProjectReplacement validates and projects a candidate against captured input.
	ProjectReplacement(CompactionSnapshot, session.CompactionEntry) ([]agent.HistoryEntry, error)
	// CommitReplacement persists a candidate under runtime protection and records its receipt.
	CommitReplacement(
		context.Context,
		CompactionSnapshot,
		session.CompactionEntry,
		ContextCommitGuard,
		func(session.Entry),
	) (session.Entry, error)
}
