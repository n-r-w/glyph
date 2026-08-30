//go:build integration

package app

import (
	"bufio"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	malformedRecoveryID   = "malformed-session"
	wrongCWDRecoveryID    = "wrong-cwd-session"
	unsupportedRecoveryID = "unsupported-session"
	interruptedRecoveryID = "interrupted-session"
)

type sessionRecoveryFixture struct {
	malformedID     string
	wrongCWDID      string
	unsupportedID   string
	interruptedID   string
	interruptedPath string
}

// findSessionStoragePath resolves a test fixture from its stored header ID.
func findSessionStoragePath(t *testing.T, dataDirectory, id string) (string, string) {
	t.Helper()
	var matchedPath string
	var workingDirectory string
	err := filepath.WalkDir(
		filepath.Join(dataDirectory, "sessions"),
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(path) != ".jsonl" {
				return nil
			}
			file, openErr := os.Open(path)
			if openErr != nil {
				return openErr
			}
			scanner := bufio.NewScanner(file)
			if !scanner.Scan() {
				return errors.Join(scanner.Err(), file.Close())
			}
			var header struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			}
			decodeErr := json.Unmarshal(scanner.Bytes(), &header)
			closeErr := file.Close()
			if decodeErr != nil || closeErr != nil {
				return errors.Join(decodeErr, closeErr)
			}
			if header.ID == id {
				matchedPath = path
				workingDirectory = header.CWD
			}
			return nil
		},
	)
	require.NoError(t, err)
	require.NotEmpty(t, matchedPath)
	require.NotEmpty(t, workingDirectory)
	return matchedPath, workingDirectory
}

// writeSessionRecoveryFixture adds completed corruption and one interrupted tail beside a valid session.
func writeSessionRecoveryFixture(t *testing.T, storagePath, workingDirectory string) sessionRecoveryFixture {
	t.Helper()
	directory := filepath.Dir(storagePath)
	fixture := sessionRecoveryFixture{
		malformedID:     malformedRecoveryID,
		wrongCWDID:      wrongCWDRecoveryID,
		unsupportedID:   unsupportedRecoveryID,
		interruptedID:   interruptedRecoveryID,
		interruptedPath: filepath.Join(directory, "interrupted.jsonl"),
	}
	header := func(version int, id, cwd string) string {
		return fmt.Sprintf(
			`{"type":"session","version":%d,"id":%q,"createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n",
			version,
			id,
			cwd,
		)
	}
	malformed := header(2, fixture.malformedID, workingDirectory) + `{"type":"session_info"` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(directory, "malformed.jsonl"), []byte(malformed), 0o600))
	wrongCWD := header(2, fixture.wrongCWDID, filepath.Join(string(filepath.Separator), "wrong-project"))
	require.NoError(t, os.WriteFile(filepath.Join(directory, "wrong-cwd.jsonl"), []byte(wrongCWD), 0o600))
	unsupported := header(1, fixture.unsupportedID, workingDirectory)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "unsupported.jsonl"), []byte(unsupported), 0o600))
	user := `{"type":"entry","entry":{"type":"user","id":"preceding-entry","parentId":null,"createdAt":"2026-08-27T10:00:01Z","message":{"content":[{"kind":1,"text":"preceding tail text"}]}}}` + "\n"
	interrupted := header(
		2,
		fixture.interruptedID,
		workingDirectory,
	) + user + `{"type":"entry","entry":{"type":"model","id":"interrupted-entry"`
	require.NoError(t, os.WriteFile(fixture.interruptedPath, []byte(interrupted), 0o640))
	return fixture
}
