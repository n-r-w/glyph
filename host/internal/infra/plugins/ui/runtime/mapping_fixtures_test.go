//go:build !integration

package runtime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"

	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// testSimpleFrame creates one authorization or information mapping fixture.
func testSimpleFrame(kind controllerui.FrameKind, text string) controllerui.Frame {
	frame := controllerui.NewFrame(kind)
	if kind == controllerui.FrameAuthorization {
		frame.AuthorizationURL = mo.Some(text)
	}
	return frame
}

// testModelSelectionFrame creates one Host-confirmed selection frame.
func testModelSelectionFrame() controllerui.Frame {
	return controllerui.Frame{
		NextInput:      mo.None[string](),
		SessionEntries: nil,
		Kind:           controllerui.FrameModelSelectionChanged,

		Lifecycle:        mo.None[controllerui.Lifecycle](),
		AuthorizationURL: mo.None[string](),

		ModelSelection: mo.Some(model.Selection{
			Provider:        "openrouter",
			Model:           "sonnet",
			ReasoningChoice: model.ReasoningChoiceHigh,
		}),
		SessionInfo:            mo.None[session.Info](),
		Sessions:               nil,
		SessionStatistics:      mo.None[session.Statistics](),
		SessionTree:            mo.None[controllerui.SessionTree](),
		TreeNavigation:         mo.None[controllerui.TreeNavigationResult](),
		TreeNavigationProgress: mo.None[controllerui.TreeNavigationProgress](),
	}
}

func testUIReasoningCapabilities(choices ...model.ReasoningChoice) model.ReasoningCapabilities {
	return model.ReasoningCapabilities{
		Supported: true,
		Choices:   choices,
		Default:   choices[len(choices)-1],
	}
}
