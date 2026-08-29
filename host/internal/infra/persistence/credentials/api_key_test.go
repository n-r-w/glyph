package credentials

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

type APIKeyResolverSuite struct {
	suite.Suite
	path string
}

//nolint:paralleltest // Environment-variable cases must not run in parallel.
func TestAPIKeyResolverSuite(t *testing.T) {
	suite.Run(t, new(APIKeyResolverSuite))
}

func (s *APIKeyResolverSuite) SetupTest() {
	s.path = filepath.Join(s.T().TempDir(), "credentials.json")
}

// TestLiteralReturnsExactValue verifies that literal values have no command syntax.
func (s *APIKeyResolverSuite) TestLiteralReturnsExactValue() {
	resolver := NewAPIKeyResolver(s.path, APIKeySource{
		Kind:  APIKeySourceLiteral,
		Value: "!echo secret-value",
	})

	key, err := resolver.ResolveAPIKey(s.T().Context())

	s.Require().NoError(err)
	s.Equal("!echo secret-value", key)
}

// TestEnvironmentReadsOnlyNamedVariable verifies explicit environment lookup and fresh resolution.
func (s *APIKeyResolverSuite) TestEnvironmentReadsOnlyNamedVariable() {
	s.T().Setenv("NAMED_GLYPH_KEY", "first-value")
	s.T().Setenv("OPENAI_API_KEY", "hidden-fallback")
	resolver := NewAPIKeyResolver(s.path, APIKeySource{
		Kind:  APIKeySourceEnvironment,
		Value: "NAMED_GLYPH_KEY",
	})

	key, err := resolver.ResolveAPIKey(s.T().Context())
	s.Require().NoError(err)
	s.Equal("first-value", key)

	s.T().Setenv("NAMED_GLYPH_KEY", "second-value")
	key, err = resolver.ResolveAPIKey(s.T().Context())
	s.Require().NoError(err)
	s.Equal("second-value", key)
}

// TestOmittedSourceReturnsNoKey verifies that keyless providers remain usable.
func (s *APIKeyResolverSuite) TestOmittedSourceReturnsNoKey() {
	s.T().Setenv("OPENAI_API_KEY", "hidden-fallback")
	key, err := NewAPIKeyResolver(s.path, APIKeySource{}).ResolveAPIKey(s.T().Context())

	s.Require().NoError(err)
	s.Empty(key)
}

// TestCredentialReadsNamedAPIKeyPayload verifies strict provider-owned credential JSON.
func (s *APIKeyResolverSuite) TestCredentialReadsNamedAPIKeyPayload() {
	s.writeEntries(map[string]jsontext.Value{
		"selected": jsontext.Value(`{"type":"api_key","key":"file-secret"}`),
		"other":    jsontext.Value(`{"type":"api_key","key":"other-secret"}`),
	})
	resolver := NewAPIKeyResolver(s.path, APIKeySource{
		Kind:  APIKeySourceCredential,
		Value: "selected",
	})

	key, err := resolver.ResolveAPIKey(s.T().Context())

	s.Require().NoError(err)
	s.Equal("file-secret", key)
}

