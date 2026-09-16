package modelselection

import (
	"errors"

	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

const (
	// ErrorCodeBusy identifies an overlapping active selection operation.
	ErrorCodeBusy = "busy"
	// ErrorCodeModelUnavailable identifies a final target that cannot be selected.
	ErrorCodeModelUnavailable = "model_unavailable"
	// ErrorCodeCredentialUnavailable identifies unavailable final-target credentials.
	ErrorCodeCredentialUnavailable = "credential_unavailable" //nolint:gosec // This is a public error code.
	// ErrorCodeExtensionRejected identifies an explicit selection-handler rejection.
	ErrorCodeExtensionRejected = "extension_rejected"
	// ErrorCodeExtensionUnavailable identifies a selected handler runtime lost before commit.
	ErrorCodeExtensionUnavailable = "extension_unavailable"
	// ErrorCodeInternal identifies an internal pre-commit selection failure.
	ErrorCodeInternal = "internal"
	// credentialUnavailableCode is the catalog credential failure category.
	credentialUnavailableCode = "credential_unavailable" //nolint:gosec // This is a public error code.
)

// SelectionError classifies one selection preparation or execution failure.
type SelectionError struct {
	// Code is the stable selection failure category.
	Code string
	// cause preserves the complete source failure.
	cause error
}

var (
	_ hostprogrammatic.SelectionFailure = (*SelectionError)(nil)
	_ hostui.SelectionFailure           = (*SelectionError)(nil)
)

// Error returns complete selection failure text.
func (e *SelectionError) Error() string { return e.cause.Error() }

// Unwrap returns the original selection failure.
func (e *SelectionError) Unwrap() error { return e.cause }

// ModelSelectionCode returns the stable selection failure category.
func (e *SelectionError) ModelSelectionCode() string { return e.Code }

// classifyFinalValidation maps catalog categories to closed execution failures.
func classifyFinalValidation(err error) error {
	code := ErrorCodeModelUnavailable
	var failure CatalogFailure
	if errors.As(err, &failure) && failure.CatalogSelectionCode() == credentialUnavailableCode {
		code = ErrorCodeCredentialUnavailable
	}
	return &SelectionError{Code: code, cause: err}
}
