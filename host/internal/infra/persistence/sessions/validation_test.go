//go:build integration

package sessions_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
)

const (
	validHeader    = `{"type":"session","version":2,"id":"stored","createdAt":"2026-08-27T10:00:00Z","cwd":%q}` + "\n"
	validEntry     = `{"type":"session_info","sessionInfo":{"createdAt":"2026-08-27T10:00:01Z","name":"Stored"}}` + "\n"
	validTreeEntry = `{"type":"entry","entry":{"type":"user","id":"entry-1",` +
		`"parentId":null,"createdAt":"2026-08-27T10:00:01Z",` +
		`"message":{"content":[]}}}` + "\n"
)

// TestLoadRejectsInvalidCompletedSessionRecords verifies strict validation rejects completed corruption.
func TestLoadRejectsInvalidCompletedSessionRecords(t *testing.T) {
	t.Parallel()

	// Arrange invalid headers and entries that are newline terminated and therefore completed.
	tests := []struct {
		name    string
		content func(string) string
	}{
		{name: "unsupported version", content: func(cwd string) string {
			return fmt.Sprintf(
				`{"type":"session","version":3,"id":"stored","createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n",
				cwd,
			)
		}},
		{name: "unknown header field", content: func(cwd string) string {
			return fmt.Sprintf(
				`{"type":"session","version":2,"id":"stored","createdAt":"2026-08-27T10:00:00Z","cwd":%q,"extra":true}`+"\n",
				cwd,
			)
		}},
		{name: "missing required header field", content: func(string) string {
			return `{"type":"session","version":2,"id":"stored","createdAt":"2026-08-27T10:00:00Z"}` + "\n"
		}},
		{name: "empty header ID", content: func(cwd string) string {
			return fmt.Sprintf(
				`{"type":"session","version":2,"id":"","createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n",
				cwd,
			)
		}},
		{name: "malformed header timestamp", content: func(cwd string) string {
			return fmt.Sprintf(
				`{"type":"session","version":2,"id":"stored","createdAt":"yesterday","cwd":%q}`+"\n",
				cwd,
			)
		}},
		{name: "unknown entry field", content: func(cwd string) string {
			return fmt.Sprintf(
				validHeader,
				cwd,
			) + `{"type":"session_info","sessionInfo":{"createdAt":"2026-08-27T10:00:01Z","name":"Stored","extra":true}}` + "\n"
		}},
		{name: "missing required entry field", content: func(cwd string) string {
			return fmt.Sprintf(
				validHeader,
				cwd,
			) + `{"type":"session_info","sessionInfo":{"createdAt":"2026-08-27T10:00:01Z"}}` + "\n"
		}},
		{name: "empty entry ID", content: func(cwd string) string {
			return fmt.Sprintf(
				validHeader,
				cwd,
			) + `{"type":"entry","entry":{"type":"user","id":"","parentId":null,` +
				`"createdAt":"2026-08-27T10:00:01Z","message":{"content":[]}}}` + "\n"
		}},
		{name: "malformed entry timestamp", content: func(cwd string) string {
			return fmt.Sprintf(
				validHeader,
				cwd,
			) + `{"type":"session_info","sessionInfo":{"createdAt":"yesterday","name":"Stored"}}` + "\n"
		}},
		{name: "missing required user payload", content: func(cwd string) string {
			return fmt.Sprintf(
				validHeader,
				cwd,
			) + `{"type":"entry","entry":{"type":"user","id":"entry-1","createdAt":"2026-08-27T10:00:01Z"}}` + "\n"
		}},
		{name: "unknown nested core field", content: func(cwd string) string {
			return fmt.Sprintf(
				validHeader,
				cwd,
			) + `{"type":"entry","entry":{"type":"user","id":"entry-1",` +
				`"createdAt":"2026-08-27T10:00:01Z","message":{"content":[],` +
				`"extra":true}}}` + "\n"
		}},
		{name: "conflicting entry payload", content: func(cwd string) string {
			return fmt.Sprintf(
				validHeader,
				cwd,
			) + `{"type":"entry","entry":{"type":"user","id":"entry-1",` +
				`"createdAt":"2026-08-27T10:00:01Z","message":{"content":[]},` +
				`"response":{}}}` + "\n"
		}},
		{name: "duplicate entry ID", content: func(cwd string) string {
			return fmt.Sprintf(validHeader, cwd) + validTreeEntry + validTreeEntry
		}},
		{name: "malformed completed record", content: func(cwd string) string {
			return fmt.Sprintf(validHeader, cwd) + `{"type":"session_info","sessionInfo":{"type":"session_info"}` + "\n"
		}},
		{name: "wrong canonical working directory", content: func(string) string {
			return fmt.Sprintf(validHeader, filepath.Join(string(filepath.Separator), "another-project"))
		}},
		{name: "invalid extension JSON", content: func(cwd string) string {
			return fmt.Sprintf(
				validHeader,
				cwd,
			) + `{"type":"entry","entry":{"type":"extension","id":"entry-1",` +
				`"createdAt":"2026-08-27T10:00:01Z","extensionId":"ext",` +
				`"entryType":"item","data":}}` + "\n"
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, projectDirectory, cwd := newValidationRepository(t)
			path := filepath.Join(projectDirectory, "stored.jsonl")
			require.NoError(t, os.WriteFile(path, []byte(test.content(cwd)), 0o600))
			before, err := os.ReadFile(path)
			require.NoError(t, err)

			// Act by loading the completed session file through its validated header ID.
			_, loadErr := repository.Load(t.Context(), session.ID("stored"))

			// Assert completed corruption is rejected without changing stored bytes.
			require.Error(t, loadErr)
			after, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, before, after)
		})
	}
}

