package extensionv1

import (
	"errors"
	"fmt"
	"slices"

	"github.com/samber/mo"

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
	// hostRequestConfiguredModel identifies explicit configured-model requests.
	hostRequestConfiguredModel
	// hostRequestAppendExtension identifies model-hidden session appends.
	hostRequestAppendExtension
	// hostRequestSessionState identifies active-branch recovery requests.
	hostRequestSessionState
	// hostRequestAppendExtensionMessage identifies model-visible session appends.
	hostRequestAppendExtensionMessage
	// hostRequestModelSelection identifies active model selection.
	hostRequestModelSelection
	// hostRequestReasoningSelection identifies active reasoning selection.
	hostRequestReasoningSelection
	// hostRequestCompaction identifies session-bound manual compaction.
	hostRequestCompaction
	// hostRequestCompactionProjection identifies candidate model-history projection.
	hostRequestCompactionProjection
	// hostRequestCompactionReplacement identifies scoped durable replacement.
	hostRequestCompactionReplacement
	// hostRequestCompactionRequest identifies one scoped request participant invocation.
	hostRequestCompactionRequest
	// hostRequestCompactionGenerator identifies one scoped generator invocation.
	hostRequestCompactionGenerator
	// hostRequestCompactionResult identifies one captured result handler call.
	hostRequestCompactionResult
	// hostRequestCancel identifies targeted cancellation.
	hostRequestCancel
	// contextCodeStale identifies a permanently invalidated context binding.
	contextCodeStale = "STALE_CONTEXT"
	// hostFailureCodeModelUnavailable identifies an unavailable configured selection.
	hostFailureCodeModelUnavailable = "MODEL_UNAVAILABLE"
	// hostFailureCodeCredentialUnavailable identifies unavailable provider credentials.
	hostFailureCodeCredentialUnavailable = "CREDENTIAL_UNAVAILABLE" //nolint:gosec // This is a protocol error code.
	// hostFailureCodeModelFailed identifies provider request failure.
	hostFailureCodeModelFailed = "MODEL_FAILED"
	// hostFailureCodeRetryExhausted identifies exhausted provider retries.
	hostFailureCodeRetryExhausted = "RETRY_EXHAUSTED"
	// hostFailureCodeRetryCanceled identifies explicit retry-handler cancellation.
	hostFailureCodeRetryCanceled = "RETRY_CANCELED"
	// hostFailureCodeExtensionFailed identifies retry-handler failure.
	hostFailureCodeExtensionFailed = "EXTENSION_FAILED"
	// hostFailureCodeRetryDelayExceeded identifies a provider delay above the configured maximum.
	hostFailureCodeRetryDelayExceeded = "RETRY_DELAY_EXCEEDED"
	// hostFailureCodeContextLimit identifies terminal provider context overflow.
	hostFailureCodeContextLimit = "CONTEXT_LIMIT"
	// hostFailureCodeCompactionFailed identifies terminal manual compaction failure.
	hostFailureCodeCompactionFailed = "COMPACTION_FAILED"
	// hostFailureCodeSessionUnavailable identifies unavailable active session state.
	hostFailureCodeSessionUnavailable = "SESSION_UNAVAILABLE"
	// hostFailureCodePersistenceUnavailable identifies a durable append failure.
	hostFailureCodePersistenceUnavailable = "PERSISTENCE_UNAVAILABLE"
	// hostFailureCodeExtensionRejected identifies explicit selection-handler rejection.
	hostFailureCodeExtensionRejected = "EXTENSION_REJECTED"
	// hostFailureCodeExtensionUnavailable identifies a selected handler runtime failure.
	hostFailureCodeExtensionUnavailable = "EXTENSION_UNAVAILABLE"
	// hostRejectionCodeNotFound identifies a missing requested starting model.
	hostRejectionCodeNotFound = "NOT_FOUND"
	// hostRejectionCodeReasoningUnsupported identifies an unsupported requested starting reasoning choice.
	hostRejectionCodeReasoningUnsupported = "REASONING_UNSUPPORTED"
)

// matchesHostCompleted checks the completion payload for its request kind.
func (kind hostRequestKind) matchesHostCompleted(result *extensionpb.HostCompleted) bool {
	descriptor, supported := newHostRequestDescriptors()[kind]
	return supported && descriptor.matches(result)
}

