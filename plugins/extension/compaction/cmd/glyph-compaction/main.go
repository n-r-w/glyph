// Command glyph-compaction serves the bundled compaction extension.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/app"
)

// main starts the standard compaction extension process.
func main() {
	if err := app.Serve(); err != nil {
		slog.ErrorContext(context.Background(), "serve standard compaction extension", "error", err)
		os.Exit(1)
	}
}
