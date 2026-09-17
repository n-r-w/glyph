//go:build integration

package codex

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/authentication"
)

// TestDeviceCodeSignInAgainstHTTPProvider checks HTTP polling, OAuth exchange, and reusable saved credentials.
func TestDeviceCodeSignInAgainstHTTPProvider(t *testing.T) {
	t.Parallel()
	// Arrange real HTTP endpoints that approve the displayed code and return an account token.
	accessToken := testJWT(t, map[string]any{
		"https://api.openai.com/auth": map[string]any{"chatgpt_account_id": "device-account"},
	})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/usercode":
			_, _ = writer.Write([]byte(`{"device_auth_id":"device-id","user_code":"ABCD-EFGH","interval":"5"}`))
		case "/poll":
			_, _ = writer.Write([]byte(`{"authorization_code":"approved","code_verifier":"verifier"}`))
		case "/token":
			_, _ = fmt.Fprintf(writer,
				`{"access_token":%q,"refresh_token":"refresh","expires_in":3600,"token_type":"Bearer"}`, accessToken)
		default:
			http.Error(writer, "unexpected authentication endpoint", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	controller := gomock.NewController(t)
	credentials := NewMockCredentials(controller)
	interaction := NewMockInteraction(controller)
	options := defaultDriverOptions()
	options.deviceCodeURL = server.URL + "/usercode"
	options.deviceTokenURL = server.URL + "/poll"
	options.tokenURL = server.URL + "/token"
	options.httpClient = server.Client()
	interaction.EXPECT().PresentAuthorization(gomock.Any(), authentication.Challenge{
		URL: options.deviceVerificationURL, UserCode: mo.Some("ABCD-EFGH"),
	}).Return(nil)
	var persisted []byte
	credentials.EXPECT().Save(gomock.Any()).DoAndReturn(func(payload []byte) error {
		persisted = payload
		return nil
	})
	credentials.EXPECT().Load().DoAndReturn(func() ([]byte, bool, error) { return persisted, true, nil })
	driver := newDriver(testConfig(), credentials, interaction, options)
	// Act through the complete HTTP sign-in flow and then the ordinary credential check.
	require.NoError(t, driver.SignIn(t.Context(), authentication.MethodDeviceCode))
	err := driver.CheckCredentials(t.Context())
	// Assert the resulting account credentials are ready for normal provider requests.
	require.NoError(t, err)
	require.NotEmpty(t, persisted)
}
