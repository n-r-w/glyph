// Package sessioncontrol coordinates client lifecycle commands with agent execution.
package sessioncontrol

import (
	"context"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessioncontrol

// ActiveSessions exposes only lifecycle operations used by client orchestration.
type ActiveSessions interface {
	// CreateActive commits a new empty active session.
	CreateActive(context.Context) (session.Replacement, error)
	// ResumeActive validates storage before atomically replacing active state.
	ResumeActive(context.Context, session.ID) (session.Replacement, error)
	// SetActiveName persists a normalized name without replacing active state.
	SetActiveName(context.Context, string) (session.Info, error)
	// ListStored returns ordered persisted-session summaries.
	ListStored(context.Context) ([]session.Summary, error)
	// ActiveInfo returns an independent active-session snapshot.
	ActiveInfo() session.Info
	// ActiveEntries returns immutable active-session entries in stored order.
	ActiveEntries() []session.Entry
	// ActiveStatistics derives counts and complete token totals from durable entries.
	ActiveStatistics() session.Statistics
	// ActiveInformation returns metadata and statistics from one locked active snapshot.
	ActiveInformation() session.InformationSnapshot
}

// NavigationResult contains the committed leaf and optional user text selected for editing.
type NavigationResult struct {
	// ActiveLeafID identifies the committed destination or is absent for the implicit root.
	ActiveLeafID mo.Option[string]
	// NextInput contains exact selected user text when the navigation target is a user message.
	NextInput mo.Option[string]
}

// Navigator performs one internal no-summary tree navigation.
type Navigator interface {
	// NavigateTree commits the destination selected by one target entry.
	NavigateTree(context.Context, string) (NavigationResult, error)
}

// OperationGate reserves active-session mutation against agent execution.
type OperationGate interface {
	// TryAcquire reserves replacement without waiting and returns idempotent cleanup on success.
	TryAcquire() (release func(), acquired bool)
}
