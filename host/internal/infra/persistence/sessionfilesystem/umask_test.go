//go:build darwin || linux || freebsd || openbsd || netbsd

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

func TestInitialSessionFileHasExactModeUnderRestrictiveUmask(t *testing.T) {
	t.Parallel()

	if os.Getenv(restrictiveUmaskHelperEnvironment) == "1" {
		runRestrictiveUmaskAppend(t)
		return
	}
	command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestInitialSessionFileHasExactModeUnderRestrictiveUmask$")
	command.Env = append(os.Environ(), restrictiveUmaskHelperEnvironment+"=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
}

func runRestrictiveUmaskAppend(t *testing.T) {
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
	result, err := repository.Append(t.Context(), hostsessions.AppendCommand{
		Header:      session.Header{Version: 1, ID: "umask", CreatedAt: createdAt, WorkingDirectory: project},
		StoragePath: "",
		Entry: session.Entry{
			ID: "entry", CreatedAt: createdAt,
			Information: mo.Some(session.Information{Name: "restricted"}),
		},
	})
	require.NoError(t, err)
	info, err := os.Stat(result.StoragePath)
	require.NoError(t, err)
	require.Equalf(t, os.FileMode(0o600), info.Mode().Perm(), "mode was %04o", info.Mode().Perm())
	reopened := sessionstore.New(filepath.Join(base, "sessions"), project, sessionfilesystem.New())
	loaded, err := reopened.Load(t.Context(), "umask")
	require.NoError(t, err)
	require.Equal(t, "restricted", loaded.Entries[0].Information.MustGet().Name)
}
