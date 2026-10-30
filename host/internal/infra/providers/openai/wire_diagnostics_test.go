//go:build !integration

package openai

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWireErrorPreservesOrdinaryIdentifiers separates protected payload headers from diagnostic secrets.
func TestWireErrorPreservesOrdinaryIdentifiers(t *testing.T) {
	t.Parallel()
	// Arrange protected account headers alongside an actual bearer credential.
	headers := http.Header{
		"Authorization":       []string{"Bearer synthetic-access-token"},
		"Chatgpt-Account-Id":  []string{"ordinary-account"},
		"Openai-Organization": []string{"ordinary-organization"},
		"Openai-Project":      []string{"ordinary-project"},
	}
	source := errors.New("HTTP 429 provider rejection account=ordinary-account organization=ordinary-organization " +
		"project=ordinary-project token=synthetic-access-token: complete original cause")
	// Act through the diagnostic envelope used by the wire bridge.
	err := &wireError{cause: source, secrets: CollectWireSecrets(headers)}
	public := projectPublicWireHeaders(headers, CollectWireSecrets(headers))
	// Assert payload exclusion remains while only the credential is removed from full error text.
	require.ErrorIs(t, err, source)
	assert.Empty(t, public)
	assert.Equal(t, "HTTP 429 provider rejection account=ordinary-account organization=ordinary-organization "+
		"project=ordinary-project token=[REDACTED]: complete original cause", err.Error())
}
