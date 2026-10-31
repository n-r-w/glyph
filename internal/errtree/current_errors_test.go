//go:build !integration

package errtree

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// boundaryError separates one current cause from diagnostic history.
type boundaryError struct {
	// current is the outcome-determining cause.
	current error
	// diagnostics is the retained history.
	diagnostics error
}

// Error returns the text of both branches.
func (e boundaryError) Error() string { return errors.Join(e.current, e.diagnostics).Error() }

// Unwrap exposes both branches.
func (e boundaryError) Unwrap() []error { return []error{e.current, e.diagnostics} }

// CurrentCause returns the outcome-determining cause.
func (e boundaryError) CurrentCause() error { return e.current }

// categoryError is an error with a model-execution category.
type categoryError struct{ code string }

// Error returns the category.
func (e categoryError) Error() string { return e.code }

// FailureCode returns the category.
func (e categoryError) FailureCode() string { return e.code }

// TestFirstCurrent verifies that the first current node of a type is found and that diagnostic history is skipped.
func TestFirstCurrent(t *testing.T) {
	t.Parallel()
	// Arrange a current category under wrappers and joins and a diagnostic category behind a boundary.
	current := fmt.Errorf("outer: %w", errors.Join(errors.New("plain"), categoryError{code: "CURRENT"}))
	historical := boundaryError{current: errors.New("current plain"), diagnostics: categoryError{code: "HISTORY"}}

	// Act by looking up the first current category in both trees.
	found, currentFound := FirstCurrent[interface{ FailureCode() string }](current)
	_, historicalFound := FirstCurrent[interface{ FailureCode() string }](historical)

	// Assert only the current category is found.
	assert.True(t, currentFound)
	assert.Equal(t, "CURRENT", found.FailureCode())
	assert.False(t, historicalFound)
}

// TestHasCurrent verifies that a sentinel counts only as a current node.
func TestHasCurrent(t *testing.T) {
	t.Parallel()
	// Arrange one sentinel under wrappers and joins and one behind a diagnostic boundary.
	sentinel := errors.New("sentinel")
	current := errors.Join(fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", sentinel)), errors.New("other"))
	historical := boundaryError{current: errors.New("current plain"), diagnostics: fmt.Errorf("wrapped: %w", sentinel)}

	// Act and assert that only the current sentinel is found.
	assert.True(t, HasCurrent(current, sentinel))
	assert.False(t, HasCurrent(historical, sentinel))
	assert.False(t, HasCurrent(current, errors.New("absent")))
}
