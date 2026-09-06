package runtime

import (
	"github.com/samber/mo"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
)

// lifecycleFrame constructs operation progress from a mapped agent fact.
func lifecycleFrame(lifecycle controllerui.Lifecycle) controllerui.Frame {
	frame := controllerui.NewFrame(controllerui.FrameLifecycle)
	frame.Lifecycle = mo.Some(lifecycle)
	return frame
}

// authorizationFrame constructs operation-scoped authorization progress.
func authorizationFrame(url string) controllerui.Frame {
	frame := controllerui.NewFrame(controllerui.FrameAuthorization)
	frame.AuthorizationURL = mo.Some(url)
	return frame
}
