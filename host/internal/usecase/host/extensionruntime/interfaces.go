package extensionruntime

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
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
	// Register invokes extension registration and returns its raw result.
	Register(ctx context.Context) (Registration, error)
	// Handle invokes one session-tree handler operation.
	Handle(
		ctx context.Context,
		handlerID string,
		request HandlerInvocation,
	) (HandlerAction, error)
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
	// Close stops the extension process and releases its resources.
	Close()
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
