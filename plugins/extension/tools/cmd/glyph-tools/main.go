// Command glyph-tools serves the standard Extension Contract v1 implementation.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/app"
)

// main assembles and serves the standard tools extension process.
func main() {
	if err := app.Serve(); err != nil {
		slog.ErrorContext(context.Background(), "serve standard tools extension", "error", err)
		os.Exit(1)
	}
}
