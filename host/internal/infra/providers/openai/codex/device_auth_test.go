//go:build !integration

package codex

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/authentication"
)

// TestDeviceCodeSignInPollsAndPersists checks the complete code flow without a callback listener.
func TestDeviceCodeSignInPollsAndPersists(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange provider replies, two pending statuses, and a successful token exchange.
		controller := gomock.NewController(t)
		transport := NewMockHTTPRoundTripper(controller)
		interaction := NewMockInteraction(controller)
		credentials := NewMockCredentials(controller)
		options := defaultDriverOptions()
		options.httpClient = &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0}
		accessToken := testJWT(t, map[string]any{
			"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "account-device"},
		})
		responses := []struct {
			// endpoint is the exact provider endpoint for this step.
			endpoint string
			// status is the protocol result for this step.
			status int
			// body contains the provider JSON response.
			body string
		}{
			{
				endpoint: options.deviceCodeURL, status: http.StatusOK,
				body: `{"device_auth_id":"device-1","user_code":"ABCD-EFGH","interval":"2"}`,
			},
			{endpoint: options.deviceTokenURL, status: http.StatusForbidden, body: `{"detail":"waiting"}`},
			{endpoint: options.deviceTokenURL, status: http.StatusNotFound, body: `{"detail":"pending"}`},
			{
				endpoint: options.deviceTokenURL, status: http.StatusOK,
				body: `{"authorization_code":"approved","code_verifier":"verifier","code_challenge":"challenge"}`,
			},
			{
				endpoint: options.tokenURL,
				status:   http.StatusOK,
				body: fmt.Sprintf(
					`{"access_token":%q,"refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`,
					accessToken,
				),
			},
		}
		step := 0
		transport.EXPECT().RoundTrip(gomock.Any()).Times(len(responses)).DoAndReturn(
			func(request *http.Request) (*http.Response, error) {
				expected := responses[step]
				step++
				assert.Equal(t, http.MethodPost, request.Method)
				assert.Equal(t, expected.endpoint, request.URL.String())
				if expected.endpoint == options.tokenURL {
					require.NoError(t, request.ParseForm())
					assert.Equal(t, "approved", request.Form.Get("code"))
					assert.Equal(t, "verifier", request.Form.Get("code_verifier"))
					assert.Equal(t, options.deviceRedirectURL, request.Form.Get("redirect_uri"))
					assert.Equal(t, codexClientID, request.Form.Get("client_id"))
				} else {
					body, err := io.ReadAll(request.Body)
					require.NoError(t, err)
					if expected.endpoint == options.deviceCodeURL {
						assert.JSONEq(t, fmt.Sprintf(`{"client_id":%q}`, codexClientID), string(body))
					} else {
						assert.JSONEq(t, `{"device_auth_id":"device-1","user_code":"ABCD-EFGH"}`, string(body))
					}
				}
				return deviceTestResponse(request, expected.status, expected.body), nil
			},
		)
		interaction.EXPECT().PresentAuthorization(gomock.Any(), authentication.Challenge{
			URL: options.deviceVerificationURL, UserCode: mo.Some("ABCD-EFGH"),
		}).Return(nil)
		credentials.EXPECT().Save(gomock.Any()).DoAndReturn(func(payload []byte) error {
			var stored oauthCredentials
			require.NoError(t, json.Unmarshal(payload, &stored))
			assert.Equal(t, accessToken, stored.AccessToken)
			assert.Equal(t, "refresh", stored.RefreshToken)
			assert.Equal(t, "account-device", stored.AccountID)
			assert.True(t, stored.ExpiresAt.After(time.Now()))
			return nil
		})
		driver := newDriver(testConfig(), credentials, interaction, options)
		started := time.Now()
		// Act through the new code flow; network and interaction remain generated mocks.
		err := driver.SignIn(t.Context(), authentication.MethodDeviceCode)
		// Assert pending replies respect the provider's interval before persisting valid credentials.
		require.NoError(t, err)
		assert.Equal(t, 4*time.Second, time.Since(started))
	})
}

// TestDeviceCodeSignInPreservesProviderFailure checks that an HTTP failure retains its body and status.
func TestDeviceCodeSignInPreservesProviderFailure(t *testing.T) {
	t.Parallel()
	// Arrange an account that cannot request a device code.
	controller := gomock.NewController(t)
	transport := NewMockHTTPRoundTripper(controller)
	options := defaultDriverOptions()
	options.httpClient = &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0}
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		return deviceTestResponse(
			request,
			http.StatusBadRequest,
			`{"detail":"device sign-in disabled for this account"}`,
		), nil
	})
	driver := newDriver(testConfig(), NewMockCredentials(controller), NewMockInteraction(controller), options)
	// Act by requesting device authorization.
	err := driver.signInDeviceCode(t.Context())
	// Assert the original diagnostic is available to every caller.
	require.ErrorContains(t, err, "400")
	assert.ErrorContains(t, err, "device sign-in disabled for this account")
}

