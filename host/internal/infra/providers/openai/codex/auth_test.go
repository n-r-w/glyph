package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

// TestServiceSignInValidatesStateExchangesAndPersists verifies the complete browser PKCE success path.
func TestServiceSignInValidatesStateExchangesAndPersists(t *testing.T) {
	t.Parallel()

	accountID := "account-123"
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": accountID},
	})
	var exchangedCode atomic.Value
	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !assert.NoError(t, request.ParseForm()) {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		exchangedCode.Store(request.Form.Get("code"))
		assert.Equal(t, "authorization_code", request.Form.Get("grant_type"))
		assert.NotEmpty(t, request.Form.Get("code_verifier"))
		assert.Equal(t, codexClientID, request.Form.Get("client_id"))
		writer.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(writer, `{"access_token":%q,"refresh_token":"refresh-1","expires_in":3600,"token_type":"Bearer"}`, accessToken)
	}))
	t.Cleanup(tokenServer.Close)

	credentials := NewMockCredentials(gomock.NewController(t))
	interaction := NewMockInteraction(gomock.NewController(t))
	credentials.EXPECT().Save(gomock.Any()).DoAndReturn(func(payload []byte) error {
		var stored oauthCredentials
		require.NoError(t, json.Unmarshal(payload, &stored))
		assert.Equal(t, accessToken, stored.AccessToken)
		assert.Equal(t, "refresh-1", stored.RefreshToken)
		assert.Equal(t, accountID, stored.AccountID)
		assert.False(t, stored.ExpiresAt.IsZero())
		return nil
	})
	var attemptedAddresses []string
	options := defaultServiceOptions()
	options.authorizationURL = tokenServer.URL + "/authorize"
	options.tokenURL = tokenServer.URL
	options.httpClient = tokenServer.Client()
	options.listen = func(network, address string) (net.Listener, error) {
		attemptedAddresses = append(attemptedAddresses, address)
		if strings.HasSuffix(address, ":1455") {
			return nil, fmt.Errorf("first callback port unavailable")
		}
		var listenConfig net.ListenConfig
		return listenConfig.Listen(t.Context(), network, "127.0.0.1:0")
	}
	gomock.InOrder(
		interaction.EXPECT().PresentAuthorizationURL(gomock.Any(), gomock.Any()).DoAndReturn(
			func(ctx context.Context, authorizationURL string) error {
				parsed, err := url.Parse(authorizationURL)
				require.NoError(t, err)
				query := parsed.Query()
				assert.Equal(t, "S256", query.Get("code_challenge_method"))
				assert.NotEmpty(t, query.Get("code_challenge"))
				assert.Equal(t, "glyph", query.Get("originator"))
				redirectURL := query.Get("redirect_uri")
				assert.True(t, strings.HasPrefix(redirectURL, "http://localhost:"))

				mismatchRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, redirectURL+"?code=ignored&state=wrong", nil)
				require.NoError(t, err)
				mismatchResponse, err := options.httpClient.Do(mismatchRequest)
				require.NoError(t, err)
				require.NoError(t, mismatchResponse.Body.Close())
				assert.Equal(t, http.StatusBadRequest, mismatchResponse.StatusCode)

				callbackRequest, err := http.NewRequestWithContext(
					ctx,
					http.MethodGet,
					redirectURL+"?code=approved-code&state="+url.QueryEscape(query.Get("state")),
					nil,
				)
				require.NoError(t, err)
				callbackResponse, err := options.httpClient.Do(callbackRequest)
				require.NoError(t, err)
				require.NoError(t, callbackResponse.Body.Close())
				assert.Equal(t, http.StatusOK, callbackResponse.StatusCode)
				return nil
			},
		),
		interaction.EXPECT().OpenBrowser(gomock.Any(), gomock.Any()).Return(errors.New("browser unavailable")),
	)
	service := newService(Config{Model: testModelDescriptor("model"), ThinkingLevel: "", Hooks: testProviderHookRunner()}, credentials, interaction, options)

	err := service.SignIn(t.Context())

	require.NoError(t, err)
	assert.Equal(t, "approved-code", exchangedCode.Load())
	assert.Equal(t, []string{"127.0.0.1:1455", "127.0.0.1:1457"}, attemptedAddresses)
}

