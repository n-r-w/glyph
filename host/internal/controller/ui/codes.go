package ui

const (
	// RejectionCodeInvalidArgument classifies malformed command input.
	RejectionCodeInvalidArgument = "INVALID_ARGUMENT"
	// RejectionCodeBusy reports occupied operation admission.
	RejectionCodeBusy = "BUSY"
	// RejectionCodeNotReady reports unavailable application readiness.
	RejectionCodeNotReady = "NOT_READY"
	// FailureCodeInternal classifies failures outside a command-specific category.
	FailureCodeInternal = "INTERNAL"
	// FailureCodeAuthentication reports a failed authentication operation.
	FailureCodeAuthentication = "AUTHENTICATION_FAILED"
	// FailureCodeProviderAuth reports unavailable provider credentials.
	FailureCodeProviderAuth = "CREDENTIAL_UNAVAILABLE"
	// FailureCodeSession reports unavailable session state.
	FailureCodeSession = "SESSION_UNAVAILABLE"
	// FailureCodePersistence reports unavailable durable session storage.
	FailureCodePersistence = "PERSISTENCE_UNAVAILABLE"
	// FailureCodeModelUnavailable reports an unavailable configured model.
	FailureCodeModelUnavailable = "MODEL_UNAVAILABLE"
	// FailureCodeNotFound reports an unavailable selected model.
	FailureCodeNotFound = "NOT_FOUND"
	// FailureCodeReasoning reports an unsupported reasoning choice.
	FailureCodeReasoning = "REASONING_UNSUPPORTED"
	// FailureCodeModelFailed reports failed model execution.
	FailureCodeModelFailed = "MODEL_FAILED"
	// FailureCodeExtensionInvalid reports an invalid extension result.
	FailureCodeExtensionInvalid = "EXTENSION_INVALID_RESULT"
	// FailureCodeExtension reports an unavailable extension.
	FailureCodeExtension = "EXTENSION_UNAVAILABLE"
	// RejectionCodeTargetNotActive reports a cancellation target that is not active.
	RejectionCodeTargetNotActive = "TARGET_NOT_ACTIVE"
	// RejectionCodeOperationIDInUse reports an already reserved operation identifier.
	RejectionCodeOperationIDInUse = "OPERATION_ID_IN_USE"
	// RejectionCodeNotFound reports an unavailable selection during preparation.
	RejectionCodeNotFound = "NOT_FOUND"
)
