// Package extension maps extension-initiated requests to session-bound Host operations.
package extension

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock_test.go -package=extension

// ContextOperations supplies bound catalog reads without transport policy.
type ContextOperations interface {
	// ValidateContext rejects a reference not issued to the connected runtime or no longer active.
	ValidateContext(extensionID, runtimeID string, reference extensiondomain.ContextRef) error
	// ReadModels returns a defensive provider-neutral catalog after binding revalidation.
	ReadModels(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
	) (ModelCatalog, error)
	// ReadProviders returns provider identifiers and ordered model identifiers after revalidation.
	ReadProviders(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
	) ([]Provider, error)
	// Request executes one explicit configured selection after binding revalidation.
	Request(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
		selection model.Selection,
		instructions string,
		history []agent.HistoryEntry,
	) (model.Response, error)
	// AppendExtension persists one hidden entry under the bound session incarnation.
	AppendExtension(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
		entryType string,
		data []byte,
	) (session.Entry, error)
	// ReadSessionState returns one coherent caller-filtered active-branch snapshot.
	ReadSessionState(
		ctx context.Context,
		extensionID, runtimeID string,
		reference extensiondomain.ContextRef,
	) (SessionState, error)
}

// SessionState contains one coherent recovery result at the controller consumer boundary.
type SessionState struct {
	// SessionID identifies the bound durable session.
	SessionID session.ID
	// ActiveLeafID identifies the snapshot leaf when present.
	ActiveLeafID mo.Option[string]
	// Entries contains the caller extension's root-first active-branch entries.
	Entries []session.Entry
}

// ModelCatalog contains descriptors and active selection for one completed read.
type ModelCatalog struct {
	// Models contains complete provider-neutral descriptors in configured order.
	Models []model.Descriptor
	// Selection contains the active provider, model, and reasoning choice.
	Selection model.Selection
}

// Provider contains the public projection of one configured provider.
type Provider struct {
	// ID identifies the configured provider.
	ID model.ProviderID
	// ModelIDs contains model identifiers in configured order.
	ModelIDs []model.ID
}

// ContextFailure exposes the closed context-operation failure category.
type ContextFailure interface {
	error
	// ContextCode returns the category without replacing the complete error text.
	ContextCode() string
}

// RuntimeOperations accounts for extension-initiated work against the connected process instance.
type RuntimeOperations interface {
	// BeginContextOperation reserves one active operation and returns its release function.
	BeginContextOperation(ctx context.Context, extensionID, runtimeID string) (func(), error)
}
