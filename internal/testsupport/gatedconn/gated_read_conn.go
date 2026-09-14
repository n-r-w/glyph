// Package gatedconn provides a gated network connection for integration tests.
package gatedconn

import (
	"net"
	"sync"
	"sync/atomic"
)

// Conn stops transport reads at an explicit test barrier.
type Conn struct {
	// Conn is the real network connection.
	net.Conn
	// gated selects whether new reads wait for release.
	gated atomic.Bool
	// blocked signals that a transport read reached the gate.
	blocked chan struct{}
	// release allows gated reads to continue during normal or failed cleanup.
	release chan struct{}
	// blockedOnce closes the blocked signal once.
	blockedOnce sync.Once
	// releaseOnce closes the release signal once.
	releaseOnce sync.Once
}

var _ net.Conn = (*Conn)(nil)

// New wraps a network connection with a read gate.
func New(connection net.Conn) *Conn {
	return &Conn{
		Conn:        connection,
		gated:       atomic.Bool{},
		blocked:     make(chan struct{}),
		release:     make(chan struct{}),
		blockedOnce: sync.Once{},
		releaseOnce: sync.Once{},
	}
}

// Read waits at the configured transport barrier before reading more bytes.
func (connection *Conn) Read(buffer []byte) (int, error) {
	if connection.gated.Load() {
		connection.blockedOnce.Do(func() { close(connection.blocked) })
		<-connection.release
	}
	return connection.Conn.Read(buffer)
}

// BlockReads makes the next transport Read wait at the gate.
func (connection *Conn) BlockReads() {
	connection.gated.Store(true)
}

// Blocked returns the signal closed when a transport read reaches the gate.
func (connection *Conn) Blocked() <-chan struct{} {
	return connection.blocked
}

// ReleaseReads unblocks transport reads exactly once for failure-safe cleanup.
func (connection *Conn) ReleaseReads() {
	connection.releaseOnce.Do(func() { close(connection.release) })
}

// Close releases a gated read before closing the real connection.
func (connection *Conn) Close() error {
	connection.ReleaseReads()
	return connection.Conn.Close()
}
