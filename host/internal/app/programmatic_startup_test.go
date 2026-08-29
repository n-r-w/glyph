package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"

	"io"

	"os"

	"path/filepath"

	"strings"

	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/controller/cli"
	"github.com/n-r-w/glyph/host/internal/controller/cli/headless"
)

// TestRunWithPathsRPCInvalidSettingsStopsBeforeSocket verifies capability validation precedes socket creation.
func TestRunWithPathsRPCInvalidSettingsStopsBeforeSocket(t *testing.T) {
	t.Parallel()

	// Arrange invalid model input and a dedicated socket path.
	paths := testPaths(t, strings.Replace(codexSettings(""), "input: [text]", "input: [image]", 1))
	socketPath := filepath.Join(t.TempDir(), "glyph.sock")
	var stdout bytes.Buffer

	// Act by starting RPC mode with invalid model capabilities.
	err := runWithPaths(t.Context(), paths, cli.Command{
		Mode:               cli.ModeRPC,
		ExtensionDirectory: t.TempDir(),
		SocketPath:         socketPath,
		Headless:           headless.Command{},
		UIDirectory:        "",
		UIID:               "",
	}, &stdout, &bytes.Buffer{})

	// Assert the field-specific error arrives before socket creation or announcement.
	require.ErrorContains(t, err, `provider "openai-codex" model "gpt-test": input must contain "text"`)
	_, statErr := os.Lstat(socketPath)
	require.ErrorIs(t, statErr, os.ErrNotExist)
	assert.Empty(t, stdout.String())
}

// TestRunWithPathsRPCPublishesSocketAndCleansUp verifies the RPC process boundary on application cancellation.
func TestRunWithPathsRPCPublishesSocketAndCleansUp(t *testing.T) {
	t.Parallel()

	paths := testPaths(t, codexSettings(""))
	extensionDirectory := t.TempDir()
	socketDirectory, err := os.MkdirTemp("/tmp", "glyph-app-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(socketDirectory)) })
	socketPath := filepath.Join(socketDirectory, "glyph.sock")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	reader, writer := io.Pipe()
	lineResult := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(reader)
		if scanner.Scan() {
			lineResult <- scanner.Text()
			cancel()
			return
		}
		lineResult <- ""
	}()

	var stderr bytes.Buffer
	runErr := runWithPaths(ctx, paths, cli.Command{
		Mode:               cli.ModeRPC,
		ExtensionDirectory: extensionDirectory,
		SocketPath:         socketPath,
		Headless:           headless.Command{},
		UIDirectory:        "",
		UIID:               "",
	}, writer, &stderr)
	require.NoError(t, writer.Close())
	line := <-lineResult
	require.NoError(t, reader.Close())

	require.ErrorIs(t, runErr, context.Canceled)
	var announcement struct {
		Socket string `json:"socket"`
	}
	require.NoError(t, json.Unmarshal([]byte(line), &announcement))
	assert.Equal(t, socketPath, announcement.Socket)
	_, statErr := os.Lstat(socketPath)
	assert.ErrorIs(t, statErr, os.ErrNotExist)
}
