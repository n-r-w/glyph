package extensionruntime

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extensionruntime

// ErrExtensionUnavailable marks process, transport, or protocol failure that invalidates a runtime.
var ErrExtensionUnavailable = errors.New("extension runtime unavailable")

// Directory identifies the effective extension catalog and its failure policy.
type Directory struct {
	// Path is the effective extension catalog directory.
	Path string
	// Explicit reports whether the invocation supplied Path.
	Explicit bool
}

// Candidate is one normalized executable extension candidate.
type Candidate struct {
	// ID identifies the extension plugin.
	ID string
	// Path is the extension executable path.
	Path string
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

// Discovery is one filtered extension catalog.
type Discovery struct {
	// Candidates contains valid executable extension plugins.
	Candidates []Candidate
	// Issues contains isolated catalog failures.
	Issues []Issue
}

// Catalog discovers executable extension candidates.
type Catalog interface {
	// Discover finds executable extension candidates in one directory.
	Discover(ctx context.Context, directory Directory) (Discovery, error)
}

// ExtensionRuntime is one independently managed extension process.
type ExtensionRuntime interface {
	// Register invokes extension registration and returns its raw result.
	Register(ctx context.Context) (startup.PendingRegistration, error)
	// Handle invokes one session-tree handler operation.
	Handle(
		ctx context.Context,
		handlerID string,
		request sessiontree.HandlerRequest,
	) (sessiontree.HandlerResponse, error)
	// Execute invokes one tool operation.
	Execute(
		ctx context.Context,
		name string,
		argumentsJSON []byte,
		handleProgress tool.ProgressHandler,
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
