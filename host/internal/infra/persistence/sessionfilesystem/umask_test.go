//go:build integration && (darwin || linux || freebsd || openbsd || netbsd)

package sessionfilesystem_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

const restrictiveUmaskHelperEnvironment = "GLYPH_SESSION_RESTRICTIVE_UMASK_HELPER"

// TestInitialSessionFileHasExactModeUnderRestrictiveUmask verifies file mode ignores a restrictive inherited umask.
func TestInitialSessionFileHasExactModeUnderRestrictiveUmask(t *testing.T) {
	t.Parallel()

	// Arrange helper mode or a subprocess configured to enter helper mode.
	if os.Getenv(restrictiveUmaskHelperEnvironment) == "1" {
		runRestrictiveUmaskApply(t)
		return
	}
	command := exec.CommandContext(
		t.Context(),
		os.Args[0],
		"-test.run=^TestInitialSessionFileHasExactModeUnderRestrictiveUmask$",
	)
	command.Env = append(os.Environ(), restrictiveUmaskHelperEnvironment+"=1")
	// Act by running the append in the isolated subprocess.
	output, err := command.CombinedOutput()

	// Assert the helper completes its exact-mode check successfully.
	require.NoError(t, err, string(output))
}

func runRestrictiveUmaskApply(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	projectPath := filepath.Join(base, "project")
	require.NoError(t, os.Mkdir(projectPath, 0o700))
	project, err := sessionstore.CanonicalWorkingDirectory(projectPath)
	require.NoError(t, err)
	oldMask := syscall.Umask(0o777)
	t.Cleanup(func() { syscall.Umask(oldMask) })
	repository := sessionstore.New(filepath.Join(base, "sessions"), project, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	createdAt := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	result, err := repository.Apply(t.Context(), hostsessions.ApplyCommand{
		Header:      session.Header{Version: 2, ID: "umask", CreatedAt: createdAt, WorkingDirectory: project},
		StoragePath: "",
		Mutation:    sessionInformationMutation(sessionInformationEntry("entry", createdAt, "restricted")),
	})
	require.NoError(t, err)
	info, err := os.Stat(result.StoragePath)
	require.NoError(t, err)
	require.Equalf(t, os.FileMode(0o600), info.Mode().Perm(), "mode was %04o", info.Mode().Perm())
	reopened := sessionstore.New(filepath.Join(base, "sessions"), project, sessionfilesystem.New())
	loaded, err := reopened.Load(t.Context(), "umask")
	require.NoError(t, err)
	require.Equal(t, mo.Some(session.Information{Name: "restricted"}), loaded.Information)
}