// TestEmptyHeaderTimestampIsUnavailable verifies Load and List reject an empty RFC3339 timestamp.
func TestEmptyHeaderTimestampIsUnavailable(t *testing.T) {
	t.Parallel()
	// Arrange one structurally complete header whose timestamp is an empty string.
	repository, projectDirectory, cwd := newValidationRepository(t)
	content := fmt.Sprintf(
		`{"type":"session","version":2,"id":"stored","createdAt":"","cwd":%q}`+"\n"+validEntry,
		cwd,
	)
	require.NoError(t, os.WriteFile(filepath.Join(projectDirectory, "stored.jsonl"), []byte(content), 0o600))

	// Act by loading the stored ID before listing sessions.
	_, loadErr := repository.Load(t.Context(), session.ID("stored"))
	listed, listErr := repository.List(t.Context())

	// Assert both boundaries reject the invalid timestamp.
	require.Error(t, loadErr)
	assert.Contains(t, loadErr.Error(), "decode session header record timestamp")
	assert.Contains(t, loadErr.Error(), `cannot parse ""`)
	require.NoError(t, listErr)
	assert.Empty(t, listed)
}

// TestLoadRetainsSessionParserCauses verifies record context accompanies JSON, timestamp, and base64 diagnostics.
func TestLoadRetainsSessionParserCauses(t *testing.T) {
	t.Parallel()

	// Arrange completed records with one parser failure each.
	tests := []struct {
		name    string
		content func(string) string
		context string
		cause   string
	}{
		{
			name: "header timestamp",
			content: func(cwd string) string {
				return fmt.Sprintf(
					`{"type":"session","version":2,"id":"stored","createdAt":"yesterday","cwd":%q}`+"\n",
					cwd,
				)
			},
			context: "decode session header record",
			cause:   "cannot parse",
		},
		{
			name: "entry discriminator",
			content: func(cwd string) string {
				return fmt.Sprintf(validHeader, cwd) + `{"type":` + "\n"
			},
			context: "decode session mutation record 1",
			cause:   "unexpected EOF",
		},
		{
			name: "entry timestamp",
			content: func(cwd string) string {
				return fmt.Sprintf(
					validHeader,
					cwd,
				) + `{"type":"session_info","sessionInfo":{"createdAt":"yesterday","name":"Stored"}}` + "\n"
			},
			context: "decode session mutation record 1",
			cause:   "cannot parse",
		},
		{
			name: "user image base64",
			content: func(cwd string) string {
				return fmt.Sprintf(
					validHeader,
					cwd,
				) + `{"type":"entry","entry":{"type":"user","id":"entry-1",` +
					`"createdAt":"2026-08-27T10:00:01Z",` +
					`"message":{"content":[{"kind":2,"mediaType":"image/png",` +
					`"data":"%%%"}]}}}` + "\n"
			},
			context: "decode session mutation record 1: user image data",
			cause:   "illegal base64 data",
		},
		{
			name: "tool result image base64",
			content: func(cwd string) string {
				return fmt.Sprintf(
					validHeader,
					cwd,
				) + `{"type":"entry","entry":{"type":"tool_result","id":"entry-1",` +
					`"createdAt":"2026-08-27T10:00:01Z","result":{"callId":"call-1",` +
					`"toolName":"read","contents":[{"kind":2,"mediaType":"image/png",` +
					`"data":"%%%"}],"isError":false}}}` + "\n"
			},
			context: "decode session mutation record 1: tool result image data",
			cause:   "illegal base64 data",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, projectDirectory, cwd := newValidationRepository(t)
			require.NoError(t, os.WriteFile(
				filepath.Join(projectDirectory, "stored.jsonl"),
				[]byte(test.content(cwd)),
				0o600,
			))

			// Act by loading the completed record through its stored session ID.
			_, err := repository.Load(t.Context(), session.ID("stored"))

			// Assert record and field context retain the original parser cause.
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.context)
			assert.Contains(t, err.Error(), test.cause)
		})
	}
}

