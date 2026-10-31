package errtree

import (
	"iter"
	"slices"
)

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

// FirstCurrent returns the first current error node of type T; diagnostic history is not searched.
func FirstCurrent[T any](err error) (T, bool) {
	for node := range IterateCurrentErrors(err) {
		if typed, matched := node.(T); matched {
			return typed, true
		}
	}
	var empty T
	return empty, false
}

// HasCurrent reports whether one of the targets is itself a current error node; diagnostic history is not searched.
func HasCurrent(err error, targets ...error) bool {
	for node := range IterateCurrentErrors(err) {
		// Identity of the node itself excludes targets kept as diagnostic history below it.
		if slices.Contains(targets, node) {
			return true
		}
	}
	return false
}

// CurrentCode returns the machine-readable category of the first current error node that has one.
func CurrentCode(err error) (string, bool) {
	for node := range IterateCurrentErrors(err) {
		if code, classified := nodeCode(node); classified {
			return code, true
		}
	}
	return "", false
}

// nodeCode returns the machine-readable category that one error node itself declares.
func nodeCode(node error) (string, bool) {
	//nolint:errorlint // The caller already selected the node, so wrapped categories must not match here.
	switch classified := node.(type) {
	case CodedError:
		return classified.Code(), true
	case interface{ FailureCode() string }:
		return classified.FailureCode(), true
	case interface{ CompactionFailureCode() string }:
		return classified.CompactionFailureCode(), true
	default:
		return "", false
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
