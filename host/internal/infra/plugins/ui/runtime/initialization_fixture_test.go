//go:build integration

package runtime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// testInitialization provides typed startup state for transport scenarios.
func testInitialization() hostui.Initialization {
	return hostui.Initialization{
		Availability:   hostui.AvailabilityCheckingAuthentication,
		Models:         nil,
		ModelSelection: mo.Some(model.Selection{}),
		SessionInfo:    session.Info{},
	}
}
