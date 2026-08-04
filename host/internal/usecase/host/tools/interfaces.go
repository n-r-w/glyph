package tools

import (
	"context"
	"errors"

	"github.com/n-r-w/glyph/host/internal/domain/tool"
)

// ErrExtensionUnavailable marks process, transport, or protocol failure that invalidates a runtime.
var ErrExtensionUnavailable = errors.New("extension runtime unavailable")

// Directory identifies the effective extension catalog and its failure policy.
type Directory struct {
	Path     string
	Explicit bool
}

// Candidate is one normalized executable extension candidate.
type Candidate struct {
	ID   string
	Path string
}

// Issue reports one isolated catalog or runtime failure.
type Issue struct {
	PluginIDs []string
	Path      string
	Err       error
}

// Discovery is one filtered extension catalog.
type Discovery struct {
	Candidates []Candidate
	Issues     []Issue
}

// LoadedExtension identifies one available extension and its owned tools.
type LoadedExtension struct {
	ID    string
	Path  string
	Tools []tool.Descriptor
}

// LoadReport contains isolated failures and every available loaded extension.
type LoadReport struct {
	Issues     []Issue
	Extensions []LoadedExtension
}

// Catalog discovers executable extension candidates.
type Catalog interface {
	Discover(ctx context.Context, directory Directory) (Discovery, error)
}

// ExtensionRuntime is one independently managed extension process.
type ExtensionRuntime interface {
	ListTools(ctx context.Context) ([]tool.Descriptor, error)
	Execute(
		ctx context.Context,
		name string,
		argumentsJSON []byte,
		handleProgress tool.ProgressHandler,
	) (tool.Result, error)
	Done() <-chan struct{}
	Close()
}

// RuntimeFactory starts one candidate.
type RuntimeFactory interface {
	Start(ctx context.Context, candidate Candidate) (ExtensionRuntime, error)
}

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=tools
