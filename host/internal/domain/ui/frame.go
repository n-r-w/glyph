package ui

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// NewFrame creates one frame with every optional payload absent.
func NewFrame(kind FrameKind) Frame {
	return Frame{
		Kind: kind, Initialization: mo.None[Initialization](), Lifecycle: mo.None[Lifecycle](),
		AuthorizationURL: mo.None[string](), Text: mo.None[string](), ErrorCode: mo.None[string](),
		ModelSelection: mo.None[ModelSelection](), SessionInfo: mo.None[session.Info](), Sessions: nil,
		SessionEntries: nil, SessionStatistics: mo.None[session.Statistics](), SessionTree: mo.None[SessionTree](),
		TreeNavigation: mo.None[TreeNavigationResult](),
	}
}
