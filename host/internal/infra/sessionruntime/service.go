// Package sessionruntime provides process clock and identifier dependencies for active sessions.
package sessionruntime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

const randomIDBytes = 16

// CryptoIDGenerator creates opaque identifiers from cryptographic randomness.
type CryptoIDGenerator struct{}

var _ hostsessions.IDGenerator = CryptoIDGenerator{}

// NewID returns one nonempty random identifier.
func (CryptoIDGenerator) NewID() (string, error) {
	data := make([]byte, randomIDBytes)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("read secure randomness: %w", err)
	}
	return hex.EncodeToString(data), nil
}

// SystemClock returns UTC wall-clock timestamps.
type SystemClock struct{}

var _ hostsessions.Clock = SystemClock{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time { return time.Now().UTC() }
