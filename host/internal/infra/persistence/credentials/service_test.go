package credentials

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// credentialFixture mirrors only the generic persistence envelope used by tests.
type credentialFixture struct {
	Version   int                       `json:"version"`
	Providers map[string]jsontext.Value `json:"providers"`
}

// TestServiceSaveLoadDeletePreservesOtherProviders verifies opaque provider isolation.
func TestServiceSaveLoadDeletePreservesOtherProviders(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "credentials.json")
	otherPayload := jsontext.Value(`{"token":"other"}`)
	writeCredentialFixture(t, path, credentialFixture{
		Version: 1,
		Providers: map[string]jsontext.Value{
			"other": otherPayload,
		},
	}, 0o600)
	service := New(path, "openai-codex")
	codexPayload := []byte(`{"access_token":"secret"}`)

	require.NoError(t, service.Save(codexPayload))
	loaded, found, err := service.Load()
	require.NoError(t, err)
	assert.True(t, found)
	assert.JSONEq(t, string(codexPayload), string(loaded))
	stored := readCredentialFixture(t, path)
	assert.JSONEq(t, string(otherPayload), string(stored.Providers["other"]))
	assert.JSONEq(t, string(codexPayload), string(stored.Providers["openai-codex"]))

	require.NoError(t, service.Delete())
	stored = readCredentialFixture(t, path)
	assert.Contains(t, stored.Providers, "other")
	assert.NotContains(t, stored.Providers, "openai-codex")
}

// TestServiceSaveCreatesOwnerOnlyAtomicStore verifies first-save format, permissions, and temporary cleanup.
func TestServiceSaveCreatesOwnerOnlyAtomicStore(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "credentials.json")
	service := New(path, "openai-codex")

	require.NoError(t, service.Save([]byte(`{"refresh_token":"value"}`)))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	stored := readCredentialFixture(t, path)
	assert.Equal(t, 1, stored.Version)
	assert.Contains(t, stored.Providers, "openai-codex")
	entries, err := os.ReadDir(directory)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "credentials.json", entries[0].Name())
}

// TestServiceLoadEnforcesOwnerOnlyFileMode verifies unsafe existing permissions are corrected before use.
func TestServiceLoadEnforcesOwnerOnlyFileMode(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "credentials.json")
	writeCredentialFixture(t, path, credentialFixture{
		Version: 1,
		Providers: map[string]jsontext.Value{
			"openai-codex": jsontext.Value(`{"refresh_token":"value"}`),
		},
	}, 0o644)

	_, found, err := New(path, "openai-codex").Load()

	require.NoError(t, err)
	assert.True(t, found)
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

// TestServiceRejectsInvalidStoreWithoutReplacement verifies malformed/versioned data remains untouched.
func TestServiceRejectsInvalidStoreWithoutReplacement(t *testing.T) {
	t.Parallel()

	testCases := map[string][]byte{
		"malformed":           []byte(`{"version":`),
		"unsupported version": []byte(`{"version":2,"providers":{}}`),
	}
	for name, original := range testCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "credentials.json")
			require.NoError(t, os.WriteFile(path, original, 0o600))

			err := New(path, "openai-codex").Save([]byte(`{"value":true}`))

			require.Error(t, err)
			stored, readErr := os.ReadFile(path)
			require.NoError(t, readErr)
			assert.Equal(t, original, stored)
		})
	}
}

// TestServiceRetainsTrailingStoreDecoderCause verifies trailing malformed JSON keeps its decoder diagnostic.
func TestServiceRetainsTrailingStoreDecoderCause(t *testing.T) {
	t.Parallel()

	// Arrange a valid store followed by one interrupted JSON value.
	path := filepath.Join(t.TempDir(), "credentials.json")
	record := []byte(`{"version":1,"providers":{}} {`)
	require.NoError(t, os.WriteFile(path, record, 0o600))

	// Act by loading the malformed store through the public service.
	_, _, err := New(path, "openai-codex").Load()

	// Assert trailing context and the JSON decoder cause remain visible.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode trailing credential store data")
	assert.Contains(t, err.Error(), "unexpected EOF")
}

// TestServiceRejectsInvalidProviderPayload verifies opaque values must still be valid JSON.
func TestServiceRejectsInvalidProviderPayload(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "credentials.json")

	err := New(path, "openai-codex").Save([]byte(`{"broken":`))

	require.Error(t, err)
	_, statErr := os.Stat(path)
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

// TestServiceLoadAndDeleteMissingStore verifies first-run operations are idempotent.
func TestServiceLoadAndDeleteMissingStore(t *testing.T) {
	t.Parallel()

	service := New(filepath.Join(t.TempDir(), "credentials.json"), "openai-codex")

	payload, found, err := service.Load()
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, payload)
	require.NoError(t, service.Delete())
}

// writeCredentialFixture writes one generic store with the requested test mode.
func writeCredentialFixture(t *testing.T, path string, fixture credentialFixture, mode os.FileMode) {
	t.Helper()
	data, err := json.Marshal(fixture)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, mode))
	require.NoError(t, os.Chmod(path, mode))
}

// readCredentialFixture decodes one generic store after a persistence operation.
func readCredentialFixture(t *testing.T, path string) credentialFixture {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var fixture credentialFixture
	require.NoError(t, json.Unmarshal(data, &fixture))
	return fixture
}
