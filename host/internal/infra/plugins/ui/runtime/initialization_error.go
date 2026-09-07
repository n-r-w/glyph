package runtime

import (
	"errors"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// initializationError preserves one classified UI contract failure.
type initializationError struct {
	// code is the stable machine-readable category.
	code string
	// cause preserves complete error text.
	cause error
}

var _ controllerui.InitializationFailure = (*initializationError)(nil)

// Error returns complete public error text.
func (e *initializationError) Error() string { return e.cause.Error() }

// InitializationCode returns the stable machine-readable category.
func (e *initializationError) InitializationCode() string { return e.code }

// Unwrap returns the preserved contract cause.
func (e *initializationError) Unwrap() error { return e.cause }

// newInitializationError creates one classified contract error.
func newInitializationError(code, message string) error {
	return &initializationError{code: code, cause: errors.New(message)}
}
