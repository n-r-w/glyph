// Package diagnostic constructs errors with separate current and diagnostic causes.
package diagnostic

import (
	"errors"

	"github.com/n-r-w/glyph/internal/errtree"
)

// currentCauseError contains terminal causes and diagnostic causes.
type currentCauseError struct {
	// current contains the causes that determine the outcome.
	current error
	// diagnostics contains causes that supplement the terminal report.
	diagnostics error
}

// The classifier traverses the terminal cause separately from this carrier's diagnostics.
var _ errtree.CurrentCause = (*currentCauseError)(nil)

// NewCurrentCause attaches diagnostics to an outcome-determining error.
func NewCurrentCause(current, diagnostics error) error {
	if diagnostics == nil {
		return current
	}
	return &currentCauseError{current: current, diagnostics: diagnostics}
}

// CurrentCause returns the outcome-determining causes.
func (e *currentCauseError) CurrentCause() error { return e.current }

// Error returns the complete terminal diagnostic text.
func (e *currentCauseError) Error() string { return errors.Join(e.current, e.diagnostics).Error() }

// Unwrap exposes the original terminal and diagnostic causes.
func (e *currentCauseError) Unwrap() []error { return []error{e.current, e.diagnostics} }
