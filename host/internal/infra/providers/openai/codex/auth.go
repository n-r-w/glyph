package codex

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

const (
	codexClientID         = "app_EMoamEEZ73f0CkXaXp7hrann"
	callbackPath          = "/auth/callback"
	firstCallbackPort     = "1455"
	secondCallbackPort    = "1457"
	oauthStateBytes       = 32
	callbackShutdownLimit = 5 * time.Second
	callbackHeaderTimeout = 5 * time.Second
	jwtSegmentCount       = 3
)

// oauthCredentials is the provider-owned payload persisted through Host opaque storage.
type oauthCredentials struct {
	// AccessToken authorizes Codex requests.
	AccessToken string `json:"access_token"`
	// RefreshToken renews expired access tokens.
	RefreshToken string `json:"refresh_token"`
	// AccountID identifies the authenticated Codex account.
	AccountID string `json:"account_id"`
	// ExpiresAt is the access token expiration time.
	ExpiresAt time.Time `json:"expires_at"`
}

// callbackResult is the first terminal result accepted by the loopback server.
type callbackResult struct {
	// code contains the OAuth authorization code.
	code string
	// err contains a terminal callback failure.
	err error
}

// loopbackServer owns one callback listener and its terminal result.
type loopbackServer struct {
	// server handles the OAuth callback.
	server *http.Server
	// listener owns the selected loopback port.
	listener net.Listener
	// results publishes the first terminal callback result.
	results chan callbackResult
	// once limits shutdown to one attempt.
	once sync.Once
	// err retains the shutdown result.
	err error
}

// SignInProvider performs browser OAuth and persists the resulting provider payload.
func (s *Driver) SignInProvider(ctx context.Context) error {
	state, err := newOAuthState()
	if err != nil {
		return err
	}
	verifier := oauth2.GenerateVerifier()
	loopback, redirectURL, err := s.startLoopbackServer(state)
	if err != nil {
		return err
	}
	defer func() {
		_ = loopback.Close(ctx)
	}()

	config := s.oauthConfig(redirectURL)
	authorizationURL := config.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("id_token_add_organizations", "true"),
		oauth2.SetAuthURLParam("codex_cli_simplified_flow", "true"),
		oauth2.SetAuthURLParam("originator", "glyph"),
	)
	presentationErr := s.interaction.PresentAuthorizationURL(ctx, authorizationURL)
	if presentationErr != nil {
		return fmt.Errorf("present OpenAI Codex authorization URL: %w", presentationErr)
	}
	// Browser launch cannot replace URL presentation and therefore never blocks sign-in.
	_ = s.interaction.OpenBrowser(ctx, authorizationURL)

	callback, err := loopback.Wait(ctx)
	if err != nil {
		return err
	}
	if callback.err != nil {
		return callback.err
	}
	callbackCloseErr := loopback.Close(ctx)
	if callbackCloseErr != nil {
		return callbackCloseErr
	}

	exchangeContext := context.WithValue(ctx, oauth2.HTTPClient, s.options.httpClient)
	token, err := config.Exchange(exchangeContext, callback.code, oauth2.VerifierOption(verifier))
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("exchange OpenAI Codex authorization code: %w", ctx.Err())
		}
		return fmt.Errorf("exchange OpenAI Codex authorization code: %w", err)
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.Expiry.IsZero() {
		return errors.New("OpenAI Codex authorization response is missing required credentials")
	}
	accountID, err := accountIDFromAccessToken(token.AccessToken)
	if err != nil {
		return err
	}
	credentials := oauthCredentials{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		AccountID:    accountID,
		ExpiresAt:    token.Expiry,
	}
	payload, err := json.Marshal(credentials)
	if err != nil {
		return fmt.Errorf("encode OpenAI Codex credentials: %w", err)
	}
	persistErr := s.credentials.Save(payload)
	if persistErr != nil {
		return fmt.Errorf("persist OpenAI Codex credentials: %w", persistErr)
	}
	return nil
}

// SignOut deletes only the OpenAI Codex provider payload.
func (s *Driver) SignOut() error {
	if err := s.credentials.Delete(); err != nil {
		return fmt.Errorf("delete OpenAI Codex credentials: %w", err)
	}
	return nil
}

// oauthConfig builds the PKCE authorization-code client for one active callback listener.
func (s *Driver) oauthConfig(redirectURL string) oauth2.Config {
	return oauth2.Config{
		ClientID:    codexClientID,
		RedirectURL: redirectURL,
		Scopes:      []string{"openid", "profile", "email", "offline_access"},
		Endpoint: oauth2.Endpoint{
			AuthURL:       s.options.authorizationURL,
			TokenURL:      s.options.tokenURL,
			AuthStyle:     oauth2.AuthStyleInParams,
			DeviceAuthURL: "",
		},
		ClientSecret: "",
	}
}

