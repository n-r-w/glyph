package extensionruntime

import (
	"context"
	"fmt"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// ValidateOutgoingInput invokes the captured policy's final input check.
func (s *Service) ValidateOutgoingInput(
	ctx context.Context,
	owner extensioncontext.PolicyOwner,
	binding extension.Context,
	input extensioncontext.OutgoingInput,
) error {
	operation, available := s.beginOperation(owner.ExtensionID, owner.RuntimeID)
	if !available {
		return extensioncontext.NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, fmt.Errorf(
			capturedPolicyUnavailableFormat,
			ErrExtensionUnavailable,
			owner.ExtensionID,
			owner.RuntimeID,
		))
	}
	err := operation.state.runtime.ValidateOutgoingInput(ctx, owner.HandlerID, binding, input)
	err = extensioncontext.NewPolicyOperationError(ctx, modelexecution.FailureExtensionFailed, err)
	s.finishAndReport(ctx, operation, err)
	return err
}
