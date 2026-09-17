package codex

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/samber/mo"
	"golang.org/x/oauth2"

	"github.com/n-r-w/glyph/host/internal/domain/authentication"
)

const (
	// deviceCodeEndpoint requests a one-time authorization code from OpenAI.
	deviceCodeEndpoint = "https://auth.openai.com/api/accounts/deviceauth/usercode"
	// devicePollingEndpoint polls the pending authorization on OpenAI.
	devicePollingEndpoint = "https://auth.openai.com/api/accounts/deviceauth/token"
	// deviceVerificationURL is the provider page opened on the browser computer.
	deviceVerificationURL = "https://auth.openai.com/codex/device"
	// deviceRedirectURL identifies the device-code OAuth exchange, not a local callback.
	deviceRedirectURL = "https://auth.openai.com/deviceauth/callback"
	// deviceContentTypeHeader names the HTTP request content type.
	deviceContentTypeHeader = "Content-Type"
	// deviceAuthorizationLifetime is the provider's documented device-code validity window.
	deviceAuthorizationLifetime = 15 * time.Minute
	// deviceJSONContentType identifies the provider's device-authorization request encoding.
	deviceJSONContentType = "application/json"
)

// deviceCodeRequest identifies the registered OAuth client requesting a user code.
type deviceCodeRequest struct {
	// ClientID identifies Glyph's Codex OAuth client.
	ClientID string `json:"client_id"`
}

// deviceCodeResponse contains the identifiers used to display and poll a pending authorization.
type deviceCodeResponse struct {
	// DeviceAuthID correlates server-side authorization polling.
	DeviceAuthID string `json:"device_auth_id"`
	// UserCode is the one-time value entered on the provider's verification page.
	UserCode string `json:"user_code"`
	// Interval is the provider's polling delay in seconds, encoded as a JSON string.
	Interval int64 `json:"interval,string"`
}

// devicePollRequest correlates one pending device authorization.
type devicePollRequest struct {
	// DeviceAuthID identifies the pending server-side authorization.
	DeviceAuthID string `json:"device_auth_id"`
	// UserCode identifies the code presented to the user.
	UserCode string `json:"user_code"`
}

// deviceAuthorizationResponse supplies the authorization code and its server-generated PKCE verifier.
type deviceAuthorizationResponse struct {
	// AuthorizationCode is exchanged for the normal persisted OAuth credentials.
	AuthorizationCode string `json:"authorization_code"`
	// CodeVerifier proves possession of the approved device authorization.
	CodeVerifier string `json:"code_verifier"`
}

// signInDeviceCode authorizes a remote Glyph process without opening a callback listener or browser.
func (s *Driver) signInDeviceCode(ctx context.Context) error {
	if s.interaction == nil {
		return ErrInteractionUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, deviceAuthorizationLifetime)
	defer cancel()
	status, body, err := s.deviceRequest(ctx, s.options.deviceCodeURL, deviceCodeRequest{ClientID: codexClientID})
	if err != nil {
		return fmt.Errorf("request OpenAI Codex device code: %w", err)
	}
	if status < http.StatusOK || status >= http.StatusMultipleChoices {
		return fmt.Errorf("request OpenAI Codex device code: HTTP %d: %s", status, body)
	}
	var code deviceCodeResponse
	if err = json.Unmarshal(body, &code); err != nil {
		return fmt.Errorf("decode OpenAI Codex device code: %w", err)
	}
	if code.DeviceAuthID == "" || code.UserCode == "" || code.Interval <= 0 {
		return errors.New(
			"OpenAI Codex device code response is missing an identifier, user code, or positive polling interval",
		)
	}
	if err = s.interaction.PresentAuthorization(ctx, authentication.Challenge{
		URL: s.options.deviceVerificationURL, UserCode: mo.Some(code.UserCode),
	}); err != nil {
		return fmt.Errorf("present OpenAI Codex device code: %w", err)
	}
	approved, err := s.pollDeviceAuthorization(ctx, code)
	if err != nil {
		return err
	}
	config := s.oauthConfig(s.options.deviceRedirectURL)
	exchangeContext := context.WithValue(ctx, oauth2.HTTPClient, s.options.httpClient)
	token, err := config.Exchange(
		exchangeContext,
		approved.AuthorizationCode,
		oauth2.VerifierOption(approved.CodeVerifier),
	)
	if err != nil {
		return fmt.Errorf("exchange OpenAI Codex device authorization code: %w", err)
	}
	return s.persistSignInToken(token)
}

// pollDeviceAuthorization waits only on the provider's pending statuses and keeps cancellation linked.
func (s *Driver) pollDeviceAuthorization(
	ctx context.Context, code deviceCodeResponse,
) (deviceAuthorizationResponse, error) {
	for {
		status, body, err := s.deviceRequest(ctx, s.options.deviceTokenURL, devicePollRequest{
			DeviceAuthID: code.DeviceAuthID, UserCode: code.UserCode,
		})
		if err != nil {
			return deviceAuthorizationResponse{}, fmt.Errorf("poll OpenAI Codex device authorization: %w", err)
		}
		switch {
		case status >= http.StatusOK && status < http.StatusMultipleChoices:
			var approved deviceAuthorizationResponse
			if err = json.Unmarshal(body, &approved); err != nil {
				return deviceAuthorizationResponse{}, fmt.Errorf("decode OpenAI Codex device authorization: %w", err)
			}
			if approved.AuthorizationCode == "" || approved.CodeVerifier == "" {
				return deviceAuthorizationResponse{}, errors.New(
					"OpenAI Codex device authorization response is incomplete",
				)
			}
			return approved, nil
		case status == http.StatusForbidden || status == http.StatusNotFound:
			// These statuses mean the user has not completed authorization, not a failed sign-in.
			timer := time.NewTimer(time.Duration(code.Interval) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return deviceAuthorizationResponse{}, fmt.Errorf(
					"wait for OpenAI Codex device authorization: %w",
					ctx.Err(),
				)
			case <-timer.C:
			}
		default:
			return deviceAuthorizationResponse{}, fmt.Errorf(
				"OpenAI Codex device authorization: HTTP %d: %s",
				status,
				body,
			)
		}
	}
}

// deviceRequest sends one provider JSON request and retains the complete response for diagnostics or decoding.
func (s *Driver) deviceRequest(ctx context.Context, endpoint string, payload any) (status int, body []byte, err error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, fmt.Errorf("encode device authorization request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return 0, nil, fmt.Errorf("create device authorization request: %w", err)
	}
	request.Header.Set(deviceContentTypeHeader, deviceJSONContentType)
	response, err := s.options.httpClient.Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("send device authorization request: %w", err)
	}
	body, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if err = errors.Join(readErr, closeErr); err != nil {
		return 0, nil, fmt.Errorf("read and close device authorization response: %w", err)
	}
	return response.StatusCode, body, nil
}
