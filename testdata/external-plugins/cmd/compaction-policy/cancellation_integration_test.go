//go:build integration

// Package main runs the public compaction policy reference fixture.
package main

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// callerCancellationSuite owns real cancellation sockets and caller diagnostics.
type callerCancellationSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestCallerCancellationRetainsFailures checks acquired coordination failures and owned shutdown.
func TestCallerCancellationRetainsFailures(t *testing.T) {
	// Arrange an independent socket scenario.
	t.Parallel()
	// Act through the cancellation scenario suite.
	suite.Run(t, &callerCancellationSuite{Suite: suite.Suite{}})
	// Assert in the scenario method.
}

// TestScenario checks failure retention, unexpected cancellation, and joined ordinary shutdown.
func (s *callerCancellationSuite) TestScenario() {
	t := s.T()
	// Arrange an externally closed real listener and an already acquired committed outcome.
	// originalCause identifies the caller failure acquired before teardown.
	const originalCause = "acquired caller failure before coordination finish"
	// committedSummary contains the durable receipt content.
	const committedSummary = "retained receipt summary"
	listener, err := new(
		net.ListenConfig,
	).Listen(t.Context(), fixtureSocketNetwork, filepath.Join(t.TempDir(), "cancel.sock"))
	require.NoError(t, err)
	require.NoError(t, listener.Close())
	// acceptCause contains the real closed-listener cause, including its operation and path.
	_, acceptCause := listener.Accept()
	require.ErrorIs(t, acceptCause, net.ErrClosed)
	_, finish := startCallerCancellation(t.Context(), listener)
	report := callerReport{Committed: true, Summary: committedSummary, Error: originalCause, Canceled: true}
	// Act by joining worker teardown before the report is final.
	report.finishCancellation(finish)
	// Assert retained acquired failure and durable outcome without a timing wait.
	require.Contains(t, report.Error, acceptCause.Error())
	require.Contains(t, report.Error, originalCause)
	require.True(t, report.Committed)
	require.True(t, report.Canceled)
	require.Equal(t, committedSummary, report.Summary)

	// Arrange a live listener whose unexpected close must cancel pending caller work.
	unexpected, err := new(
		net.ListenConfig,
	).Listen(t.Context(), fixtureSocketNetwork, filepath.Join(t.TempDir(), "cancel.sock"))
	require.NoError(t, err)
	owner, joinUnexpected := startCallerCancellation(t.Context(), unexpected)
	// Act by closing the listener outside the owning finish function.
	require.NoError(t, unexpected.Close())
	<-owner.Done()
	// Assert the original accept cause and the joined worker failure.
	require.ErrorIs(t, context.Cause(owner), net.ErrClosed)
	require.ErrorIs(t, joinUnexpected(), net.ErrClosed)

	// Arrange an owner-controlled listener with no incoming cancellation signal.
	shutdown, err := new(
		net.ListenConfig,
	).Listen(t.Context(), fixtureSocketNetwork, filepath.Join(t.TempDir(), "cancel.sock"))
	require.NoError(t, err)
	_, joinShutdown := startCallerCancellation(t.Context(), shutdown)
	// Act through the owning shutdown function.
	shutdownErr := joinShutdown()
	// Assert that normal listener shutdown supplies no acquired failure.
	require.NoError(t, shutdownErr)

	// Arrange a real incoming cancellation signal.
	signaled, err := new(
		net.ListenConfig,
	).Listen(t.Context(), fixtureSocketNetwork, filepath.Join(t.TempDir(), "cancel.sock"))
	require.NoError(t, err)
	signaledOwner, joinSignaled := startCallerCancellation(t.Context(), signaled)
	// Act through a real Unix connection.
	connection, err := new(net.Dialer).DialContext(t.Context(), fixtureSocketNetwork, signaled.Addr().String())
	require.NoError(t, err)
	<-signaledOwner.Done()
	// Assert expected cancellation and joined successful connection/listener cleanup.
	require.ErrorIs(t, context.Cause(signaledOwner), context.Canceled)
	require.NoError(t, joinSignaled())
	require.NoError(t, connection.Close())
}
