package extensionv1

import (
	"context"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// parentInvocationContextKey binds preparation to the enclosing Host envelope identifier.
type parentInvocationContextKey struct{}

// start preserves invocation ownership independently of the caller's cancellation context.
func (c *ExtensionContext) start(
	ctx context.Context,
	request *extensionpb.ExtensionRequest,
) (*contextOperation, error) {
	request.SetParentInvocationId(c.parentID)
	if c.compactionID != "" {
		request.SetParentCompactionId(c.compactionID)
	}
	return c.initiator.start(ctx, request)
}