// validateHostFailureCode enforces the current request's closed execution categories.
func (kind hostRequestKind) validateHostFailureCode(code string) error {
	if code == failureCodeInternal || code == contextCodeStale {
		return nil
	}
	if slices.Contains(newHostRequestDescriptors()[kind].failures, code) {
		return nil
	}
	return fmt.Errorf("unsupported Host failure category %q for request kind %d", code, kind)
}

// validateHostRejectionCode enforces operation-specific admission categories.
func (kind hostRequestKind) validateHostRejectionCode(code string) error {
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
	case hostRejectionCodeNotFound, hostRejectionCodeReasoningUnsupported:
		if newHostRequestDescriptors()[kind].selection {
			return nil
		}
	}
	return fmt.Errorf("unsupported Host catalog rejection category %q", code)
}

// validateHostCompleted enforces the result contract for one extension-initiated request kind.
func (kind hostRequestKind) validateHostCompleted(result *extensionpb.HostCompleted) error {
	if !kind.matchesHostCompleted(result) {
		return errors.New("received Host completion does not match request kind")
	}
	if validate, present := newHostRequestDescriptors()[kind].validate.Get(); present {
		return validate(result)
	}
	return nil
}

// hostCompletionValidator checks a kind-specific completed payload.
type hostCompletionValidator func(*extensionpb.HostCompleted) error

// hostProgressPredicate identifies allowed progress for one request kind.
type hostProgressPredicate func(*extensionpb.HostProgress) bool

// hostRequestDescriptor contains one current Host request contract.
type hostRequestDescriptor struct {
	// requestCase identifies the protobuf request discriminator.
	requestCase int
	// matches identifies the corresponding completion payload.
	matches func(*extensionpb.HostCompleted) bool
	// validate contains additional completion validation when required.
	validate mo.Option[hostCompletionValidator]
	// failures contains the kind-specific execution categories.
	failures []string
	// progress contains the allowed progress predicate when present.
	progress mo.Option[hostProgressPredicate]
	// selection allows model/reasoning selection rejection categories.
	selection bool
}

