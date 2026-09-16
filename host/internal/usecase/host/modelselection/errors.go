package modelselection

import (
	"errors"
	"fmt"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
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
	// ErrorCodeStaleContext identifies an invalidated extension runtime or session binding.
	ErrorCodeStaleContext = "stale_context"
	// credentialUnavailableCode is the catalog credential failure category.
	credentialUnavailableCode = "credential_unavailable" //nolint:gosec // This is a public error code.
	// bindingStaleContextCode is the context owner's stale-binding category.
	bindingStaleContextCode = "STALE_CONTEXT"
)

// SelectionError classifies one selection preparation or execution failure.
type SelectionError struct {
	// Code is the stable selection failure category.
	Code string
	// cause preserves the complete source failure.
	cause error
}

var (
	_ extensioncontroller.SelectionFailure = (*SelectionError)(nil)
	_ hostprogrammatic.SelectionFailure    = (*SelectionError)(nil)
	_ hostui.SelectionFailure              = (*SelectionError)(nil)
)

// Error returns complete selection failure text.
func (e *SelectionError) Error() string { return e.cause.Error() }

// Unwrap returns the original selection failure.
func (e *SelectionError) Unwrap() error { return e.cause }

// ModelSelectionCode returns the stable selection failure category.
func (e *SelectionError) ModelSelectionCode() string { return e.Code }

// classifyBindingProtection maps context-owner failures to extension selection categories.
func classifyBindingProtection(err error) error {
	var failure BindingFailure
	if errors.As(err, &failure) && failure.ContextCode() == bindingStaleContextCode {
		return &SelectionError{
			Code: ErrorCodeStaleContext, cause: fmt.Errorf("protect extension model selection commit: %w", err),
		}
	}
	return &SelectionError{
		Code: ErrorCodeInternal, cause: fmt.Errorf("protect extension model selection commit: %w", err),
	}
}

// classifyFinalValidation maps catalog categories to closed execution failures.
func classifyFinalValidation(err error) error {
	code := ErrorCodeModelUnavailable
	var failure CatalogFailure
	if errors.As(err, &failure) && failure.CatalogSelectionCode() == credentialUnavailableCode {
		code = ErrorCodeCredentialUnavailable
	}
	return &SelectionError{Code: code, cause: err}
}
