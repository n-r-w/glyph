// Package errtree provides dependency-neutral traversal of Go error trees.
package errtree

import "errors"

// AllLeavesMatch reports whether match accepts every recursively unwrapped error leaf.
func AllLeavesMatch(err error, match func(error) bool) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if !AllLeavesMatch(cause, match) {
				return false
			}
		}
		return true
	}
	if cause := errors.Unwrap(err); cause != nil {
		return AllLeavesMatch(cause, match)
	}
	return match(err)
}
