//go:build !integration

package extensionruntime

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestAcceptDiscoveryExcludesEveryDuplicate preserves independent extension acceptance.
func TestAcceptDiscoveryExcludesEveryDuplicate(t *testing.T) {
	t.Parallel()
	// Arrange raw executable observations and one filesystem failure.
	cause := errors.New("inspect cause")
	observed := Discovery{Candidates: []Executable{
		{ID: "good-tool", Path: "/plugins/Good_Tool"},
		{ID: "good-tool", Path: "/plugins/good tool"},
		{ID: "other-tool", Path: "/plugins/Other__Tool"},
		{ID: "", Path: "/plugins/___"},
	}, Issues: []Issue{{PluginIDs: nil, Path: "/plugins/broken", Err: cause}}, DirectoryError: nil}
	// Act at the runtime acceptance owner.
	result, err := (&Service{}).acceptDiscovery(startup.Directory{Path: "/plugins", Explicit: true}, observed)
	// Assert unaffected candidates remain and all rejected observations retain issues.
	require.NoError(t, err)
	assert.Equal(t, []Executable{{ID: "other-tool", Path: "/plugins/Other__Tool"}}, result.Candidates)
	require.Len(t, result.Issues, 4)
	assert.Equal(
		t,
		[]string{"/plugins/Good_Tool", "/plugins/___", "/plugins/broken", "/plugins/good tool"},
		[]string{result.Issues[0].Path, result.Issues[1].Path, result.Issues[2].Path, result.Issues[3].Path},
	)
	require.ErrorIs(t, result.Issues[2].Err, cause)
}

// TestAcceptDiscoveryDirectoryPolicy preserves default absence, default isolation and explicit failure.
func TestAcceptDiscoveryDirectoryPolicy(t *testing.T) {
	t.Parallel()
	// Arrange a complete filesystem cause without real filesystem dependencies.
	missing := &os.PathError{Op: "readdir", Path: "/missing", Err: os.ErrNotExist}
	denied := &os.PathError{Op: "readdir", Path: "/missing", Err: os.ErrPermission}
	service := &Service{}
	// Act on each established directory policy.
	absent, absentErr := service.acceptDiscovery(
		startup.Directory{Path: "/missing", Explicit: false},
		Discovery{Candidates: nil, Issues: nil, DirectoryError: missing},
	)
	isolated, isolatedErr := service.acceptDiscovery(
		startup.Directory{Path: "/missing", Explicit: false},
		Discovery{Candidates: nil, Issues: nil, DirectoryError: denied},
	)
	_, explicitErr := service.acceptDiscovery(
		startup.Directory{Path: "/missing", Explicit: true},
		Discovery{Candidates: nil, Issues: nil, DirectoryError: missing},
	)
	// Assert complete causes and distinct acceptance outcomes.
	require.NoError(t, absentErr)
	assert.Empty(t, absent.Issues)
	require.NoError(t, isolatedErr)
	require.Len(t, isolated.Issues, 1)
	require.ErrorIs(t, isolated.Issues[0].Err, denied)
	require.ErrorIs(t, explicitErr, missing)
	require.ErrorContains(t, explicitErr, "read explicit extension directory")
}
