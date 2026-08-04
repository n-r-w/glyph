package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/n-r-w/glyph/host/internal/app"
)

// main exits with the terminal status returned after application cleanup.
func main() {
	os.Exit(runMain())
}

// runMain configures one application signal context and runs the Glyph Host.
func runMain() int {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return execute(ctx, os.Args[1:], os.Stdout, os.Stderr, app.Run)
}