// newHostRequestDescriptors constructs request classification and result rules.
func newHostRequestDescriptors() map[hostRequestKind]hostRequestDescriptor {
	appendFailures := []string{hostFailureCodePersistenceUnavailable, hostFailureCodeSessionUnavailable}
	selectionFailures := []string{
		hostFailureCodeModelUnavailable, hostFailureCodeCredentialUnavailable,
		hostFailureCodeExtensionRejected, hostFailureCodeExtensionUnavailable,
	}
	compactionFailures := []string{
		hostFailureCodeCompactionFailed, hostFailureCodeExtensionFailed,
		hostFailureCodeSessionUnavailable, hostFailureCodePersistenceUnavailable,
	}
	return map[hostRequestKind]hostRequestDescriptor{
		hostRequestModels: {
			requestCase: int(extensionpb.ExtensionRequest_GetModels_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetGetModels() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    nil,
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestProviders: {
			requestCase: int(extensionpb.ExtensionRequest_GetProviders_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetGetProviders() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    nil,
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestConfiguredModel: {
			requestCase: int(extensionpb.ExtensionRequest_ConfiguredModel_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetConfiguredModel() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures: []string{
				hostFailureCodeModelUnavailable,
				hostFailureCodeCredentialUnavailable,
				hostFailureCodeModelFailed,
				hostFailureCodeRetryExhausted,
				hostFailureCodeRetryCanceled,
				hostFailureCodeExtensionFailed,
				hostFailureCodeRetryDelayExceeded,
				hostFailureCodeContextLimit,
			},
			progress: mo.Some[hostProgressPredicate](
				func(progress *extensionpb.HostProgress) bool { return progress.GetConfiguredModelRetry() != nil },
			),
			selection: false,
		},
		hostRequestAppendExtension: {
			requestCase: int(extensionpb.ExtensionRequest_AppendExtension_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetAppendExtension() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    appendFailures,
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestSessionState: {
			requestCase: int(extensionpb.ExtensionRequest_GetSessionState_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetGetSessionState() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    []string{hostFailureCodeSessionUnavailable},
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestAppendExtensionMessage: {
			requestCase: int(extensionpb.ExtensionRequest_AppendExtensionMessage_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetAppendExtensionMessage() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    appendFailures,
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestModelSelection: {
			requestCase: int(extensionpb.ExtensionRequest_SelectModel_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetSelection() != nil },
			validate: mo.Some[hostCompletionValidator](
				func(result *extensionpb.HostCompleted) error { return validateSelectionResult(result.GetSelection()) },
			),
			failures:  selectionFailures,
			progress:  mo.None[hostProgressPredicate](),
			selection: true,
		},
		hostRequestReasoningSelection: {
			requestCase: int(extensionpb.ExtensionRequest_SelectReasoning_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetSelection() != nil },
			validate: mo.Some[hostCompletionValidator](
				func(result *extensionpb.HostCompleted) error { return validateSelectionResult(result.GetSelection()) },
			),
			failures:  selectionFailures,
			progress:  mo.None[hostProgressPredicate](),
			selection: true,
		},
		hostRequestCompaction: {
			requestCase: int(extensionpb.ExtensionRequest_Compact_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetCompact() != nil },
			validate: mo.Some[hostCompletionValidator](
				func(result *extensionpb.HostCompleted) error { return validateCompactionCompleted(result.GetCompact()) },
			),
			failures: compactionFailures,
			progress: mo.Some[hostProgressPredicate](
				func(progress *extensionpb.HostProgress) bool { return progress.GetCompaction() != nil },
			),
			selection: false,
		},
		hostRequestCompactionProjection: {
			requestCase: int(extensionpb.ExtensionRequest_ProjectCompaction_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetProjectCompaction() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    compactionFailures,
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestCompactionReplacement: {
			requestCase: int(extensionpb.ExtensionRequest_ReplaceCompaction_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetReplaceCompaction() != nil },
			validate: mo.Some[hostCompletionValidator](func(result *extensionpb.HostCompleted) error {
				return validateCompactionCompleted(result.GetReplaceCompaction())
			}),
			failures:  compactionFailures,
			progress:  mo.None[hostProgressPredicate](),
			selection: false,
		},
		hostRequestCompactionRequest: {
			requestCase: int(extensionpb.ExtensionRequest_InvokeCompactionRequest_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetInvokeCompactionRequest() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    compactionFailures,
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestCompactionGenerator: {
			requestCase: int(extensionpb.ExtensionRequest_InvokeCompactionGenerator_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetInvokeCompactionGenerator() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    compactionFailures,
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestCompactionResult: {
			requestCase: int(extensionpb.ExtensionRequest_InvokeCompactionResult_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetInvokeCompactionResult() != nil },
			validate:    mo.None[hostCompletionValidator](),
			failures:    compactionFailures,
			progress:    mo.None[hostProgressPredicate](),
			selection:   false,
		},
		hostRequestCancel: {
			requestCase: int(extensionpb.ExtensionRequest_Cancel_case),
			matches:     func(result *extensionpb.HostCompleted) bool { return result.GetCancel() != nil },
			validate: mo.Some[hostCompletionValidator](
				func(result *extensionpb.HostCompleted) error { return validateCancelCompleted(result.GetCancel()) },
			),
			failures:  nil,
			progress:  mo.None[hostProgressPredicate](),
			selection: false,
		},
	}
}

// classifyHostRequest identifies an implemented extension-initiated request.
func classifyHostRequest(request *extensionpb.ExtensionRequest) hostRequestKind {
	if request == nil {
		return hostRequestInvalid
	}
	for kind, descriptor := range newHostRequestDescriptors() {
		if descriptor.requestCase == int(request.WhichRequest()) {
			return kind
		}
	}
	return hostRequestInvalid
}

// validateHostOutputFailureCode validates publishable Host failure categories.
func validateHostOutputFailureCode(code string) error {
	for _, kind := range []hostRequestKind{
		hostRequestModels, hostRequestConfiguredModel, hostRequestAppendExtension, hostRequestSessionState,
		hostRequestModelSelection, hostRequestReasoningSelection, hostRequestCompaction,
	} {
		if kind.validateHostFailureCode(code) == nil {
			return nil
		}
	}
	return fmt.Errorf("unsupported Host failure category %q", code)
}

// validateCompactionCompleted checks exclusive terminal state and durable marker fields.
func validateCompactionCompleted(compaction *extensionpb.CompactResult) error {
	if compaction.GetCommitted() == nil && !compaction.GetCanceled() {
		return errors.New("host compaction completion requires a commit or cancellation")
	}
	if compaction.GetCommitted() != nil && compaction.GetCanceled() {
		return errors.New("host compaction completion cannot be committed and canceled")
	}
	if err := validateCommittedCompaction(compaction.GetCommitted()); err != nil {
		return err
	}
	return validateCompactionDiagnostic(compaction)
}

// validateCommittedCompaction checks complete durable marker identity when present.
func validateCommittedCompaction(committed *extensionpb.CommittedCompaction) error {
	if committed == nil {
		return nil
	}
	marker := committed.GetCompaction()
	if committed.GetEntryId() == "" || marker == nil || marker.GetSummary() == "" ||
		marker.GetFirstKeptEntryId() == "" || marker.GetSource() == nil {
		return errors.New("host committed compaction is incomplete")
	}
	return nil
}

// validateCompactionDiagnostic checks post-commit error text and category presence together.
func validateCompactionDiagnostic(compaction *extensionpb.CompactResult) error {
	if compaction.HasError() && compaction.GetCommitted() == nil {
		return errors.New("host compaction completion error requires committed state")
	}
	if compaction.HasError() && (!compaction.HasFailureCode() || compaction.GetFailureCode() == "") {
		return errors.New("host compaction completion error requires a failure code")
	}
	if !compaction.HasError() && compaction.HasFailureCode() {
		return errors.New("host compaction completion failure code requires an error")
	}
	return nil
}

// validateSelectionResult validates the required full selection and closed diagnostic kinds.
func validateSelectionResult(result *extensionpb.SelectionResult) error {
	selection := result.GetSelection()
	if selection == nil || selection.GetProviderId() == "" || selection.GetModelId() == "" ||
		selection.GetReasoningChoice() == "" {
		return errors.New("host selection completion requires a complete selection")
	}
	for _, issue := range result.GetIssues() {
		if issue == nil || issue.GetMessage() == "" {
			return errors.New("host selection completion requires complete issue text")
		}
		switch issue.GetCode() {
		case extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_HANDLER_ERROR,
			extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_INVALID_HANDLER_ACTION,
			extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_DELIVERY_FAILED,
			extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_OBSERVER_ERROR:
		case extensionpb.SelectionIssueCode_SELECTION_ISSUE_CODE_UNSPECIFIED:
			return errors.New("host selection completion has an unspecified issue kind")
		default:
			return fmt.Errorf("host selection completion has unsupported issue kind %d", issue.GetCode())
		}
	}
	return nil
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
) (operation.Event[*extensionpb.HostProgress, *extensionpb.HostCompleted], bool, error) {
	event := operation.Event[*extensionpb.HostProgress, *extensionpb.HostCompleted]{
		ID:       id,
		Kind:     0,
		Progress: nil,
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
	case extensionpb.HostEvent_Progress_case:
		progress := payload.GetProgress()
		allowed, present := newHostRequestDescriptors()[kind].progress.Get()
		if !present || !allowed(progress) {
			return event, false, errors.New("host progress does not match request kind")
		}
		event.Kind, event.Progress = operation.EventProgress, payload.GetProgress()
	case extensionpb.HostEvent_Completed_case:
		event.Kind, event.Result = operation.EventCompleted, payload.GetCompleted()
		if err := kind.validateHostCompleted(event.Result); err != nil {
			return event, false, err
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
		if err := kind.validateHostFailureCode(event.Code); err != nil {
			return event, false, fmt.Errorf("%w: peer failure text: %s", err, event.Message)
		}
		return event, true, nil
	case extensionpb.HostEvent_Rejected_case:
		event.Kind, event.Code, event.Message = operation.EventRejected, payload.GetRejected().
			GetCode(),
			payload.GetRejected().
				GetMessage()
		if err := kind.validateHostRejectionCode(event.Code); err != nil {
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
