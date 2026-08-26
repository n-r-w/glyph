// Package sessioncontrol coordinates client lifecycle commands with agent execution.
package sessioncontrol

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessioncontrol

// ActiveSessions exposes only lifecycle operations used by client orchestration.
type ActiveSessions interface {
	// CreateActive commits a new empty active session.
	CreateActive(context.Context) (session.Info, error)
	// ResumeActive validates storage before atomically replacing active state.
	ResumeActive(context.Context, session.ID) (session.Info, error)
	// SetActiveName persists a normalized name without replacing active state.
	SetActiveName(context.Context, string) (session.Info, error)
	// ListStored returns ordered persisted-session summaries.
	ListStored(context.Context) ([]session.Summary, error)
	// ActiveInfo returns an independent active-session snapshot.
	ActiveInfo() session.Info
}

// OperationGate reserves active-session replacement against agent execution.
type OperationGate interface {
	// TryAcquire reserves replacement without waiting and returns idempotent cleanup on success.
	TryAcquire() (release func(), acquired bool)
}
