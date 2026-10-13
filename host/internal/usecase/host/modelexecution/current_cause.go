package modelexecution

import (
	"errors"

	"github.com/n-r-w/glyph/host/internal/errtree"
)

// currentCauseError contains the terminal cause and recovered provider diagnostics.
type currentCauseError struct {
	// current contains the terminal cause.
	current error
	// recovered contains failures from recovered provider attempts.
	recovered error
}

var _ errtree.CurrentCause = (*currentCauseError)(nil)

// CurrentCause returns the terminal cause.
func (e *currentCauseError) CurrentCause() error { return e.current }

// Error returns the complete current and recovered diagnostic text.
func (e *currentCauseError) Error() string { return errors.Join(e.current, e.recovered).Error() }

// Unwrap exposes the original current and recovered causes.
func (e *currentCauseError) Unwrap() []error { return []error{e.current, e.recovered} }
