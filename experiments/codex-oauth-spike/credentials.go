package main

import (
	"bytes"
	"context"

	"encoding/base64"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"

	"net/http"
	"os"

	"path/filepath"
	"strings"

	"time"
)

// glyphCredentialsPath returns the already approved persistent Glyph credential path.
func glyphCredentialsPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home for Glyph credentials: %w", err)
	}
	return filepath.Join(homeDir, ".glyph", "credentials.json"), nil
}

// loadCredentials reads only the experiment's provider payload from the Glyph credential store.
func loadCredentials(path string) (oauthCredentials, bool, error) {
	store, found, err := readCredentialStore(path)
	if err != nil || !found {
		return oauthCredentials{}, found, err
	}
	payload, ok := store.Providers[credentialsProvider]
	if !ok {
		return oauthCredentials{}, false, nil
	}
	var credentials oauthCredentials
	if err := json.Unmarshal(payload, &credentials); err != nil {
		return oauthCredentials{}, false, fmt.Errorf("decode stored OpenAI Codex credentials: %w", err)
	}
	if credentials.RefreshToken == "" {
		return oauthCredentials{}, false, fmt.Errorf("stored OpenAI Codex credentials are missing a refresh token")
	}
	return credentials, true, nil
}

// readCredentialStore validates the credential file version and owner-only permissions.
func readCredentialStore(path string) (credentialStore, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return credentialStore{}, false, nil
	}
	if err != nil {
		return credentialStore{}, false, fmt.Errorf("stat Glyph credential store: %w", err)
	}
	if info.Mode().Perm() != 0o600 {
		return credentialStore{}, false, fmt.Errorf(
			"Glyph credential store has mode %04o, expected 0600",
			info.Mode().Perm(),
		)
	}
	if info.Size() > maxCredentialStore {
		return credentialStore{}, false, fmt.Errorf("Glyph credential store exceeds %d bytes", maxCredentialStore)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return credentialStore{}, false, fmt.Errorf("read Glyph credential store: %w", err)
	}
	var store credentialStore
	if err := json.Unmarshal(data, &store); err != nil {
		return credentialStore{}, false, fmt.Errorf("decode Glyph credential store: %w", err)
	}
	if store.Version != credentialsVersion {
		return credentialStore{}, false, fmt.Errorf(
			"Glyph credential store version is %d, expected %d",
			store.Version,
			credentialsVersion,
		)
	}
	if store.Providers == nil {
		store.Providers = make(map[string]jsontext.Value)
	}
	return store, true, nil
}

// persistCredentials atomically replaces the provider payload without discarding other providers.
func persistCredentials(path string, credentials oauthCredentials) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create Glyph credential directory: %w", err)
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		return fmt.Errorf("stat Glyph credential directory: %w", err)
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		return fmt.Errorf("Glyph credential directory has mode %04o, expected 0700", directoryInfo.Mode().Perm())
	}

	store, found, err := readCredentialStore(path)
	if err != nil {
		return err
	}
	if !found {
		store = credentialStore{
			Version:   credentialsVersion,
			Providers: make(map[string]jsontext.Value),
		}
	}
	payload, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("encode OpenAI Codex credentials: %w", err)
	}
	store.Providers[credentialsProvider] = payload
	data, err := json.Marshal(store)
	if err != nil {
		return fmt.Errorf("encode Glyph credential store: %w", err)
	}

	temporary, err := os.CreateTemp(directory, ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary Glyph credential file: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary Glyph credential file: %w", err)
	}
	defer func() {
		_ = os.Remove(temporaryPath)
	}()
	if err := os.WriteFile(temporaryPath, data, 0o600); err != nil {
		return fmt.Errorf("write temporary Glyph credential file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace Glyph credential store: %w", err)
	}
	return nil
}

// refreshCredentials forces the current official Codex JSON refresh request shape.
func refreshCredentials(parent context.Context, current oauthCredentials) (oauthCredentials, error) {
	payload, err := json.Marshal(map[string]string{
		"client_id":     clientID,
		"grant_type":    "refresh_token",
		"refresh_token": current.RefreshToken,
	})
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("encode OAuth refresh request: %w", err)
	}

	ctx, cancel := context.WithTimeout(parent, tokenTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, bytes.NewReader(payload))
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("create OAuth refresh request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")

	response, err := (&http.Client{Timeout: tokenTimeout}).Do(request)
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("send OAuth refresh request: %w", err)
	}
	defer func() {
		_ = response.Body.Close()
	}()
	body, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("read OAuth refresh response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return oauthCredentials{}, fmt.Errorf("OAuth refresh returned HTTP %d", response.StatusCode)
	}

	var refreshed tokenResponse
	if err := json.Unmarshal(body, &refreshed); err != nil {
		return oauthCredentials{}, fmt.Errorf("decode OAuth refresh response: %w", err)
	}
	if refreshed.AccessToken == "" || refreshed.ExpiresIn <= 0 {
		return oauthCredentials{}, fmt.Errorf("OAuth refresh response is missing required token fields")
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = current.RefreshToken
	}
	if refreshed.IDToken == "" {
		refreshed.IDToken = current.IDToken
	}

	return oauthCredentials{
		AccessToken:  refreshed.AccessToken,
		RefreshToken: refreshed.RefreshToken,
		IDToken:      refreshed.IDToken,
		ExpiresAt:    time.Now().Add(time.Duration(refreshed.ExpiresIn) * time.Second),
	}, nil
}

// accountIDFromAccessToken extracts routing metadata without treating the unsigned payload as authorization.
func accountIDFromAccessToken(accessToken string) (string, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("OAuth access token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode OAuth JWT payload: %w", err)
	}
	var claims struct {
		OpenAIAuth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("decode OAuth JWT claims: %w", err)
	}
	if claims.OpenAIAuth.AccountID == "" {
		return "", fmt.Errorf("OAuth JWT is missing chatgpt_account_id")
	}
	return claims.OpenAIAuth.AccountID, nil
}
