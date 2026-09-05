package extensionv1

import (
	"errors"
	"fmt"

	"github.com/n-r-w/glyph/internal/operation"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// hostRequestKind identifies the initiated Host result contract.
type hostRequestKind uint8

const (
	// hostRequestInvalid identifies a missing or unknown request payload.
	hostRequestInvalid hostRequestKind = iota
	// hostRequestModels identifies complete model-catalog reads.
	hostRequestModels
	// hostRequestProviders identifies provider-identifier catalog reads.
	hostRequestProviders
	// hostRequestCancel identifies targeted cancellation.
	hostRequestCancel
	// contextCodeStale identifies a permanently invalidated context binding.
	contextCodeStale = "STALE_CONTEXT"
)

// classifyHostRequest identifies an implemented extension-initiated request.
func classifyHostRequest(request *extensionpb.ExtensionRequest) hostRequestKind {
	if request == nil {
		return hostRequestInvalid
	}
	switch request.WhichRequest() {
	case extensionpb.ExtensionRequest_GetModels_case:
		return hostRequestModels
	case extensionpb.ExtensionRequest_GetProviders_case:
		return hostRequestProviders
	case extensionpb.ExtensionRequest_Cancel_case:
		return hostRequestCancel
	case extensionpb.ExtensionRequest_Request_not_set_case:
		return hostRequestInvalid
	default:
		return hostRequestInvalid
	}
}

// hostCompletedMatches checks that the terminal payload belongs to its initiator request kind.
func hostCompletedMatches(kind hostRequestKind, result *extensionpb.HostCompleted) bool {
	if result == nil {
		return false
	}
	switch kind {
	case hostRequestModels:
		return result.GetGetModels() != nil
	case hostRequestProviders:
		return result.GetGetProviders() != nil
	case hostRequestCancel:
		return result.GetCancel() != nil
	case hostRequestInvalid:
		return false
	default:
		return false
	}
}

// validateHostFailureCode enforces the closed catalog execution categories.
func validateHostFailureCode(code string) error {
	if code == failureCodeInternal || code == contextCodeStale {
		return nil
	}
	return fmt.Errorf("unsupported Host catalog failure category %q", code)
}

// validateHostRejectionCode enforces operation-specific admission categories.
func validateHostRejectionCode(kind hostRequestKind, code string) error {
	if kind == hostRequestCancel {
		return validateRejectionCode(requestCancel, code)
	}
	switch code {
	case rejectionCodeInvalidArgument,
		rejectionCodeOperationIDInUse,
		rejectionCodeNotReady,
		rejectionCodeBusy,
		contextCodeStale,
		failureCodeInternal:
		return nil
	default:
		return fmt.Errorf("unsupported Host catalog rejection category %q", code)
	}
}

// hostPeerError keeps Host-owned category and cause text when lifecycle validation fails.
func hostPeerError(cause error, event *extensionpb.HostEvent) error {
	if failure := event.GetFailed(); failure != nil {
		return fmt.Errorf(
			"%w: peer failure category %q: peer failure text: %s",
			cause,
			failure.GetCode(),
			failure.GetMessage(),
		)
	}
	if rejection := event.GetRejected(); rejection != nil {
		return fmt.Errorf(
			"%w: peer rejection category %q: peer rejection text: %s",
			cause,
			rejection.GetCode(),
			rejection.GetMessage(),
		)
	}
	return cause
}

// mapHostEvent maps peer lifecycle events without truncating Host-owned error causes.
func mapHostEvent(
	id string,
	kind hostRequestKind,
	payload *extensionpb.HostEvent,
) (operation.Event[struct{}, *extensionpb.HostCompleted], bool, error) {
	event := operation.Event[struct{}, *extensionpb.HostCompleted]{
		ID:       id,
		Kind:     0,
		Progress: struct{}{},
		Result:   nil,
		Code:     "",
		Message:  "",
	}
	if payload == nil {
		return event, false, errors.New("received Host operation requires an event")
	}
	switch payload.WhichEvent() {
	case extensionpb.HostEvent_Accepted_case:
		event.Kind = operation.EventAccepted
	case extensionpb.HostEvent_Running_case:
		event.Kind = operation.EventRunning
	case extensionpb.HostEvent_Completed_case:
		event.Kind, event.Result = operation.EventCompleted, payload.GetCompleted()
		if !hostCompletedMatches(kind, event.Result) {
			return event, false, errors.New("received Host completion does not match request kind")
		}
		if kind == hostRequestCancel {
			if err := validateCancelCompleted(event.Result.GetCancel()); err != nil {
				return event, false, err
			}
		}
		return event, true, nil
	case extensionpb.HostEvent_Canceled_case:
		event.Kind = operation.EventCanceled
		return event, true, nil
	case extensionpb.HostEvent_Failed_case:
		event.Kind, event.Code, event.Message = operation.EventFailed, payload.GetFailed().
			GetCode(),
			payload.GetFailed().
				GetMessage()
		if err := validateHostFailureCode(event.Code); err != nil {
			return event, false, fmt.Errorf("%w: peer failure text: %s", err, event.Message)
		}
		return event, true, nil
	case extensionpb.HostEvent_Rejected_case:
		event.Kind, event.Code, event.Message = operation.EventRejected, payload.GetRejected().
			GetCode(),
			payload.GetRejected().
				GetMessage()
		if err := validateHostRejectionCode(kind, event.Code); err != nil {
			return event, false, fmt.Errorf("%w: peer rejection text: %s", err, event.Message)
		}
		return event, true, nil
	case extensionpb.HostEvent_Event_not_set_case:
		return event, false, errors.New("host operation event payload is required")
	default:
		return event, false, errors.New("host operation event payload is unknown")
	}
	return event, false, nil
}
