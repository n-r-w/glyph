package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"golang.org/x/oauth2"
)

// authenticate performs browser PKCE without consulting existing credential stores.
func authenticate(parent context.Context) (oauthCredentials, error) {
	state, err := randomState()
	if err != nil {
		return oauthCredentials{}, err
	}
	verifier := oauth2.GenerateVerifier()

	loopback, redirectURL, err := startLoopbackServer(state)
	if err != nil {
		return oauthCredentials{}, err
	}
	defer func() {
		_ = loopback.Close()
	}()

	config := oauth2.Config{
		ClientID:    clientID,
		RedirectURL: redirectURL,
		Scopes:      []string{"openid", "profile", "email", "offline_access"},
		Endpoint: oauth2.Endpoint{
			AuthURL:   authURL,
			TokenURL:  tokenURL,
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
	authorizationURL := config.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("id_token_add_organizations", "true"),
		oauth2.SetAuthURLParam("codex_cli_simplified_flow", "true"),
		oauth2.SetAuthURLParam("originator", "glyph"),
	)

	fmt.Printf("Open this URL on the same computer if the browser does not open automatically:\n%s\n", authorizationURL)
	if err := exec.CommandContext(parent, "open", authorizationURL).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: automatic browser launch failed; use the displayed URL: %v\n", err)
	}

	loginCtx, cancelLogin := context.WithTimeout(parent, loginTimeout)
	defer cancelLogin()
	callback, err := loopback.Wait(loginCtx)
	if err != nil {
		return oauthCredentials{}, err
	}
	if callback.Err != nil {
		return oauthCredentials{}, callback.Err
	}
	if err := loopback.Close(); err != nil {
		return oauthCredentials{}, fmt.Errorf("close OAuth callback server: %w", err)
	}

	exchangeCtx, cancelExchange := context.WithTimeout(parent, tokenTimeout)
	defer cancelExchange()
	exchangeClient := &http.Client{Timeout: tokenTimeout}
	exchangeCtx = context.WithValue(exchangeCtx, oauth2.HTTPClient, exchangeClient)
	token, err := config.Exchange(exchangeCtx, callback.Code, oauth2.VerifierOption(verifier))
	if err != nil {
		return oauthCredentials{}, fmt.Errorf("exchange OAuth authorization code: %w", err)
	}
	if token.AccessToken == "" || token.RefreshToken == "" || token.Expiry.IsZero() {
		return oauthCredentials{}, fmt.Errorf("OAuth exchange response is missing required token fields")
	}

	idToken, _ := token.Extra("id_token").(string)
	return oauthCredentials{
		AccessToken:  token.AccessToken,
		RefreshToken: token.RefreshToken,
		IDToken:      idToken,
		ExpiresAt:    token.Expiry,
	}, nil
}

// randomState creates an unpredictable OAuth state value for callback correlation.
func randomState() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// startLoopbackServer binds the first available registered OpenAI callback port.
func startLoopbackServer(state string) (*loopbackServer, string, error) {
	var listener net.Listener
	var lastErr error
	for _, port := range callbackPorts {
		listener, lastErr = net.Listen("tcp4", net.JoinHostPort("127.0.0.1", port))
		if lastErr == nil {
			redirectURL := "http://localhost:" + port + callbackPath
			server := newLoopbackServer(listener, state)
			return server, redirectURL, nil
		}
	}
	return nil, "", fmt.Errorf("bind registered OAuth callback ports 1455 and 1457: %w", lastErr)
}

// newLoopbackServer starts the callback HTTP server after its listener is reserved.
func newLoopbackServer(listener net.Listener, state string) *loopbackServer {
	results := make(chan authCallback, 1)
	mux := http.NewServeMux()
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	loopback := &loopbackServer{server: server, results: results}

	mux.HandleFunc(callbackPath, func(writer http.ResponseWriter, request *http.Request) {
		if subtle.ConstantTimeCompare([]byte(request.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(writer, "OAuth state mismatch.", http.StatusBadRequest)
			return
		}
		if providerError := request.URL.Query().Get("error"); providerError != "" {
			description := request.URL.Query().Get("error_description")
			result := authCallback{Err: fmt.Errorf("OpenAI authorization failed: %s: %s", providerError, description)}
			deliverCallback(results, result)
			http.Error(writer, "Authentication failed. Return to the terminal.", http.StatusBadRequest)
			return
		}
		code := request.URL.Query().Get("code")
		if code == "" {
			http.Error(writer, "Missing authorization code.", http.StatusBadRequest)
			return
		}
		deliverCallback(results, authCallback{Code: code})
		writer.Header().Set("Content-Type", "text/html; charset=utf-8")
		writer.Header().Set("Connection", "close")
		_, _ = io.WriteString(
			writer,
			"<!doctype html><html><body><p>Authentication completed. Return to Glyph.</p></body></html>",
		)
	})

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			deliverCallback(results, authCallback{Err: fmt.Errorf("serve OAuth callback: %w", err)})
		}
	}()
	return loopback
}

// deliverCallback publishes only the first terminal callback result.
func deliverCallback(results chan<- authCallback, result authCallback) {
	select {
	case results <- result:
	default:
	}
}

// Wait waits for a validated callback or cancellation.
func (server *loopbackServer) Wait(ctx context.Context) (authCallback, error) {
	select {
	case result := <-server.results:
		return result, nil
	case <-ctx.Done():
		return authCallback{}, fmt.Errorf("wait for OAuth callback: %w", ctx.Err())
	}
}

// Close stops the callback server exactly once.
func (server *loopbackServer) Close() error {
	server.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.server.Shutdown(ctx); err != nil {
			server.err = err
		}
	})
	return server.err
}
