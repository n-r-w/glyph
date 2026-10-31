package extensionruntime

import (
	"context"
	"errors"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extensionruntime

// ErrExtensionUnavailable marks process, transport, or protocol failure that invalidates a runtime.
var ErrExtensionUnavailable = errors.New("extension runtime unavailable")

// Directory identifies the path inspected by the extension catalog.
type Directory struct {
	// Path is the effective extension catalog directory.
	Path string
}

// Executable contains one normalized filesystem observation before runtime binding.
type Executable struct {
	// ID contains the normalized executable name, including an empty name before acceptance.
	ID string
	// Path identifies the inspected executable.
	Path string
}

// Candidate binds one accepted executable to its trusted process incarnation.
type Candidate struct {
	// ID identifies the extension plugin.
	ID string
	// Path is the extension executable path.
	Path string
	// InstanceID is assigned by the runtime owner before this process starts.
	InstanceID string
}

// Issue reports one isolated catalog or runtime failure.
type Issue struct {
	// PluginIDs identifies affected extension plugins.
	PluginIDs []string
	// Path identifies the failed catalog entry.
	Path string
	// Err contains the isolated discovery or runtime failure.
	Err error
}

// Discovery contains filesystem observations before Host acceptance.
type Discovery struct {
	// Candidates contains all executable observations in filesystem name order.
	Candidates []Executable
	// Issues contains filesystem entry failures.
	Issues []Issue
	// DirectoryError contains the complete directory-read failure, when present.
	DirectoryError error
}

// Catalog discovers executable extension candidates.
type Catalog interface {
	// Discover finds executable extension candidates in one directory.
	Discover(ctx context.Context, directory Directory) (Discovery, error)
}

// ExtensionRuntime is one independently managed extension process.
type ExtensionRuntime interface {
	// HandleWire invokes one ordinary provider-wire operation.
	HandleWire(context.Context, string, middleware.Invocation) (middleware.Action, error)
	// HandleSystemInstructions invokes one system-instructions transformer.
	HandleSystemInstructions(
		context.Context, string, middleware.Transformation[string],
	) (middleware.Transformed[string], error)
	// HandleModelContext invokes one request-local history transformer and restores retained private replay.
	HandleModelContext(
		context.Context, string, middleware.Transformation[[]agent.HistoryEntry],
	) (middleware.Transformed[[]agent.HistoryEntry], error)
	// HandleUserInput invokes one user-input handler.
	HandleUserInput(
		context.Context, string, middleware.Transformation[model.Message],
	) (middleware.Transformed[model.Message], error)
	// HandleFinalizedMessage invokes one finalized-message transformer and restores retained private replay.
	HandleFinalizedMessage(
		context.Context, string, middleware.Transformation[agent.HistoryEntry],
	) (middleware.Transformed[agent.HistoryEntry], error)
	// BeginInvocationChild admits work owned by one live connection-local Host invocation.
	BeginInvocationChild(parentID string) (extensioncontroller.InvocationChild, error)
	// Register invokes extension registration and returns its raw result.
	Register(ctx context.Context) (Registration, error)
	// Handle invokes one session-tree handler operation.
	Handle(
		ctx context.Context,
		handlerID string,
		request HandlerInvocation,
	) (HandlerAction, error)
	// HandleCompactionRequest invokes one compaction request handler.
	HandleCompactionRequest(
		context.Context,
		string,
		extension.Context,
		extensioncontext.RequestInvocation,
	) (extensioncontext.RequestAction, error)
	// GenerateCompaction invokes one compaction generation capability.
	GenerateCompaction(
		context.Context,
		string,
		extension.Context,
		extensioncontext.RequestInvocation,
	) (extensioncontext.Result, error)
	// HandleCompactionResult invokes one compaction result handler.
	HandleCompactionResult(
		context.Context,
		string,
		extension.Context,
		extensioncontext.ResultInvocation,
	) (extensioncontext.ResultAction, error)
	// ObserveCompactionSuccess invokes one committed-compaction observer.
	ObserveCompactionSuccess(context.Context, string, extension.Context, extensioncontext.OutcomeInvocation) error
	// ObserveCompactionFailure invokes one failed-compaction observer.
	ObserveCompactionFailure(context.Context, string, extension.Context, extensioncontext.OutcomeInvocation) error
	// ObserveConversation invokes the policy handler for a completed conversation.
	ObserveConversation(context.Context, string, extension.Context, extensioncontext.ConversationObservation) error
	// InvokeCompactionPolicy invokes one captured compaction policy capability.
	InvokeCompactionPolicy(
		context.Context,
		string,
		extension.Context,
		extensioncontext.PolicyInvocation,
	) (extensioncontext.PolicyDisposition, error)
	// ValidateOutgoingInput invokes the captured policy's final input check.
	ValidateOutgoingInput(context.Context, string, extension.Context, extensioncontext.OutgoingInput) error
	// ObserveLifecycle invokes one Agent Core lifecycle observer.
	ObserveLifecycle(context.Context, string, LifecycleInvocation) error
	// Execute invokes one tool operation.
	Execute(
		ctx context.Context,
		name string,
		argumentsJSON []byte,
		handleProgress tool.ProgressHandler,
		binding extension.Context,
	) (tool.Result, error)
	// Done closes when the extension process exits.
	Done() <-chan struct{}
	// Close joins resources and returns retained undelivered sources and independent cleanup failures.
	Close() error
}

// RuntimeFactory starts one candidate.
type RuntimeFactory interface {
	// Start creates one runtime for the candidate.
	Start(ctx context.Context, candidate Candidate) (ExtensionRuntime, error)
}

// FailureReporter delivers mode-specific diagnostics after runtime management classifies a failure.
type FailureReporter interface {
	// ReportRuntimeFailure preserves the complete runtime failure at the selected mode output.
	ReportRuntimeFailure(context.Context, extension.RuntimeFailure) error
}