// TestDeviceCodeSignInCancelsPendingWait checks cancellation without waiting for the next poll.
func TestDeviceCodeSignInCancelsPendingWait(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange a pending response and cancel immediately after the first poll.
		controller := gomock.NewController(t)
		transport := NewMockHTTPRoundTripper(controller)
		interaction := NewMockInteraction(controller)
		options := defaultDriverOptions()
		options.httpClient = &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		gomock.InOrder(
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
				return deviceTestResponse(request, http.StatusOK,
					`{"device_auth_id":"device-1","user_code":"CODE","interval":"30"}`), nil
			}),
			transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
				cancel()
				return deviceTestResponse(request, http.StatusForbidden, `{"detail":"waiting"}`), nil
			}),
		)
		interaction.EXPECT().PresentAuthorization(gomock.Any(), gomock.Any()).Return(nil)
		driver := newDriver(testConfig(), NewMockCredentials(controller), interaction, options)
		started := time.Now()
		// Act while authorization is pending in the browser.
		err := driver.signInDeviceCode(ctx)
		// Assert cancellation is preserved and no fake time passes waiting for another poll.
		require.ErrorIs(t, err, context.Canceled)
		assert.Zero(t, time.Since(started))
	})
}

// TestDeviceCodeSignInPreservesPresentationFailure checks that a disconnected client stops authorization.
func TestDeviceCodeSignInPreservesPresentationFailure(t *testing.T) {
	t.Parallel()
	// Arrange a valid code and a failed UI delivery.
	controller := gomock.NewController(t)
	transport := NewMockHTTPRoundTripper(controller)
	interaction := NewMockInteraction(controller)
	options := defaultDriverOptions()
	options.httpClient = &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0}
	transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
		return deviceTestResponse(request, http.StatusOK,
			`{"device_auth_id":"device-1","user_code":"CODE","interval":"5"}`), nil
	})
	source := errors.New("UI connection lost")
	interaction.EXPECT().PresentAuthorization(gomock.Any(), gomock.Any()).Return(source)
	driver := newDriver(testConfig(), NewMockCredentials(controller), interaction, options)
	// Act by attempting to display the code.
	err := driver.signInDeviceCode(t.Context())
	// Assert no polling or credential writes occur after the presentation failure.
	require.ErrorIs(t, err, source)
}

// TestDeviceCodeSignInExpires checks the provider's validity window without real sleeping.
func TestDeviceCodeSignInExpires(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		// Arrange an authorization that stays pending for its entire lifetime.
		controller := gomock.NewController(t)
		transport := NewMockHTTPRoundTripper(controller)
		interaction := NewMockInteraction(controller)
		options := defaultDriverOptions()
		options.httpClient = &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0}
		transport.EXPECT().RoundTrip(gomock.Any()).AnyTimes().DoAndReturn(
			func(request *http.Request) (*http.Response, error) {
				if request.URL.String() == options.deviceCodeURL {
					return deviceTestResponse(request, http.StatusOK,
						`{"device_auth_id":"pending","user_code":"CODE","interval":"60"}`), nil
				}
				return deviceTestResponse(request, http.StatusForbidden, `{"detail":"pending"}`), nil
			},
		)
		interaction.EXPECT().PresentAuthorization(gomock.Any(), gomock.Any()).Return(nil)
		driver := newDriver(testConfig(), NewMockCredentials(controller), interaction, options)
		started := time.Now()
		// Act with a fake clock while the user never completes sign-in.
		err := driver.SignIn(t.Context(), authentication.MethodDeviceCode)
		// Assert a bounded attempt terminates and retains the deadline error.
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, 15*time.Minute, time.Since(started))
	})
}

// TestDeviceCodeSignInRejectsIncompleteApproval checks that provider omissions cannot reach token exchange.
func TestDeviceCodeSignInRejectsIncompleteApproval(t *testing.T) {
	t.Parallel()
	// Arrange a valid displayed code followed by a success response without a PKCE verifier.
	controller := gomock.NewController(t)
	transport := NewMockHTTPRoundTripper(controller)
	interaction := NewMockInteraction(controller)
	options := defaultDriverOptions()
	options.httpClient = &http.Client{Transport: transport, CheckRedirect: nil, Jar: nil, Timeout: 0}
	gomock.InOrder(
		transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
			return deviceTestResponse(request, http.StatusOK,
				`{"device_auth_id":"pending","user_code":"CODE","interval":"5"}`), nil
		}),
		transport.EXPECT().RoundTrip(gomock.Any()).DoAndReturn(func(request *http.Request) (*http.Response, error) {
			return deviceTestResponse(request, http.StatusOK, `{"authorization_code":"approved"}`), nil
		}),
	)
	interaction.EXPECT().PresentAuthorization(gomock.Any(), gomock.Any()).Return(nil)
	driver := newDriver(testConfig(), NewMockCredentials(controller), interaction, options)
	// Act by decoding the incomplete provider approval.
	err := driver.SignIn(t.Context(), authentication.MethodDeviceCode)
	// Assert the missing verifier is exposed rather than exchanging or storing incomplete data.
	require.ErrorContains(t, err, "device authorization response is incomplete")
}

// deviceTestResponse constructs a real response body for generated transport expectations.
func deviceTestResponse(request *http.Request, status int, body string) *http.Response {
	return &http.Response{
		Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), StatusCode: status,
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)), TransferEncoding: nil, Close: false, Uncompressed: false,
		Trailer: nil, Request: request, TLS: nil,
	}
}
