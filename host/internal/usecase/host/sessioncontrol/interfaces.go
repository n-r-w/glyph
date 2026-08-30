// Package sessioncontrol coordinates client lifecycle commands with agent execution.
package sessioncontrol

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessioncontrol

// ActiveSessions exposes only lifecycle operations used by client orchestration.
type ActiveSessions interface {
	// CreateActive commits a new empty active session.
	CreateActive(context.Context) (session.Replacement, error)
	// ResumeActive validates storage before atomically replacing active state.
	ResumeActive(context.Context, session.ID) (session.Replacement, error)
	// ForkActive persists and activates the path before one selected user message.
	ForkActive(context.Context, string) (session.Replacement, string, error)
	// CloneActive persists and activates a copy of the complete active branch.
	CloneActive(context.Context) (session.Replacement, error)
	// SetLabel persists one entry label and returns the committed tree.
	SetLabel(context.Context, string, string) (session.Tree, error)
	// SetActiveName persists a normalized name without replacing active state.
	SetActiveName(context.Context, string) (session.Info, error)
	// ListStored returns ordered persisted-session summaries.
	ListStored(context.Context) ([]session.Summary, error)
	// ActiveInfo returns an independent active-session snapshot.
	ActiveInfo() session.Info
	// ActiveEntries returns immutable active-branch entries in root-first order.
	ActiveEntries() []session.Entry
	// Tree returns an independent active-session tree snapshot.
	Tree() session.Tree
	// ActiveStatistics derives counts and complete token totals from durable entries.
	ActiveStatistics() session.Statistics
	// ActiveInformation returns metadata and statistics from one locked active snapshot.
	ActiveInformation() session.InformationSnapshot
}

// Navigator performs one internal tree navigation with optional built-in summarization.
type Navigator interface {
	// NavigateTree commits the destination and optional branch summary for one request.
	NavigateTree(context.Context, sessionnavigation.Request) (sessionnavigation.Result, error)
}

// OperationGate reserves active-session mutation against agent execution.
type OperationGate interface {
	// TryAcquire reserves replacement without waiting and returns idempotent cleanup on success.
	TryAcquire() (release func(), acquired bool)
}
