//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"

	uisdk "github.com/n-r-w/glyph/sdk/plugins/ui/v1"
)

// startManualUICancellation owns the fixture's public UI cancellation request.
func startManualUICancellation(ctx context.Context, host *uisdk.Host) (func() error, error) {
	// manualFixtureCancelObserver identifies the UI observer cancellation request.
	const manualFixtureCancelObserver = "cancel-observer"

	// path identifies the cancellation signal socket.
	path := os.Getenv(manualFixtureGlyphManualCancelSocket)
	if path == "" {
		return func() error { return nil }, nil
	}
	// listener owns the cancellation signal socket.
	listener, err := new(net.ListenConfig).Listen(ctx, manualFixtureUnix, path)
	if err != nil {
		return nil, err
	}
	// result carries the cancellation worker failure.
	result := make(chan error, 1)
	go func() {
		// connection contains the incoming cancellation signal.
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				err = nil
			}
			result <- err
			return
		}
		// cancellation owns the public cancellation operation result.
		cancellation, err := host.Cancel(ctx, manualFixtureCancelObserver, manualFixtureManualUiCompact)
		if err == nil {
			_, err = cancellation.Wait(ctx)
		}
		if err != nil {
			err = errors.Join(err, context.Cause(ctx))
		}
		result <- errors.Join(err, connection.Close())
	}()
	// finish joins the worker and closes its listener once.
	finish := sync.OnceValue(func() error {
		// closeErr contains the listener shutdown failure.
		closeErr := listener.Close()
		if errors.Is(closeErr, net.ErrClosed) {
			closeErr = nil
		}
		return errors.Join(closeErr, <-result)
	})
	return finish, nil
}
