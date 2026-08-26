package operationgate

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReleaseIsIdempotentAndDoesNotReleaseLaterOwner(t *testing.T) {
	t.Parallel()

	gate := New()
	release, acquired := gate.TryAcquire()
	require.True(t, acquired)
	release()

	laterRelease, acquired := gate.TryAcquire()
	require.True(t, acquired)
	release()
	_, acquired = gate.TryAcquire()
	require.False(t, acquired)
	laterRelease()
}
