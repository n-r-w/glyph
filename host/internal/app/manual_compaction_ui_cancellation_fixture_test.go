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

	path := os.Getenv(manualFixtureGlyphManualCancelSocket)
	if path == "" {
		return func() error { return nil }, nil
	}
	listener, err := new(net.ListenConfig).Listen(ctx, manualFixtureUnix, path)
	if err != nil {
		return nil, err
	}
	result := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				err = nil
			}
			result <- err
			return
		}
		cancellation, err := host.Cancel(ctx, manualFixtureCancelObserver, manualFixtureManualUiCompact)
		if err == nil {
			_, err = cancellation.Wait(ctx)
		}
		if err != nil {
			err = errors.Join(err, context.Cause(ctx))
		}
		result <- errors.Join(err, connection.Close())
	}()
	finish := sync.OnceValue(func() error {
		closeErr := listener.Close()
		if errors.Is(closeErr, net.ErrClosed) {
			closeErr = nil
		}
		return errors.Join(closeErr, <-result)
	})
	return finish, nil
}
