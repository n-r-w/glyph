// Package app assembles and serves the standard TUI plugin process.
package app

import (
	plugininput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuiinput "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
	hostinfra "github.com/n-r-w/glyph/plugins/ui/tui/internal/infra/host"
	terminalinfra "github.com/n-r-w/glyph/plugins/ui/tui/internal/infra/terminal"
	terminaldevice "github.com/n-r-w/glyph/plugins/ui/tui/internal/infra/terminal/device"
	"github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// Serve binds concrete input, application, SDK, and terminal owners before activation.
func Serve() error {
	host := hostinfra.New()
	terminal := terminalinfra.NewRuntime(terminaldevice.New())
	application := presentation.New(host, terminal, terminal)
	plugin := plugininput.New(application)
	input := tuiinput.New(application)
	host.BindInput(plugin)
	terminal.BindInput(input, plugin, host)
	uisdk.Serve(host)
	return nil
}
