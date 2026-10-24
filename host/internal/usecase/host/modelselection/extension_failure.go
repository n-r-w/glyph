package modelselection

import (
	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/internal/errtree"
)

// extensionFailureError retains explicit current selection-participant failure meaning.
type extensionFailureError struct {
	// cause preserves the complete participant failure.
	cause error
}

var (
	_ extensioncontroller.NestedExtensionFailure = (*extensionFailureError)(nil)
	_ errtree.CodedError                         = (*extensionFailureError)(nil)
)

// Error returns the complete participant failure text.
func (e *extensionFailureError) Error() string { return e.cause.Error() }

// Unwrap returns the original participant failure.
func (e *extensionFailureError) Unwrap() error { return e.cause }

// Code preserves independent failure classification during owning cancellation.
func (e *extensionFailureError) Code() string { return selectionExtensionFailureCode }

// IsExtensionFailure identifies a failed selected extension participant.
func (e *extensionFailureError) IsExtensionFailure() bool { return true }

// NewExtensionFailure classifies a selected participant failure while preserving its full cause.
func NewExtensionFailure(cause error) error {
	return &SelectionError{Code: ErrorCodeExtensionFailed, cause: &extensionFailureError{cause: cause}}
}
