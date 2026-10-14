package errtree

import "iter"

// CodedError is an error with a machine-readable failure or rejection category.
type CodedError interface {
	error
	// Code returns the machine-readable failure or rejection category.
	Code() string
}

// IterateCurrentErrors visits outcome-determining nodes in an error tree.
func IterateCurrentErrors(err error) iter.Seq[error] {
	return func(yield func(error) bool) {
		visitCurrentErrors(err, yield)
	}
}

// visitCurrentErrors traverses current branches for the iterator.
func visitCurrentErrors(err error, yield func(error) bool) bool {
	if err == nil {
		return true
	}
	if !yield(err) {
		return false
	}
	//nolint:errorlint // visitCurrentErrors uses a direct assertion because errors.As searches diagnostic descendants.
	if boundary, ok := err.(CurrentCause); ok {
		return visitCurrentErrors(boundary.CurrentCause(), yield)
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, child := range joined.Unwrap() {
			if !visitCurrentErrors(child, yield) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return visitCurrentErrors(wrapped.Unwrap(), yield)
	}
	return true
}