// newOAuthState creates an unpredictable callback correlation value.
func newOAuthState() (string, error) {
	data := make([]byte, oauthStateBytes)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// startLoopbackServer tries the two registered Codex callback ports in order.
func (s *Driver) startLoopbackServer(state string) (*loopbackServer, string, error) {
	var listener net.Listener
	var listenErr error
	for _, port := range []string{firstCallbackPort, secondCallbackPort} {
		listener, listenErr = s.options.listen("tcp4", net.JoinHostPort("127.0.0.1", port))
		if listenErr == nil {
			_, actualPort, splitErr := net.SplitHostPort(listener.Addr().String())
			if splitErr != nil {
				_ = listener.Close()
				return nil, "", fmt.Errorf("resolve OAuth callback port: %w", splitErr)
			}
			redirectURL := "http://localhost:" + actualPort + callbackPath
			return newLoopbackServer(listener, state), redirectURL, nil
		}
	}
	return nil, "", fmt.Errorf("bind OpenAI Codex callback ports 1455 and 1457: %w", listenErr)
}

// newLoopbackServer starts the short-lived callback server on a reserved listener.
func newLoopbackServer(listener net.Listener, state string) *loopbackServer {
	results := make(chan callbackResult, 1)
	mux := http.NewServeMux()
	server := &http.Server{
		Handler:                      mux,
		ReadHeaderTimeout:            callbackHeaderTimeout,
		Addr:                         "",
		DisableGeneralOptionsHandler: false,
		TLSConfig:                    nil,
		ReadTimeout:                  0,
		WriteTimeout:                 0,
		IdleTimeout:                  0,
		MaxHeaderBytes:               0,
		MaxHeaderValueCount:          0,
		TLSNextProto:                 nil,
		ConnState:                    nil,
		ErrorLog:                     nil,
		BaseContext:                  nil,
		ConnContext:                  nil,
		HTTP2:                        nil,
		Protocols:                    nil,
		DisableClientPriority:        false,
	}
	loopback := &loopbackServer{
		server:   server,
		listener: listener,
		results:  results,
		once:     sync.Once{},
		err:      nil,
	}
	mux.HandleFunc(callbackPath, func(writer http.ResponseWriter, request *http.Request) {
		if subtle.ConstantTimeCompare([]byte(request.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(writer, "OAuth state mismatch.", http.StatusBadRequest)
			return
		}
		if oauthError := request.URL.Query().Get("error"); oauthError != "" {
			detail := request.URL.Query().Get("error_description")
			failure := fmt.Errorf("OpenAI authorization failed: %s", oauthError)
			if detail != "" {
				failure = fmt.Errorf("OpenAI authorization failed: %s: %s", oauthError, detail)
			}
			deliverCallback(results, callbackResult{
				code: "",
				err:  failure,
			})
			http.Error(writer, "Authentication failed. Return to Glyph.", http.StatusBadRequest)
			return
		}
		code := request.URL.Query().Get("code")
		if code == "" {
			http.Error(writer, "Missing authorization code.", http.StatusBadRequest)
			return
		}
		deliverCallback(results, callbackResult{
			code: code,
			err:  nil,
		})
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("Connection", "close")
		_, _ = io.WriteString(
			writer,
			"<!doctype html><html><body><p>Authentication completed. Return to Glyph.</p></body></html>",
		)
	})
	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			deliverCallback(results, callbackResult{
				code: "",
				err:  fmt.Errorf("OpenAI authorization callback server failed: %w", err),
			})
		}
	}()
	return loopback
}

// deliverCallback publishes only the first terminal callback result.
func deliverCallback(results chan<- callbackResult, result callbackResult) {
	select {
	case results <- result:
	default:
	}
}

// Wait blocks until a validated callback or caller cancellation.
func (s *loopbackServer) Wait(ctx context.Context) (callbackResult, error) {
	select {
	case result := <-s.results:
		return result, nil
	case <-ctx.Done():
		return callbackResult{}, fmt.Errorf("wait for OpenAI authorization callback: %w", ctx.Err())
	}
}

// Close stops the callback server once while retaining caller context values.
func (s *loopbackServer) Close(ctx context.Context) error {
	s.once.Do(func() {
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), callbackShutdownLimit)
		defer cancel()
		shutdownErr := s.server.Shutdown(shutdownContext)
		if errors.Is(shutdownErr, http.ErrServerClosed) {
			shutdownErr = nil
		}
		closeErr := s.listener.Close()
		if errors.Is(closeErr, net.ErrClosed) {
			closeErr = nil
		}
		if err := errors.Join(shutdownErr, closeErr); err != nil {
			s.err = fmt.Errorf("stop OpenAI authorization callback server: %w", err)
		}
	})
	return s.err
}

// accountIDFromAccessToken extracts routing data without treating unsigned claims as authorization.
func accountIDFromAccessToken(accessToken string) (string, error) {
	parts := strings.Split(accessToken, ".")
	if len(parts) != jwtSegmentCount {
		return "", errors.New("OpenAI Codex access token has an invalid format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode OpenAI Codex access token claims: %w", err)
	}
	var claims struct {
		OpenAIAuth struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
	}
	claimsErr := json.Unmarshal(payload, &claims)
	if claimsErr != nil {
		return "", fmt.Errorf("decode OpenAI Codex access token claims JSON: %w", claimsErr)
	}
	if claims.OpenAIAuth.AccountID == "" {
		return "", errors.New("OpenAI Codex access token is missing an account ID")
	}
	return claims.OpenAIAuth.AccountID, nil
}
