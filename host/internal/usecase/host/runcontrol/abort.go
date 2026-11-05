package runcontrol

import (
	"context"
	"fmt"

	"github.com/n-r-w/glyph/internal/errtree/diagnostic"
)

// extensionAbortFormat identifies the extension that requested agent-run cancellation.
const extensionAbortFormat = "agent run aborted by extension %q"

// CheckAbort captures the active run identity for a bound extension request.
func (c *Coordinator) CheckAbort() (string, error) {
	return c.executor.CheckEnqueue()
}

// Abort accepts cancellation of the captured run without acquiring the operation gate or awaiting settlement.
func (c *Coordinator) Abort(ctx context.Context, runID, extensionID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	cause := diagnostic.NewCurrentCause(context.Canceled, fmt.Errorf(extensionAbortFormat, extensionID))
	if err := c.executor.Abort(runID, cause); err != nil {
		return fmt.Errorf("abort agent run from extension %q: %w", extensionID, err)
	}
	return nil
}
