package errtree

// CurrentCause describes the outcome-determining branch of a diagnostic error.
type CurrentCause interface {
	error
	// CurrentCause returns the terminal cause.
	CurrentCause() error
}

// IsCurrentCancellation checks cancellation of the current error tree.
func IsCurrentCancellation(err error, match func(error) bool) bool {
	canceled, _ := classifyCancellation(err, match)
	return canceled
}

// IsPlainCancellation checks cancellation without diagnostics.
func IsPlainCancellation(err error, match func(error) bool) bool {
	canceled, diagnostics := classifyCancellation(err, match)
	return canceled && !diagnostics
}

// classifyCancellation checks current cancellation and the presence of diagnostics.
func classifyCancellation(err error, match func(error) bool) (isCancellation, hasDiagnostics bool) {
	if err == nil {
		return false, false
	}
	canceled, diagnostics := true, false
	for node := range IterateCurrentErrors(err) {
		if _, classified := node.(interface{ FailureCode() string }); classified {
			return false, diagnostics
		}
		if _, classified := node.(interface{ CompactionFailureCode() string }); classified {
			return false, diagnostics
		}
		//nolint:errorlint // classifyCancellation checks this node directly to exclude diagnostics from classification.
		if _, classified := node.(CodedError); classified {
			return false, diagnostics
		}
		//nolint:errorlint // The iterator traverses only the current branch of this diagnostic boundary.
		if boundary, present := node.(CurrentCause); present {
			diagnostics = true
			if boundary.CurrentCause() == nil {
				canceled = false
			}
			continue
		}
		if joined, present := node.(interface{ Unwrap() []error }); present {
			for _, child := range joined.Unwrap() {
				if child == nil {
					canceled = false
				}
			}
			continue
		}
		if wrapped, present := node.(interface{ Unwrap() error }); present && wrapped.Unwrap() != nil {
			continue
		}
		canceled = canceled && match(node)
	}
	return canceled, diagnostics
}
