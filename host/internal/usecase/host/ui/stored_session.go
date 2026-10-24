package ui

import (
	"strings"

	"github.com/samber/mo"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// StoredSession supplies validated stored facts for one client list row.
type StoredSession struct {
	// Info contains session identity and lifecycle metadata.
	Info session.Info
	// FirstUserText contains exact text selected from the active branch.
	FirstUserText mo.Option[string]
	// TotalMessages counts terminal messages across the complete tree.
	TotalMessages int
}

// isListLineBreak identifies the delimiters used in public session previews.
func isListLineBreak(value rune) bool {
	return value == '\r' || value == '\n'
}

// publicItem normalizes stored text while preserving metadata, count, and optional presence.
func (stored StoredSession) publicItem() controllerui.SessionListItem {
	return controllerui.SessionListItem{
		Info: stored.Info,
		FirstUserText: stored.FirstUserText.Map(func(text string) (string, bool) {
			return strings.TrimSpace(strings.Join(strings.FieldsFunc(text, isListLineBreak), " ")), true
		}),
		TotalMessages: stored.TotalMessages,
	}
}
