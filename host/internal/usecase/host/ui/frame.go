package ui

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// modelSelectionChangedFrame confirms a committed command selection.
func modelSelectionChangedFrame(selection model.Selection) controllerui.Frame {
	frame := controllerui.NewFrame(controllerui.FrameModelSelectionChanged)
	frame.ModelSelection = mo.Some(selection)
	return frame
}