// TestLoadPrioritizesHeaderDecodeErrors verifies direct header failures precede derived semantic checks.
func TestLoadPrioritizesHeaderDecodeErrors(t *testing.T) {
	t.Parallel()

	// Arrange partial headers where derived checks can also fail.
	tests := []struct {
		name    string
		content func(string) string
		cause   string
	}{
		{
			name: "strict decode before timestamp",
			content: func(cwd string) string {
				return fmt.Sprintf(
					`{"type":"session","version":2,"id":"stored","createdAt":"yesterday","cwd":%q,"extra":true}`+"\n",
					cwd,
				)
			},
			cause: `unknown object member name "extra"`,
		},
		{
			name: "required field before header shape",
			content: func(cwd string) string {
				return fmt.Sprintf(
					`{"version":2,"id":"stored","createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n",
					cwd,
				)
			},
			cause: `required field "type" is missing`,
		},
		{
			name: "null timestamp before timestamp parsing",
			content: func(cwd string) string {
				return fmt.Sprintf(
					`{"type":"session","version":2,"id":"stored","createdAt":null,"cwd":%q}`+"\n",
					cwd,
				)
			},
			cause: `required field "createdAt" is null`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repository, projectDirectory, cwd := newValidationRepository(t)
			require.NoError(t, os.WriteFile(
				filepath.Join(projectDirectory, "stored.jsonl"),
				[]byte(test.content(cwd)),
				0o600,
			))

			// Act by loading the partially decoded header through its retained ID.
			_, err := repository.Load(t.Context(), session.ID("stored"))

			// Assert header context keeps the direct strict-decode or required-field cause.
			require.Error(t, err)
			assert.Contains(t, err.Error(), "decode session header:")
			assert.Contains(t, err.Error(), test.cause)
		})
	}
}

// TestLoadClassifiesMalformedMatchedHeaderUnavailable verifies a valid ID is retained for strict-header failure
// mapping.
func TestLoadClassifiesMalformedMatchedHeaderUnavailable(t *testing.T) {
	t.Parallel()

	// Arrange a structurally identified header with one unknown core field.
	repository, projectDirectory, cwd := newValidationRepository(t)
	content := fmt.Sprintf(
		`{"type":"session","version":2,"id":"stored","createdAt":"2026-08-27T10:00:00Z","cwd":%q,"extra":true}`+"\n",
		cwd,
	)
	require.NoError(t, os.WriteFile(filepath.Join(projectDirectory, "stored.jsonl"), []byte(content), 0o600))

	// Act by explicitly loading the session through its structurally valid header ID.
	_, err := repository.Load(t.Context(), session.ID("stored"))

	// Assert strict schema failure is unavailable rather than indistinguishable from an unknown ID.
	require.ErrorIs(t, err, session.ErrUnavailable)
}

// TestListSkipsNonregularAndInvalidFiles verifies discovery returns only valid regular session files.
func TestListSkipsNonregularAndInvalidFiles(t *testing.T) {
	t.Parallel()

	// Arrange one valid file, one malformed file, and one symlink with a session extension.
	repository, projectDirectory, cwd := newValidationRepository(t)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(projectDirectory, "valid.jsonl"),
			[]byte(fmt.Sprintf(validHeader, cwd)+validEntry),
			0o600,
		),
	)
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(projectDirectory, "invalid.jsonl"),
			[]byte(fmt.Sprintf(validHeader, cwd)+"not-json\n"),
			0o600,
		),
	)
	require.NoError(
		t,
		os.Symlink(filepath.Join(projectDirectory, "valid.jsonl"), filepath.Join(projectDirectory, "linked.jsonl")),
	)

	// Act by listing sessions in the project partition.
	listed, err := repository.List(t.Context())

	// Assert only the regular, strictly valid file is returned.
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, session.ID("stored"), listed[0].Header.ID)
}

// TestLoadDoesNotTreatClientIDAsPath verifies resume lookup uses validated header IDs only.
func TestLoadDoesNotTreatClientIDAsPath(t *testing.T) {
	t.Parallel()

	// Arrange one valid stored session whose filename is unrelated to a path-like client ID.
	repository, projectDirectory, cwd := newValidationRepository(t)
	path := filepath.Join(projectDirectory, "opaque-name.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(validHeader, cwd)+validEntry), 0o600))
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	// Act by loading with a path-like client-provided identifier.
	_, loadErr := repository.Load(t.Context(), session.ID("../opaque-name.jsonl"))

	// Assert lookup fails and does not mutate or resolve the identifier as a path.
	require.ErrorIs(t, loadErr, os.ErrNotExist)
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, before, after)
}

// TestExtensionDataAcceptsEveryJSONValue verifies extension data is opaque valid JSON.
func TestExtensionDataAcceptsEveryJSONValue(t *testing.T) {
	t.Parallel()

	// Arrange each JSON value family in an otherwise valid extension entry.
	values := []string{"null", "true", "42", `"text"`, "[]", "{}"}
	for index, value := range values {
		entry := fmt.Sprintf(
			`{"type":"entry","entry":{"type":"extension","id":"entry-%d",`+
				`"createdAt":"2026-08-27T10:00:01Z","extensionId":"ext",`+
				`"entryType":"item","data":%s}}`+"\n",
			index,
			value,
		)
		repository, projectDirectory, cwd := newValidationRepository(t)
		require.NoError(
			t,
			os.WriteFile(
				filepath.Join(projectDirectory, "stored.jsonl"),
				[]byte(fmt.Sprintf(validHeader, cwd)+entry),
				0o600,
			),
		)

		// Act by loading the valid extension data value.
		loaded, err := repository.Load(t.Context(), session.ID("stored"))

		// Assert the extension entry is accepted for this JSON value family.
		require.NoError(t, err)
		require.Len(t, loaded.Tree.Entries(), 1)
		assert.JSONEq(t, value, string(loaded.Tree.Entries()[0].Extension.MustGet().Data))
	}
}

// TestLoadRepairsReadOnlyCompleteSession verifies direct resume validates before repairing a complete file.
func TestLoadRepairsReadOnlyCompleteSession(t *testing.T) {
	t.Parallel()

	// Arrange a complete valid session that is readable but not writable.
	repository, projectDirectory, cwd := newValidationRepository(t)
	path := filepath.Join(projectDirectory, "stored.jsonl")
	content := []byte(fmt.Sprintf(validHeader, cwd) + validEntry)
	require.NoError(t, os.WriteFile(path, content, 0o400))

	// Act by loading directly without a preceding list operation.
	loaded, err := repository.Load(t.Context(), session.ID("stored"))

	// Assert load succeeds, content stays exact, and owner mode becomes 0600.
	require.NoError(t, err)
	require.Empty(t, loaded.Tree.Entries())
	assert.Equal(t, mo.Some(session.Information{Name: "Stored"}), loaded.Information)
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, content, after)
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestLoadRecoversReadOnlyInterruptedSession verifies direct resume repairs access before tail mutation.
func TestLoadRecoversReadOnlyInterruptedSession(t *testing.T) {
	t.Parallel()

	// Arrange a read-only valid prefix followed by one unterminated final record.
	repository, projectDirectory, cwd := newValidationRepository(t)
	path := filepath.Join(projectDirectory, "stored.jsonl")
	complete := fmt.Sprintf(validHeader, cwd) + validEntry
	interrupted := complete + `{"type":"entry","entry":{"type":"user","id":"entry-2"}`
	require.NoError(t, os.WriteFile(path, []byte(interrupted), 0o400))

	// Act by loading directly without a preceding list operation.
	loaded, err := repository.Load(t.Context(), session.ID("stored"))

	// Assert only the complete prefix remains and final mode is 0600.
	require.NoError(t, err)
	require.Empty(t, loaded.Tree.Entries())
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, []byte(complete), after)
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestLoadKeepsReadOnlyMalformedCompletedSessionImmutable verifies validation failure blocks access repair.
func TestLoadKeepsReadOnlyMalformedCompletedSessionImmutable(t *testing.T) {
	t.Parallel()

	// Arrange a read-only session with one malformed newline-terminated record.
	repository, projectDirectory, cwd := newValidationRepository(t)
	path := filepath.Join(projectDirectory, "stored.jsonl")
	content := []byte(fmt.Sprintf(validHeader, cwd) + "not-json\n")
	require.NoError(t, os.WriteFile(path, content, 0o400))

	// Act by loading directly without a preceding list operation.
	_, err := repository.Load(t.Context(), session.ID("stored"))

	// Assert unavailable storage keeps exact bytes and mode 0400.
	require.ErrorIs(t, err, session.ErrUnavailable)
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, content, after)
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o400), info.Mode().Perm())
}

// TestListPreservesInterruptedTailAndLoadRecoversIt verifies only explicit load repairs one incomplete final append.
func TestListPreservesInterruptedTailAndLoadRecoversIt(t *testing.T) {
	t.Parallel()

	// Arrange a valid header and entry followed by one unterminated final record.
	repository, projectDirectory, cwd := newValidationRepository(t)
	path := filepath.Join(projectDirectory, "stored.jsonl")
	complete := fmt.Sprintf(validHeader, cwd) + validEntry
	interrupted := complete + `{"type":"entry","entry":{"type":"user","id":"entry-2"}`
	require.NoError(t, os.WriteFile(path, []byte(interrupted), 0o640))

	// Act by listing first and then explicitly loading the same session.
	listed, listErr := repository.List(t.Context())
	afterList, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	infoAfterList, statErr := os.Stat(path)
	require.NoError(t, statErr)
	loaded, loadErr := repository.Load(t.Context(), session.ID("stored"))

	// Assert list preserves tail bytes and mode while load truncates, syncs, repairs mode, and restores the prefix.
	require.NoError(t, listErr)
	require.Len(t, listed, 1)
	assert.Equal(t, []byte(interrupted), afterList)
	assert.Equal(t, os.FileMode(0o640), infoAfterList.Mode().Perm())
	require.NoError(t, loadErr)
	require.Empty(t, loaded.Tree.Entries())
	afterLoad, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, []byte(complete), afterLoad)
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestLoadRecoversOnlyTheRequestedHeaderID verifies discovery does not repair another session candidate.
func TestLoadRecoversOnlyTheRequestedHeaderID(t *testing.T) {
	t.Parallel()

	// Arrange one requested complete session and one different session with an interrupted tail.
	repository, projectDirectory, cwd := newValidationRepository(t)
	requestedPath := filepath.Join(projectDirectory, "requested.jsonl")
	require.NoError(t, os.WriteFile(requestedPath, []byte(fmt.Sprintf(validHeader, cwd)+validEntry), 0o600))
	otherHeader := fmt.Sprintf(
		`{"type":"session","version":2,"id":"other","createdAt":"2026-08-27T10:00:00Z","cwd":%q}`+"\n",
		cwd,
	)
	otherPath := filepath.Join(projectDirectory, "000-other.jsonl")
	otherContent := otherHeader + validEntry + `{"type":"entry","entry":{"type":"user"}`
	require.NoError(t, os.WriteFile(otherPath, []byte(otherContent), 0o600))

	// Act by loading only the session identified by the requested validated header.
	loaded, err := repository.Load(t.Context(), session.ID("stored"))

	// Assert the requested session loads and the other interrupted tail remains unchanged.
	require.NoError(t, err)
	assert.Equal(t, session.ID("stored"), loaded.Header.ID)
	after, readErr := os.ReadFile(otherPath)
	require.NoError(t, readErr)
	assert.Equal(t, []byte(otherContent), after)
}

// TestMalformedCompletedRecordNeverChangesFileMetadata verifies rejected corruption does not trigger mode repair.
func TestMalformedCompletedRecordNeverChangesFileMetadata(t *testing.T) {
	t.Parallel()

	// Arrange a wrong-mode session with a malformed newline-terminated entry.
	repository, projectDirectory, cwd := newValidationRepository(t)
	path := filepath.Join(projectDirectory, "stored.jsonl")
	content := fmt.Sprintf(validHeader, cwd) + "not-json\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o640))

	// Act by listing and then explicitly loading the malformed completed session.
	listed, listErr := repository.List(t.Context())
	infoAfterList, statErr := os.Stat(path)
	require.NoError(t, statErr)
	_, loadErr := repository.Load(t.Context(), session.ID("stored"))

	// Assert neither list nor load changes file bytes or mode.
	require.NoError(t, listErr)
	assert.Empty(t, listed)
	assert.Equal(t, os.FileMode(0o640), infoAfterList.Mode().Perm())
	require.Error(t, loadErr)
	after, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, []byte(content), after)
	info, statErr := os.Stat(path)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
}

func newValidationRepository(t *testing.T) (*sessionstore.Service, string, string) {
	t.Helper()
	root := t.TempDir()
	cwd, err := sessionstore.CanonicalWorkingDirectory(t.TempDir())
	require.NoError(t, err)
	repository := sessionstore.New(root, cwd, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	projectDirectory := filepath.Join(root, sessionstore.ProjectDirectoryName(cwd))
	return repository, projectDirectory, cwd
}
