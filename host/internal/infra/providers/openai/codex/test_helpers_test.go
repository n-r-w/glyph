package codex

import (
	"encoding/base64"
	"encoding/json/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// testJWT encodes unsigned claims used only for provider routing tests.
func testJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "none"})
	require.NoError(t, err)
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(header) + "." +
		base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}