// TestFailuresReturnTypedSafeErrors verifies all unavailable source failures without secret leakage.
func (s *APIKeyResolverSuite) TestFailuresReturnTypedSafeErrors() {
	s.T().Setenv("EMPTY_GLYPH_KEY", "")
	testCases := map[string]struct {
		source  APIKeySource
		entries map[string]jsontext.Value
		secret  string
	}{
		"missing environment": {
			source: APIKeySource{
				Kind:  APIKeySourceEnvironment,
				Value: "MISSING_GLYPH_KEY",
			},
			entries: nil,
			secret:  "",
		},
		"empty environment": {
			source: APIKeySource{
				Kind:  APIKeySourceEnvironment,
				Value: "EMPTY_GLYPH_KEY",
			},
			entries: nil,
			secret:  "",
		},
		"missing credential": {
			source: APIKeySource{
				Kind:  APIKeySourceCredential,
				Value: "missing",
			},
			entries: map[string]jsontext.Value{},
			secret:  "",
		},
		"wrong credential type": {
			source: APIKeySource{
				Kind:  APIKeySourceCredential,
				Value: "selected",
			},
			entries: map[string]jsontext.Value{
				"selected": jsontext.Value(`{"type":"oauth","key":"file-secret"}`),
			},
			secret: "file-secret",
		},
		"malformed credential": {
			source: APIKeySource{
				Kind:  APIKeySourceCredential,
				Value: "selected",
			},
			entries: map[string]jsontext.Value{
				"selected": jsontext.Value(`"malformed"`),
			},
			secret: "",
		},
		"empty credential key": {
			source: APIKeySource{
				Kind:  APIKeySourceCredential,
				Value: "selected",
			},
			entries: map[string]jsontext.Value{
				"selected": jsontext.Value(`{"type":"api_key","key":""}`),
			},
			secret: "",
		},
		"credential unknown field": {
			source: APIKeySource{
				Kind:  APIKeySourceCredential,
				Value: "selected",
			},
			entries: map[string]jsontext.Value{
				"selected": jsontext.Value(`{"type":"api_key","key":"file-secret","extra":true}`),
			},
			secret: "file-secret",
		},
		"unknown source kind": {
			source: APIKeySource{
				Kind:  "command",
				Value: "echo file-secret",
			},
			secret:  "file-secret",
			entries: nil,
		},
	}
	for name, test := range testCases {
		s.Run(name, func() {
			if test.entries != nil {
				s.writeEntries(test.entries)
			}
			_, err := NewAPIKeyResolver(s.path, test.source).ResolveAPIKey(s.T().Context())
			var keyErr *APIKeyError
			s.Require().ErrorAs(err, &keyErr)
			if test.secret != "" {
				s.NotContains(err.Error(), test.secret)
			}
		})
	}
}

// TestCredentialFailuresRetainCauses verifies store and payload decoder errors keep credential context.
func (s *APIKeyResolverSuite) TestCredentialFailuresRetainCauses() {
	// Arrange one store load failure and two invalid payloads for the same named credential.
	testCases := map[string]struct {
		prepare func()
		cause   string
	}{
		"store load": {
			prepare: func() {
				s.Require().NoError(os.Mkdir(s.path, 0o700))
			},
			cause: "is not a regular file",
		},
		"payload decode": {
			prepare: func() {
				s.writeEntries(map[string]jsontext.Value{"selected": jsontext.Value(`"malformed"`)})
			},
			cause: "unmarshal JSON string",
		},
	}
	for name, test := range testCases {
		s.Run(name, func() {
			s.path = filepath.Join(s.T().TempDir(), "credentials.json")
			test.prepare()

			// Act by resolving the named credential through the public resolver.
			_, err := NewAPIKeyResolver(s.path, APIKeySource{
				Kind:  APIKeySourceCredential,
				Value: "selected",
			}).ResolveAPIKey(s.T().Context())

			// Assert source and name context accompany the original cause.
			s.Require().Error(err)
			s.Contains(err.Error(), `resolve credential API key "selected"`)
			s.Contains(err.Error(), test.cause)
		})
	}
}

// TestCanceledResolutionReturnsTypedSafeError verifies cancellation before credential I/O.
func (s *APIKeyResolverSuite) TestCanceledResolutionReturnsTypedSafeError() {
	ctx, cancel := context.WithCancel(s.T().Context())
	cancel()

	_, err := NewAPIKeyResolver(
		s.path, APIKeySource{
			Kind:  APIKeySourceCredential,
			Value: "selected",
		},
	).ResolveAPIKey(ctx)

	var keyErr *APIKeyError
	s.Require().ErrorAs(err, &keyErr)
	s.ErrorIs(err, context.Canceled)
}

func (s *APIKeyResolverSuite) writeEntries(entries map[string]jsontext.Value) {
	s.T().Helper()
	data, err := json.Marshal(credentialFixture{
		Version:   credentialStoreVersion,
		Providers: entries,
	})
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(s.path, data, 0o600))
}
