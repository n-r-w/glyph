package credentials

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/n-r-w/glyph/host/internal/infra/providers/openai/compatible"
	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
)

// APIKeySourceKind identifies one API-key source.
type APIKeySourceKind string

const (
	// APIKeySourceLiteral uses a configured literal value.
	APIKeySourceLiteral APIKeySourceKind = "literal"
	// APIKeySourceEnvironment reads one named environment variable.
	APIKeySourceEnvironment APIKeySourceKind = "environment"
	// APIKeySourceCredential reads one named credential entry.
	APIKeySourceCredential APIKeySourceKind = "credential"
)

// APIKeySource contains one validated API-key source.
type APIKeySource struct {
	// Kind identifies how the API key is stored.
	Kind APIKeySourceKind
	// Value contains source-specific lookup data.
	Value string
}

// APIKeyError reports an API-key resolution failure with source context.
type APIKeyError struct {
	// Source identifies the failed API key source.
	Source APIKeySourceKind
	// Name identifies the failed environment variable or credential entry.
	Name string
	// cause contains the retained resolution failure.
	cause error
}

// Error returns source metadata without resolved key material.
func (e *APIKeyError) Error() string {
	message := fmt.Sprintf("resolve %s API key", e.Source)
	if e.Name != "" {
		message = fmt.Sprintf("%s %q", message, e.Name)
	}
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", message, e.cause)
	}
	return message + ": unavailable"
}

// Unwrap exposes the retained resolution cause for classification.
func (e *APIKeyError) Unwrap() error {
	return e.cause
}

// APIKeyResolver resolves one immutable configured source.
type APIKeyResolver struct {
	// path is the shared credential file path.
	path string
	// source contains the validated API key source.
	source APIKeySource
}

var (
	_ compatible.APIKeyResolver   = (*APIKeyResolver)(nil)
	_ providers.CredentialChecker = (*APIKeyResolver)(nil)
)

// NewAPIKeyResolver creates a resolver for one configured source.
func NewAPIKeyResolver(path string, source APIKeySource) *APIKeyResolver {
	return &APIKeyResolver{path: path, source: source}
}

// ResolveAPIKey resolves the current key value without caching it.
func (r *APIKeyResolver) ResolveAPIKey(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", r.safeError(err)
	}
	switch r.source.Kind {
	case "":
		return "", nil
	case APIKeySourceLiteral:
		return r.source.Value, nil
	case APIKeySourceEnvironment:
		value, found := os.LookupEnv(r.source.Value)
		if !found || value == "" {
			return "", r.safeError(nil)
		}
		return value, nil
	case APIKeySourceCredential:
		return r.resolveCredential(ctx)
	default:
		return "", &APIKeyError{Source: r.source.Kind, Name: "", cause: nil}
	}
}

// CheckCredentials checks that the configured source resolves.
func (r *APIKeyResolver) CheckCredentials(ctx context.Context) error {
	_, err := r.ResolveAPIKey(ctx)
	return err
}

func (r *APIKeyResolver) resolveCredential(ctx context.Context) (string, error) {
	payload, found, err := New(r.path, r.source.Value).Load()
	if err != nil {
		return "", r.safeError(fmt.Errorf("load credential: %w", err))
	}
	if !found {
		return "", r.safeError(nil)
	}
	if err = ctx.Err(); err != nil {
		return "", r.safeError(err)
	}
	decoder := jsontext.NewDecoder(bytes.NewReader(payload))
	var credential struct {
		Type string `json:"type"`
		Key  string `json:"key"`
	}
	if err = json.UnmarshalDecode(decoder, &credential, json.RejectUnknownMembers(true)); err != nil {
		return "", r.safeError(fmt.Errorf("decode credential payload: %w", err))
	}
	var extra any
	if err = json.UnmarshalDecode(decoder, &extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", r.safeError(nil)
		}
		return "", r.safeError(fmt.Errorf("decode trailing credential payload: %w", err))
	}
	if credential.Type != "api_key" || credential.Key == "" {
		return "", r.safeError(nil)
	}
	return credential.Key, nil
}

func (r *APIKeyResolver) safeError(cause error) *APIKeyError {
	name := r.source.Value
	if r.source.Kind != APIKeySourceEnvironment && r.source.Kind != APIKeySourceCredential {
		name = ""
	}
	return &APIKeyError{Source: r.source.Kind, Name: name, cause: cause}
}
