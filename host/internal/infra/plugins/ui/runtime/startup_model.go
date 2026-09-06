package runtime

import (
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// ContentSeverity identifies startup content importance.
type ContentSeverity uint8

const (
	// ContentSeverityInformation identifies normal startup content.
	ContentSeverityInformation ContentSeverity = iota + 1
	// ContentSeverityError identifies one startup failure.
	ContentSeverityError
	// ContentSeverityWarning identifies one non-fatal automatic exclusion.
	ContentSeverityWarning
)

// StartupContent carries one initialization information or error item.
type StartupContent struct {
	// Severity identifies the content importance.
	Severity ContentSeverity
	// Text contains the user-visible startup message.
	Text string
}

// ExtensionAvailability identifies one available extension and its tool names.
type ExtensionAvailability struct {
	// PluginID identifies the available extension.
	PluginID string
	// Path is the extension executable path.
	Path string
	// Tools lists model-callable tool names.
	Tools []string
}

// Initialization is the first Host frame sent to a selected UI.
type Initialization struct {
	// SelectedUIID identifies the UI plugin selected by the Host.
	SelectedUIID string
	// StartupContent contains ordered startup messages.
	StartupContent []StartupContent
	// Extensions lists available extensions and their tools.
	Extensions []ExtensionAvailability
	// Availability identifies which user actions the Host accepts.
	Availability hostui.Availability
	// Models lists selectable configured models.
	Models []model.Descriptor
	// ModelSelection contains the active model selection when configured.
	ModelSelection mo.Option[model.Selection]
	// SessionInfo identifies the empty active session created before UI startup.
	SessionInfo session.Info
}
