package extensionv1

import (
	"errors"
	"fmt"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// validateHostFailed checks failure data and whether an optional HostFailed.result belongs to the request.
func (kind hostRequestKind) validateHostFailed(failed *extensionpb.HostFailed) error {
	failure := failed.GetFailure()
	if failure == nil || failure.GetCode() == "" || failure.GetMessage() == "" {
		return errors.New("host failure requires a category and complete error text")
	}
	if err := kind.validateHostFailureCode(failure.GetCode()); err != nil {
		return err
	}
	if result := failed.GetResult(); result != nil {
		if err := kind.validateHostCompleted(result); err != nil {
			return fmt.Errorf("invalid retained Host result: %w", err)
		}
	}
	return nil
}
