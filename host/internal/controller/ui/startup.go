package ui

import (
	"errors"
	"fmt"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// RejectStartupRequest applies common validation before readiness admission.
func RejectStartupRequest(
	output StartupOutput,
	response *uiv1.OpenResponse,
) (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("reject UI operation before initialization: %w", err)
		}
	}()
	id := response.GetOperationId()
	if id == "" {
		return output.Reject(
			id, RejectionCodeInvalidArgument, errors.New("UI operation identifier is required"),
		)
	}
	request := response.GetRequest()
	if request.GetCancel() != nil {
		if request.GetCancel().GetTargetOperationId() == "" {
			return output.Reject(
				id, RejectionCodeInvalidArgument, errors.New("UI cancellation target is required"),
			)
		}
		return output.Reject(

			id,
			RejectionCodeTargetNotActive,
			fmt.Errorf("UI cancellation target %q is not active", request.GetCancel().GetTargetOperationId()),
		)
	}
	if _, mappingErr := mapCommand(response); mappingErr != nil {
		return output.Reject(id, RejectionCodeInvalidArgument, mappingErr)
	}
	return output.Reject(
		id, RejectionCodeNotReady, errors.New("host UI is not ready"),
	)
}
