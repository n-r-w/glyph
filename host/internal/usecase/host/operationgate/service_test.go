//go:build !integration

package operationgate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestReleaseIsIdempotentAndDoesNotReleaseLaterOwner verifies a stale release cannot release a later reservation.
func TestReleaseIsIdempotentAndDoesNotReleaseLaterOwner(t *testing.T) {
	t.Parallel()

	// Arrange an operation gate with one acquired reservation.
	gate := New()
	release, acquired := gate.TryAcquire()
	require.True(t, acquired)

	// Act by releasing, acquiring a later owner, and invoking the stale release again.
	release()
	laterRelease, acquired := gate.TryAcquire()
	require.True(t, acquired)
	release()
	_, acquired = gate.TryAcquire()

	// Assert the later owner retains exclusion until its own release function runs.
	require.False(t, acquired)
	laterRelease()
}
