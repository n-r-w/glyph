//go:build !integration

package operation

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestConstructionRejectsMissingDependencies verifies validation leaves later construction usable.
func TestConstructionRejectsMissingDependencies(t *testing.T) {
	t.Parallel()
	// Arrange independent invalid dependency scenarios.
	t.Run("writer", func(t *testing.T) {
		t.Parallel()
		// Act with an absent transport callback.
		writerInvalid, err := NewWriter[string](nil)
		require.ErrorContains(t, err, "send function is required")
		require.Nil(t, writerInvalid)
		// Assert a later writer can deliver queued work.
		writer, constructionErr := NewWriter(func(string) error { return nil })
		require.NoError(t, constructionErr)
		require.NoError(t, writer.Enqueue("work", nil))
		writer.Close()
		require.NoError(t, writer.Run(t.Context()))
	})
	t.Run("owner", func(t *testing.T) {
		t.Parallel()
		// Act with an absent delivery dependency.
		ownerInvalid, err := NewOwner[string, string](t.Context(), nil)
		require.ErrorContains(t, err, "delivery is required")
		require.Nil(t, ownerInvalid)
		// Assert a later owner can close without outstanding work.
		delivery := NewMockDelivery[string, string](gomock.NewController(t))
		owner, constructionErr := NewOwner(t.Context(), delivery)
		require.NoError(t, constructionErr)
		owner.Close()
		owner.Wait()
	})
}
