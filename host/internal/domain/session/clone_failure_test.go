//go:build !integration

package session

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
)

// TestCloneRejectsAliasedTreeState verifies public value aliases cannot hide reconstruction failures.
func TestCloneRejectsAliasedTreeState(t *testing.T) {
	t.Parallel()
	for _, label := range []bool{false, true} {
		t.Run(map[bool]string{false: "active leaf", true: "label"}[label], func(t *testing.T) {
			t.Parallel()
			// Arrange an initialized empty tree and a value alias with one valid root entry.
			original, err := NewTree(nil, mo.None[string](), nil)
			require.NoError(t, err)
			alias := original
			require.NoError(t, alias.Add(treeUserEntry("x", mo.None[string](), "input", time.Unix(1, 0).UTC())))
			if label {
				require.NoError(t, alias.SetLabel("x", "label"))
			} else {
				require.NoError(t, original.SetActiveLeaf(mo.Some("x")))
			}

			// Act by reconstructing the inconsistent original through the public snapshot API.
			_, err = original.Clone()

			// Assert the full reconstruction cause and a later valid clone in the same process.
			if label {
				require.ErrorContains(t, err, `label target "x" does not exist`)
			} else {
				require.ErrorContains(t, err, "active leaf does not exist")
			}
			cloned, err := alias.Clone()
			require.NoError(t, err)
			require.Equal(t, alias.Entries(), cloned.Entries())
			require.Equal(t, alias.ActiveLeafID(), cloned.ActiveLeafID())
			require.Equal(t, alias.Labels(), cloned.Labels())
		})
	}
}
