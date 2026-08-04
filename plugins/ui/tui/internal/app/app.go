// Package app assembles and serves the standard TUI plugin process.
package app

import (
	plugincontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/plugin"
	tuicontroller "github.com/n-r-w/glyph/plugins/ui/tui/internal/controller/tui"
	terminalinfra "github.com/n-r-w/glyph/plugins/ui/tui/internal/infra/terminal"
	presentationusecase "github.com/n-r-w/glyph/plugins/ui/tui/internal/usecase/presentation"
	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// Serve assembles the standard TUI and starts the UI plugin server.
func Serve() error {
	projection := presentationusecase.New()
	programs := tuicontroller.NewFactory(projection.Apply)
	controller := plugincontroller.New(terminalinfra.New(), programs)
	uisdk.Serve(controller)
	return nil
}
