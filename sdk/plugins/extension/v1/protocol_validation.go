package extensionv1

import (
	"errors"
	"fmt"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
)

// validateRejectionCode checks the closed rejection set for one request kind.
func validateRejectionCode(kind requestKind, code string) error {
	valid := false
	switch kind {
	case requestRegister:
		valid = code == rejectionCodeInvalidArgument || code == rejectionCodeOperationIDInUse ||
			code == rejectionCodeBusy || code == rejectionCodeNotReady
	case requestHandle, requestExecute:
		valid = code == rejectionCodeInvalidArgument || code == rejectionCodeOperationIDInUse ||
			code == rejectionCodeNotReady || code == rejectionCodeBusy
	case requestCancel:
		valid = code == rejectionCodeInvalidArgument || code == rejectionCodeOperationIDInUse ||
			code == rejectionCodeTargetNotActive
	}
	if !valid {
		return fmt.Errorf("unsupported extension rejection code %q for request kind %d", code, kind)
	}
	return nil
}

// validateFailureCode checks the Extension contract's closed failure set.
func validateFailureCode(code string) error {
	if code != failureCodeInternal {
		return fmt.Errorf("unsupported extension failure code %q", code)
	}
	return nil
}

// validateCancelCompleted checks required target state presence and value.
func validateCancelCompleted(completed *operationpb.CancelCompleted) error {
	if completed == nil || !completed.HasTargetState() {
		return errors.New("cancellation completion target state is required")
	}
	switch completed.GetTargetState() {
	case operationpb.TerminalState_TERMINAL_STATE_COMPLETED,
		operationpb.TerminalState_TERMINAL_STATE_CANCELED,
		operationpb.TerminalState_TERMINAL_STATE_FAILED:
		return nil
	case operationpb.TerminalState_TERMINAL_STATE_UNSPECIFIED:
		return errors.New("cancellation completion target state is unspecified")
	default:
		return fmt.Errorf("cancellation completion target state %d is unknown", completed.GetTargetState())
	}
}
