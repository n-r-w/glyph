package ui

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// NewFrame creates one frame with every optional payload absent.
func NewFrame(kind FrameKind) Frame {
	return Frame{
		NextInput: mo.None[string](),
		Kind:      kind, Lifecycle: mo.None[Lifecycle](),
		AuthorizationURL: mo.None[string](),
		ModelSelection:   mo.None[model.Selection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](), SessionTree: mo.None[SessionTree](),
		TreeNavigationProgress: mo.None[TreeNavigationProgress](),
		TreeNavigation:         mo.None[TreeNavigationResult](),
	}
}
