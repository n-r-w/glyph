package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	refreshWindow        = 5 * time.Minute
	maxTokenResponseBody = 1 << 20
)

// ErrSignInRequired identifies credentials that cannot authorize a Codex request.
var ErrSignInRequired = errors.New("OpenAI Codex sign-in required")

// tokenResponse contains the provider refresh fields used to rotate credentials.
type tokenResponse struct {
	// AccessToken contains the rotated access token.
	AccessToken string `json:"access_token"`
	// RefreshToken contains the rotated refresh token.
	RefreshToken string `json:"refresh_token"`
	// ExpiresIn contains the access token lifetime in seconds.
	ExpiresIn int64 `json:"expires_in"`
}

// resolveCredentials reloads provider state and refreshes it at the approved threshold.
func (s *Driver) resolveCredentials(ctx context.Context) (oauthCredentials, error) {
	payload, found, loadErr := s.credentials.Load()
	if loadErr != nil {
		return oauthCredentials{}, fmt.Errorf("load OpenAI Codex credentials: %w", loadErr)
	}
	if !found {
		return oauthCredentials{}, ErrSignInRequired
	}
	var credentials oauthCredentials
	decodeErr := json.Unmarshal(payload, &credentials)
	if decodeErr != nil {
		return oauthCredentials{}, fmt.Errorf("decode stored OpenAI Codex credentials: %w", decodeErr)
	}
	validationErr := validateCredentials(credentials)
	if validationErr != nil {
		return oauthCredentials{}, validationErr
	}
	if credentials.ExpiresAt.After(s.options.now().Add(refreshWindow)) {
		return credentials, nil
	}
	return s.refreshCredentials(ctx, credentials)
}

// validateCredentials rejects incomplete and account-inconsistent persisted provider data.
func validateCredentials(credentials oauthCredentials) error {
	if credentials.AccessToken == "" || credentials.RefreshToken == "" ||
		credentials.AccountID == "" || credentials.ExpiresAt.IsZero() {
		return ErrSignInRequired
	}
	accountID, err := accountIDFromAccessToken(credentials.AccessToken)
	if err != nil {
		return fmt.Errorf("validate stored OpenAI Codex account data: %w", err)
	}
	if accountID != credentials.AccountID {
		return errors.New("stored OpenAI Codex credentials have inconsistent account data")
	}
	return nil
}

// refreshCredentials performs the provider's JSON refresh request and persists rotation.
func (s *Driver) refreshCredentials(ctx context.Context, current oauthCredentials) (oauthCredentials, error) {
	requestBody, err := json.Marshal(map[string]string{
		"client_id":     codexClientID,
		"grant_type":    "refresh_token",
		"refresh_token": current.RefreshToken,
	})
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("encode OpenAI Codex refresh request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, s.options.tokenURL, bytes.NewReader(requestBody))
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("create OpenAI Codex refresh request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := s.options.httpClient.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return oauthCredentials{}, fmt.Errorf("refresh OpenAI Codex credentials: %w", ctx.Err())
		}
		return oauthCredentials{}, fmt.Errorf("refresh OpenAI Codex credentials: %w", err)
	}
	refreshed, err := decodeRefreshResponse(response)
	if err != nil {
		return oauthCredentials{}, err
	}
	rotated, err := s.rotateCredentials(current, refreshed)
	if err != nil {
		return oauthCredentials{}, err
	}
	//nolint:gosec // This method exists to persist the approved credential payload.
	payload, err := json.Marshal(rotated)
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("encode refreshed OpenAI Codex credentials: %w", err)
	}
	persistErr := s.credentials.Save(payload)
	if persistErr != nil {
		return oauthCredentials{}, fmt.Errorf("persist refreshed OpenAI Codex credentials: %w", persistErr)
	}
	return rotated, nil
}

// decodeRefreshResponse reads a bounded body, returns non-success details, and decodes successful token data.
func decodeRefreshResponse(response *http.Response) (tokenResponse, error) {
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxTokenResponseBody+1))
	closeErr := response.Body.Close()
	if readErr != nil {
		readFailure := fmt.Errorf("read OpenAI Codex credential refresh response: %w", readErr)
		if closeErr != nil {
			return tokenResponse{}, errors.Join(
				readFailure,
				fmt.Errorf("close OpenAI Codex credential refresh response: %w", closeErr),
			)
		}
		return tokenResponse{}, readFailure
	}
	if closeErr != nil {
		return tokenResponse{}, fmt.Errorf("close OpenAI Codex credential refresh response: %w", closeErr)
	}
	isFailure := response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices
	if len(body) > maxTokenResponseBody {
		return tokenResponse{}, ErrSignInRequired
	}
	if isFailure {
		detail := strings.TrimSpace(string(body))
		if detail == "" {
			return tokenResponse{}, fmt.Errorf("%w: token endpoint returned %s", ErrSignInRequired, response.Status)
		}
		return tokenResponse{}, fmt.Errorf("%w: token endpoint returned %s: %s", ErrSignInRequired, response.Status, detail)
	}
	var refreshed tokenResponse
	decodeErr := json.Unmarshal(body, &refreshed)
	if decodeErr != nil {
		return tokenResponse{}, fmt.Errorf("decode OpenAI Codex credential refresh response: %w", decodeErr)
	}
	return refreshed, nil
}

// rotateCredentials validates provider rotation and retains an omitted refresh token.
func (s *Driver) rotateCredentials(current oauthCredentials, refreshed tokenResponse) (oauthCredentials, error) {
	if refreshed.AccessToken == "" || refreshed.ExpiresIn <= 0 {
		return oauthCredentials{}, errors.New("OpenAI Codex credential refresh response is incomplete")
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = current.RefreshToken
	}
	accountID, err := accountIDFromAccessToken(refreshed.AccessToken)
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("validate refreshed OpenAI Codex account: %w", err)
	}
	if accountID != current.AccountID {
		return oauthCredentials{}, errors.New("OpenAI Codex credential refresh changed the account")
	}
	return oauthCredentials{
		AccessToken:  refreshed.AccessToken,
		RefreshToken: refreshed.RefreshToken,
		AccountID:    accountID,
		ExpiresAt:    s.options.now().Add(time.Duration(refreshed.ExpiresIn) * time.Second),
	}, nil
}
