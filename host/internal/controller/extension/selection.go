package extension

import (
	"context"
	"errors"
	"fmt"

	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

const (
	// selectionCodeBusy identifies shared selection admission contention.
	selectionCodeBusy = "busy"
	// selectionCodeNotFound identifies a missing requested starting model.
	selectionCodeNotFound = "not_found"
	// selectionCodeReasoningUnsupported identifies an unsupported requested starting reasoning choice.
	selectionCodeReasoningUnsupported = "reasoning_unsupported"
	// selectionCodeStaleContext identifies invalidated extension binding protection.
	selectionCodeStaleContext = "stale_context"
	// selectionCodeModelUnavailable identifies a final target that cannot be selected.
	selectionCodeModelUnavailable = "model_unavailable"
	// selectionCodeCredentialUnavailable identifies unavailable final-target credentials.
	selectionCodeCredentialUnavailable = "credential_unavailable" //nolint:gosec // This is a public error code.
	// selectionCodeExtensionRejected identifies explicit handler rejection.
	selectionCodeExtensionRejected = "extension_rejected"
	// selectionCodeExtensionUnavailable identifies selected handler runtime loss.
	selectionCodeExtensionUnavailable = "extension_unavailable"
	// selectionCodeInternal identifies an unclassified shared selection failure.
	selectionCodeInternal = "internal"
	// publicSelectionCodeBusy identifies public shared-admission rejection.
	publicSelectionCodeBusy = "BUSY"
	// publicSelectionCodeNotFound identifies public starting-model rejection.
	publicSelectionCodeNotFound = "NOT_FOUND"
	// publicSelectionCodeReasoningUnsupported identifies public starting-reasoning rejection.
	publicSelectionCodeReasoningUnsupported = "REASONING_UNSUPPORTED"
	// publicSelectionCodeStaleContext identifies public invalidated binding failure.
	publicSelectionCodeStaleContext = "STALE_CONTEXT"
	// publicSelectionCodeModelUnavailable identifies public final-target failure.
	publicSelectionCodeModelUnavailable = "MODEL_UNAVAILABLE"
	// publicSelectionCodeCredentialUnavailable identifies public credential failure.
	publicSelectionCodeCredentialUnavailable = "CREDENTIAL_UNAVAILABLE" //nolint:gosec // Public error code.
	// publicSelectionCodeExtensionRejected identifies public handler rejection.
	publicSelectionCodeExtensionRejected = "EXTENSION_REJECTED"
	// publicSelectionCodeExtensionUnavailable identifies public handler runtime loss.
	publicSelectionCodeExtensionUnavailable = "EXTENSION_UNAVAILABLE"
	// selectionIssueHandlerError identifies an ordinary handler error.
	selectionIssueHandlerError = "HANDLER_ERROR"
	// selectionIssueInvalidHandlerAction identifies a malformed handler action.
	selectionIssueInvalidHandlerAction = "INVALID_HANDLER_ACTION"
)

// mapModelSelectionRequest validates and projects a public model-selection request.
func mapModelSelectionRequest(request *extensionpb.SelectModelRequest) (SelectionCommand, error) {
	if request == nil || request.GetProviderId() == "" || request.GetModelId() == "" {
		return SelectionCommand{}, errors.New("model selection provider and model are required")
	}
	return SelectionCommand{
		Kind: SelectionCommandModel, ExtensionID: "", RuntimeID: "",
		Context:  extensiondomain.ContextRef{ID: "", RuntimeInstanceID: "", SessionID: ""},
		Provider: model.ProviderID(request.GetProviderId()), Model: model.ID(request.GetModelId()), ReasoningChoice: "",
	}, nil
}

// mapReasoningSelectionRequest validates and projects a public reasoning-selection request.
func mapReasoningSelectionRequest(request *extensionpb.SelectReasoningRequest) (SelectionCommand, error) {
	if request == nil || request.GetReasoningChoice() == "" {
		return SelectionCommand{}, errors.New("reasoning selection choice is required")
	}
	return SelectionCommand{
		Kind: SelectionCommandReasoning, ExtensionID: "", RuntimeID: "",
		Context:  extensiondomain.ContextRef{ID: "", RuntimeInstanceID: "", SessionID: ""},
		Provider: "", Model: "", ReasoningChoice: model.ReasoningChoice(request.GetReasoningChoice()),
	}, nil
}

// mapSelectionRejection maps shared preparation categories into the closed public rejection set.
func mapSelectionRejection(err error) error {
	if failure, ok := errors.AsType[SelectionFailure](err); ok {
		switch failure.ModelSelectionCode() {
		case selectionCodeBusy, selectionCodeNotFound, selectionCodeReasoningUnsupported:
			return extensionsdk.Reject(publicSelectionCode(failure.ModelSelectionCode()), err)
		}
	}
	return extensionsdk.Reject(internalFailureCode, fmt.Errorf("prepare extension model selection: %w", err))
}

// mapSelectionResult maps a shared terminal result into one public completion or failure.
func mapSelectionResult(result SelectionResult) (*extensionpb.SelectionResult, error) {
	if !result.Committed {
		if result.Source == nil {
			return nil, extensionsdk.Fail(
				internalFailureCode,
				errors.New("model selection returned no committed result or failure"),
			)
		}
		if isSelectionCancellationOnly(result.Source) {
			return nil, result.Source
		}
		if failure, ok := errors.AsType[SelectionFailure](result.Source); ok {
			return nil, extensionsdk.Fail(
				publicSelectionCode(failure.ModelSelectionCode()), fmt.Errorf("select model: %w", result.Source),
			)
		}
		return nil, fmt.Errorf("select model: %w", result.Source)
	}
	issues := make([]*extensionpb.SelectionIssue, 0, len(result.Issues))
	for issueIndex := range result.Issues {
		issue := &result.Issues[issueIndex]
		code, err := mapSelectionIssueCode(issue.Code)
		if err != nil {
			return nil, extensionsdk.Fail(internalFailureCode, err)
		}
		issues = append(issues, extensionpb.SelectionIssue_builder{
			Code: new(code), ExtensionId: new(issue.ExtensionID), HandlerId: new(issue.HandlerID),
			Message: new(issue.Message),
		}.Build())
	}
	return extensionpb.SelectionResult_builder{
		Selection: extensionpb.ModelSelection_builder{
			ProviderId: new(string(result.Selection.Provider)), ModelId: new(string(result.Selection.Model)),
			ReasoningChoice: new(string(result.Selection.ReasoningChoice)),
		}.Build(),
		Issues: issues,
	}.Build(), nil
}

// multiCause exposes all leaves of an error joined from independent causes.
type multiCause interface {
	// Unwrap returns every independent cause.
	Unwrap() []error
}

// singleCause exposes the next error in one wrapped cause chain.
type singleCause interface {
	// Unwrap returns the wrapped cause.
	Unwrap() error
}

// isSelectionCancellationOnly recognizes pure cancellation without hiding an independent failure leaf.
func isSelectionCancellationOnly(err error) bool {
	if causes, ok := err.(multiCause); ok {
		for _, cause := range causes.Unwrap() {
			if !isSelectionCancellationOnly(cause) {
				return false
			}
		}
		return true
	}
	if cause, ok := err.(singleCause); ok {
		return isSelectionCancellationOnly(cause.Unwrap())
	}
	return errors.Is(err, context.Canceled)
}

// publicSelectionCode maps shared internal categories to the closed extension contract.
func publicSelectionCode(code string) string {
	switch code {
	case selectionCodeBusy:
		return publicSelectionCodeBusy
	case selectionCodeNotFound:
		return publicSelectionCodeNotFound
	case selectionCodeReasoningUnsupported:
		return publicSelectionCodeReasoningUnsupported
	case selectionCodeStaleContext:
		return publicSelectionCodeStaleContext
	case selectionCodeModelUnavailable:
		return publicSelectionCodeModelUnavailable
	case selectionCodeCredentialUnavailable:
		return publicSelectionCodeCredentialUnavailable
	case selectionCodeExtensionRejected:
		return publicSelectionCodeExtensionRejected
	case selectionCodeExtensionUnavailable:
		return publicSelectionCodeExtensionUnavailable
	case selectionCodeInternal:
		return internalFailureCode
	default:
		return internalFailureCode
	}
}

// mapSelectionIssueCode maps shared issue categories into the closed public enum.
func mapSelectionIssueCode(code string) (extensionpb.SelectionIssueCode, error) {
	switch code {
	case selectionIssueHandlerError:
		return extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_HANDLER_ERROR, nil
	case selectionIssueInvalidHandlerAction:
		return extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_INVALID_HANDLER_ACTION, nil
	case deliveryFailedIssueCode:
		return extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_DELIVERY_FAILED, nil
	default:
		return extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_UNSPECIFIED,
			fmt.Errorf("unknown model selection issue %q", code)
	}
}
