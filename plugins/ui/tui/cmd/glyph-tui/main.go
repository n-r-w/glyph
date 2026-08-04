// Package main starts the standard TUI plugin process.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/n-r-w/glyph/plugins/ui/tui/internal/app"
)

// main serves the standard TUI through the public UI plugin transport.
func main() {
	if err := app.Serve(); err != nil {
		slog.ErrorContext(context.Background(), "serve standard TUI plugin", "error", err)
		os.Exit(1)
	}
}
