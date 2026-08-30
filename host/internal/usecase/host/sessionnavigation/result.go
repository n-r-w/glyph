// Package sessionnavigation defines client-neutral session-tree navigation results.
package sessionnavigation

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// Result contains committed no-summary navigation state.
type Result struct {
	// Tree is the complete committed active-session tree.
	Tree session.Tree
	// ActiveLeafID identifies the committed destination or is absent for the implicit root.
	ActiveLeafID mo.Option[string]
	// ActiveBranch contains committed active-branch entries in root-first order.
	ActiveBranch []session.Entry
	// NextInput contains exact selected user text when the navigation target is a user message.
	NextInput mo.Option[string]
}