// TestServiceSignInCancellationClosesCallbackServer verifies cancellation stops both waiting and listening.
func TestServiceSignInCancellationClosesCallbackServer(t *testing.T) {
	t.Parallel()

	credentials := NewMockCredentials(gomock.NewController(t))
	interaction := NewMockInteraction(gomock.NewController(t))
	interaction.EXPECT().PresentAuthorizationURL(gomock.Any(), gomock.Any()).Return(nil)
	interaction.EXPECT().OpenBrowser(gomock.Any(), gomock.Any()).Return(nil)
	var callbackListener *net.TCPListener
	options := defaultServiceOptions()
	options.listen = func(network, _ string) (net.Listener, error) {
		listener, err := net.ListenTCP(
			"tcp4",
			&net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0, Zone: ""},
		)
		if err == nil {
			callbackListener = listener
		}
		return listener, err
	}
	service := newService(
		Config{Model: ModelDescriptor(""), ThinkingLevel: "", Hooks: testProviderHookRunner()}, credentials, interaction, options,
	)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	err := service.SignIn(ctx)

	require.ErrorIs(t, err, context.Canceled)
	require.NotNil(t, callbackListener)
	replacement, listenErr := net.ListenTCP("tcp4", callbackListener.Addr().(*net.TCPAddr))
	require.NoError(t, listenErr)
	require.NoError(t, replacement.Close())
}

// TestServiceSignInRejectsIncompleteToken verifies invalid provider token data is never persisted.
func TestServiceSignInRejectsIncompleteToken(t *testing.T) {
	t.Parallel()

	tokenServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"access_token":"missing-refresh","expires_in":3600,"token_type":"Bearer"}`))
	}))
	t.Cleanup(tokenServer.Close)
	credentials := NewMockCredentials(gomock.NewController(t))
	interaction := NewMockInteraction(gomock.NewController(t))
	options := defaultServiceOptions()
	options.authorizationURL = tokenServer.URL + "/authorize"
	options.tokenURL = tokenServer.URL
	options.httpClient = tokenServer.Client()
	options.listen = func(network, _ string) (net.Listener, error) {
		var listenConfig net.ListenConfig
		return listenConfig.Listen(t.Context(), network, "127.0.0.1:0")
	}
	interaction.EXPECT().PresentAuthorizationURL(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, authorizationURL string) error {
			parsed, err := url.Parse(authorizationURL)
			require.NoError(t, err)
			callbackURL := parsed.Query().Get("redirect_uri") + "?code=code&state=" + url.QueryEscape(parsed.Query().Get("state"))
			request, err := http.NewRequestWithContext(ctx, http.MethodGet, callbackURL, nil)
			require.NoError(t, err)
			response, err := options.httpClient.Do(request)
			require.NoError(t, err)
			return response.Body.Close()
		},
	)
	interaction.EXPECT().OpenBrowser(gomock.Any(), gomock.Any()).Return(nil)
	service := newService(
		Config{Model: ModelDescriptor(""), ThinkingLevel: "", Hooks: testProviderHookRunner()}, credentials, interaction, options,
	)

	err := service.SignIn(t.Context())

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "missing-refresh")
}

// TestServiceSignOutDeletesOnlyProviderPayload verifies sign-out delegates one opaque delete.
func TestServiceSignOutDeletesOnlyProviderPayload(t *testing.T) {
	t.Parallel()

	credentials := NewMockCredentials(gomock.NewController(t))
	interaction := NewMockInteraction(gomock.NewController(t))
	credentials.EXPECT().Delete().Return(nil)
	service := New(Config{Model: ModelDescriptor(""), ThinkingLevel: "", Hooks: testProviderHookRunner()}, credentials, interaction)

	require.NoError(t, service.SignOut())
}

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
