package extensionv1

import (
	"context"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// AbortRunOperation reports acceptance of agent-run cancellation independently of run settlement.
type AbortRunOperation struct {
	// operation owns correlated Host lifecycle events.
	operation *contextOperation
}

// StartAbortRun requests cancellation of the current agent run without waiting for Host acceptance.
func (c *ExtensionContext) StartAbortRun(ctx context.Context) (*AbortRunOperation, error) {
	request := new(extensionpb.ExtensionRequest)
	request.SetAbortRun(extensionpb.AbortRunRequest_builder{Context: c.reference()}.Build())
	started, err := c.start(ctx, request)
	if err != nil {
		return nil, err
	}
	return &AbortRunOperation{operation: started}, nil
}

// Wait reports cancellation acceptance without waiting for the agent run to end.
func (o *AbortRunOperation) Wait(ctx context.Context) error {
	_, err := o.operation.wait(ctx, nil)
	return err
}
