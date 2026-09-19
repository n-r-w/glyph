// Package extensionmodels owns extension-facing model catalog and configured-request operations.
package extensionmodels

import (
	"context"
	"time"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=extensionmodels

// Catalog supplies provider-neutral descriptors and active selection.
type Catalog interface {
	// Models returns defensive descriptors in configured order.
	Models() []model.Descriptor
	// ActiveSelection returns the complete active selection.
	ActiveSelection() model.Selection
}

// ModelRequester executes explicit configured model requests.
type ModelRequester interface {
	// Request executes one explicit configured selection without changing active selection.
	RequestConfigured(
		ctx context.Context,
		selection model.Selection,
		instructions string,
		history []agent.HistoryEntry,
		progress func(completedAttempts, attemptLimit int64, delay time.Duration, failure string) error,
	) (model.Response, error)
}

// RequestFailure exposes a provider-owned configured-request failure category.
type RequestFailure interface {
	error
	// SelectionCode returns the provider catalog failure code.
	SelectionCode() string
}

// ContextValidator validates extension bindings at model-operation boundaries.
type ContextValidator interface {
	// ValidateContext rejects a reference not issued to the connected runtime or no longer active.
	ValidateContext(extensionID, runtimeID string, reference extensiondomain.ContextRef) error
}
