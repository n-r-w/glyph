package programmatic

import (
	"context"

	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=programmatic

// ConnectionOutput binds unsolicited output to the controller's one ordered writer.
type ConnectionOutput interface {
	// BindWriter attaches output for this connection and returns its detach action.
	BindWriter(*operation.Writer[*programmaticv1.OpenResponse]) func()
}

// HostSession prepares transport-independent Programmatic operations.
type HostSession interface {
	Prepare(ctx context.Context, command Command) (operation.Prepared[OperationProgress, Response], error)
}

// OpenStream is the gRPC stream surface used by the controller.
type OpenStream interface {
	Context() context.Context
	Recv() (*programmaticv1.OpenRequest, error)
	Send(*programmaticv1.OpenResponse) error
}
