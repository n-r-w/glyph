package ui

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// SessionListItem describes one validated stored session for the client list.
type SessionListItem struct {
	// Info contains identity and lifecycle timestamps.
	Info session.Info
	// FirstUserText contains the first nonempty active-branch user text.
	FirstUserText mo.Option[string]
	// TotalMessages counts terminal messages across the complete stored tree.
	TotalMessages int
}
