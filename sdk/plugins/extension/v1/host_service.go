package extensionv1

import (
	"context"

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
	// Run performs the read outside stream receipt and returns its typed result.
	Run(context.Context) (*extensionpb.HostCompleted, error)
	// Release releases the runtime's active-operation reservation.
	Release()
}

// BindHostService binds runtime-specific Host dispatch before extension registration.
func (c *Connection) BindHostService(service HostService) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.hostService = service
}
