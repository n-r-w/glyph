package extensionv1

import (
	"context"
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/internal/operation"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

//go:generate go tool mockgen -source=host_service.go -destination=host_service_mock_test.go -package=extensionv1

// HostService admits extension-initiated work on a connected runtime's stream.
type HostService interface {
	// Prepare validates a catalog request before acceptance.
	Prepare(ctx context.Context, operationID string, request *extensionpb.ExtensionRequest) (HostOperation, error)
}

// HostOperation owns one admitted catalog operation.
type HostOperation interface {
	// Run performs work outside stream receipt and reports typed progress.
	Run(context.Context, *HostProgressReporter) (*extensionpb.HostCompleted, error)
	// Release releases the runtime's active-operation reservation.
	Release()
}

// HostProgressReporter delivers extension-initiated Host operation progress.
type HostProgressReporter struct {
	// reporter is the shared asynchronous operation reporter.
	reporter operation.Reporter[*extensionpb.HostProgress]
}

// Report queues one configured-model retry progress event.
func (r *HostProgressReporter) Report(ctx context.Context, progress *extensionpb.HostProgress) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("report Host progress: %w", err)
	}
	if progress == nil || progress.GetConfiguredModelRetry() == nil {
		return errors.New("report Host progress: configured-model retry progress is required")
	}
	if err := r.reporter.Report(progress); err != nil {
		return fmt.Errorf("report Host progress: %w", err)
	}
	return nil
}

// BindHostService binds runtime-specific Host dispatch before extension registration.
func (c *Connection) BindHostService(service HostService) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.hostService = service
}
