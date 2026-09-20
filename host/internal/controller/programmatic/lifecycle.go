package programmatic

import (
	"errors"
)

const (
	// RejectionCodeInvalidArgument reports an invalid operation identifier, kind, or payload.
	RejectionCodeInvalidArgument = "INVALID_ARGUMENT"
	// RejectionCodeOperationIDInUse reports an identifier owned by a nonterminal operation.
	RejectionCodeOperationIDInUse = "OPERATION_ID_IN_USE"
	// RejectionCodeBusy reports unavailable bounded admission.
	RejectionCodeBusy = "BUSY"
	// RejectionCodeTargetNotActive reports an inactive cancellation target.
	RejectionCodeTargetNotActive = "TARGET_NOT_ACTIVE"
	// RejectionCodeNotFound reports an unknown model selection.
	RejectionCodeNotFound = "NOT_FOUND"
	// RejectionCodeReasoningUnsupported reports an unsupported reasoning choice.
	RejectionCodeReasoningUnsupported = "REASONING_UNSUPPORTED"
	// FailureCodeCredentialUnavailable reports unavailable model credentials.
	FailureCodeCredentialUnavailable = "CREDENTIAL_UNAVAILABLE" //nolint:gosec // This is a failure code.
	// FailureCodeModelUnavailable reports an unavailable requested model.
	FailureCodeModelUnavailable = "MODEL_UNAVAILABLE"
	// FailureCodeModelFailed reports model execution failure.
	FailureCodeModelFailed = "MODEL_FAILED"
	// FailureCodeRetryExhausted reports a retryable failure after all attempts.
	FailureCodeRetryExhausted = "RETRY_EXHAUSTED"
	// FailureCodeRetryCanceled reports explicit retry-handler cancellation.
	FailureCodeRetryCanceled = "RETRY_CANCELED"
	// FailureCodeExtensionFailed reports a retry-handler failure or invalid action.
	FailureCodeExtensionFailed = "EXTENSION_FAILED"
	// FailureCodeRetryDelayExceeded reports a provider delay above the configured maximum.
	FailureCodeRetryDelayExceeded = "RETRY_DELAY_EXCEEDED"
	// FailureCodeContextLimit reports terminal provider context overflow.
	FailureCodeContextLimit = "CONTEXT_LIMIT"
	// FailureCodeCompactionFailed reports terminal active-conversation compaction failure.
	FailureCodeCompactionFailed = "COMPACTION_FAILED"
	// FailureCodeExtensionInvalidResult reports invalid extension output.
	FailureCodeExtensionInvalidResult = "EXTENSION_INVALID_RESULT"
	// FailureCodeExtensionUnavailable reports extension transport or protocol failure.
	FailureCodeExtensionUnavailable = "EXTENSION_UNAVAILABLE"
	// FailureCodeExtensionRejected reports an explicit extension selection rejection.
	FailureCodeExtensionRejected = "EXTENSION_REJECTED"
	// FailureCodeSessionUnavailable reports an unreadable requested session.
	FailureCodeSessionUnavailable = "SESSION_UNAVAILABLE"
	// FailureCodePersistenceUnavailable reports unavailable session persistence.
	FailureCodePersistenceUnavailable = "PERSISTENCE_UNAVAILABLE"
	// FailureCodeInternal reports an unclassified operation failure.
	FailureCodeInternal = "INTERNAL"
)

// RejectionError reports a request failure that does not create an operation.
type RejectionError struct {
	// code is the public machine-readable rejection code.
	code string
	// cause is the complete rejection reason.
	cause error
}

// Error returns the complete rejection cause text.
func (e *RejectionError) Error() string {
	return e.cause.Error()
}

// Code returns the public machine code.
func (e *RejectionError) Code() string {
	return e.code
}

// Unwrap returns the original rejection cause.
func (e *RejectionError) Unwrap() error {
	return e.cause
}

// Reject creates one closed Programmatic rejection.
func Reject(code string, cause error) error {
	return &RejectionError{code: code, cause: cause}
}

// rejectionCode extracts a closed rejection code.
func rejectionCode(err error) (string, bool) {
	var rejection *RejectionError
	if !errors.As(err, &rejection) {
		return "", false
	}
	return rejection.Code(), true
}
