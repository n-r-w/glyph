package sessiontree

import (
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// navigationError preserves the original navigation cause identity and its client category.
type navigationError struct {
	// code identifies the source failure independently of diagnostic text.
	code string
	// text retains the established navigation error message.
	text string
}

var (
	_ ui.NavigationFailure           = (*navigationError)(nil)
	_ programmatic.NavigationFailure = (*navigationError)(nil)
)

// Error returns the complete source diagnostic.
func (e *navigationError) Error() string { return e.text }

// NavigationCode exposes the source-owned client category.
func (e *navigationError) NavigationCode() string { return e.code }
