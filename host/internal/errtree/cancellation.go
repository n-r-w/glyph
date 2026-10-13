package errtree

// CurrentCause is an error with separate terminal and recovered causes.
type CurrentCause interface {
	error
	// CurrentCause returns the terminal cause.
	CurrentCause() error
}

// IsCurrentCancellation checks cancellation of the current error tree.
func IsCurrentCancellation(err error, match func(error) bool) bool {
	canceled, _ := cancellationState(err, match)
	return canceled
}

// IsPlainCancellation checks cancellation without recovered diagnostics.
func IsPlainCancellation(err error, match func(error) bool) bool {
	canceled, diagnostics := cancellationState(err, match)
	return canceled && !diagnostics
}

// cancellationState checks current cancellation and the presence of recovered diagnostics.
func cancellationState(err error, match func(error) bool) (isCancellation, hasDiagnostics bool) {
	if err == nil {
		return false, false
	}
	if _, classified := err.(interface{ FailureCode() string }); classified {
		return false, false
	}
	if _, classified := err.(interface{ CompactionFailureCode() string }); classified {
		return false, false
	}
	//nolint:errorlint // errors.As would include recovered causes in the current-cause decision.
	if current, present := err.(CurrentCause); present {
		canceled, _ := cancellationState(current.CurrentCause(), match)
		return canceled, true
	}
	if joined, present := err.(interface{ Unwrap() []error }); present {
		canceled, diagnostics := true, false
		for _, child := range joined.Unwrap() {
			childCanceled, childDiagnostics := cancellationState(child, match)
			canceled = canceled && childCanceled
			diagnostics = diagnostics || childDiagnostics
		}
		return canceled, diagnostics
	}
	if wrapped, present := err.(interface{ Unwrap() error }); present {
		if child := wrapped.Unwrap(); child != nil {
			return cancellationState(child, match)
		}
	}
	return match(err), false
}
