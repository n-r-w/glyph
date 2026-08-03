// Command codex-oauth-spike verifies the prototype Codex integration against the live backend.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/oauth2"
)

const (
	modelID                = "gpt-5.6-luna"
	clientID               = "app_EMoamEEZ73f0CkXaXp7hrann"
	authURL                = "https://auth.openai.com/oauth/authorize"
	tokenURL               = "https://auth.openai.com/oauth/token"
	codexBaseURL           = "https://chatgpt.com/backend-api/codex"
	callbackPath           = "/auth/callback"
	firstPrompt            = "Derive the file path before using a tool: reverse elpmas, append a dot, then append the ASCII characters 116, 120, 116. Call the only available tool exactly once with the derived path. Do not answer before the tool result."
	sampleFileName         = "sample.txt"
	sampleFileValue        = "glyph-live-spike-ok"
	credentialsVersion     = 1
	credentialsProvider    = "openai-codex"
	readSchemaURL          = "urn:glyph:tool:read"
	loginTimeout           = 10 * time.Minute
	requestTimeout         = 3 * time.Minute
	tokenTimeout           = 30 * time.Second
	maxCredentialStore     = 1 << 20
	maxProviderErrorBody   = 64 << 10
	maxProviderErrorDetail = 4000
)

var callbackPorts = [...]string{"1455", "1457"} //nolint:gochecknoglobals // Registered OpenAI loopback ports are immutable protocol data.

const readSchemaJSON = `{
  "type": "object",
  "properties": {
    "path": {
      "type": "string",
      "description": "Path to the text file."
    }
  },
  "required": ["path"],
  "additionalProperties": false
}`

// oauthCredentials contains only the provider fields needed by this experiment.
type oauthCredentials struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// tokenResponse models the successful OpenAI token endpoint response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// authCallback is the validated terminal result of the loopback callback.
type authCallback struct {
	Code string
	Err  error
}

// loopbackServer owns one OAuth callback listener and its single terminal result.
type loopbackServer struct {
	server  *http.Server
	results chan authCallback
	once    sync.Once
	err     error
}

// readArguments is the typed input accepted by the spike's safe read tool.
type readArguments struct {
	Path string `json:"path"`
}

// streamedTurn contains only the provider data required to verify the next step.
type streamedTurn struct {
	Reasoning   *responses.ResponseReasoningItem
	ToolCall    *responses.ResponseFunctionToolCall
	Text        string
	OutputTypes []string
	EventCount  int
	TextDeltas  int
	IsCompleted bool
}

// credentialStore mirrors the approved versioned provider-keyed Glyph credential file.
type credentialStore struct {
	Version   int                        `json:"version"`
	Providers map[string]json.RawMessage `json:"providers"`
}

// codexClient keeps the SDK service and transport-only error diagnostics together.
type codexClient struct {
	responses responses.ResponseService
	errors    *errorCaptureTransport
}

// errorCaptureTransport retains only bounded bodies from failed provider responses.
type errorCaptureTransport struct {
	base http.RoundTripper
	mu   sync.Mutex
	body []byte
}

// main reports one actionable failure without exposing OAuth tokens.
func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

// run executes the approved live verification while isolating all sample file access.
func run() (runErr error) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workDir, err := os.MkdirTemp("", "glyph-codex-spike-run-")
	if err != nil {
		return fmt.Errorf("create isolated run directory: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			runErr = errors.Join(runErr, fmt.Errorf("remove isolated run directory: %w", err))
		}
	}()

	if err := os.WriteFile(filepath.Join(workDir, sampleFileName), []byte(sampleFileValue), 0o600); err != nil {
		return fmt.Errorf("write safe sample file: %w", err)
	}

	credentialsPath, err := glyphCredentialsPath()
	if err != nil {
		return err
	}
	credentials, found, err := loadCredentials(credentialsPath)
	if err != nil {
		return err
	}
	if !found {
		credentials, err = authenticate(ctx)
		if err != nil {
			return err
		}
		if err := persistCredentials(credentialsPath, credentials); err != nil {
			return err
		}
		fmt.Println("PASS: browser PKCE and persistent credential storage")
	} else {
		fmt.Println("PASS: persistent OAuth credential loading without browser login")
	}

	credentials, err = refreshCredentials(ctx, credentials)
	if err != nil {
		return fmt.Errorf("refresh saved OAuth credential; remove ~/.glyph/credentials.json to sign in again: %w", err)
	}
	if err := persistCredentials(credentialsPath, credentials); err != nil {
		return err
	}
	fmt.Println("PASS: forced JSON token refresh and persistent rotation")

	accountID, err := accountIDFromAccessToken(credentials.AccessToken)
	if err != nil {
		return err
	}

	readSchema, readSchemaMap, err := compileReadSchema()
	if err != nil {
		return err
	}
	fmt.Println("PASS: Draft 2020-12 strict read schema compilation")

	client := newCodexClient(credentials.AccessToken, accountID)
	tools := readTools(readSchemaMap)

	firstCtx, cancelFirst := context.WithTimeout(ctx, requestTimeout)
	first, err := streamTurn(firstCtx, client, firstRequest(tools))
	cancelFirst()
	if err != nil {
		return fmt.Errorf("first Responses turn: %w", err)
	}
	if first.ToolCall == nil || first.ToolCall.Name != "read" {
		return fmt.Errorf("first Responses turn did not produce the required read tool call")
	}
	if first.Reasoning == nil {
		return fmt.Errorf("first Responses turn did not include a reasoning item; output types: %v", first.OutputTypes)
	}
	if first.Reasoning.EncryptedContent == "" {
		return fmt.Errorf("first Responses reasoning item has no encrypted content; output types: %v", first.OutputTypes)
	}
	if !first.IsCompleted || first.EventCount == 0 {
		return fmt.Errorf("first Responses SSE stream did not complete")
	}
	fmt.Println("PASS: SSE tool call and encrypted reasoning capture")

	toolOutput, err := executeSafeRead(workDir, readSchema, first.ToolCall.Arguments)
	if err != nil {
		return fmt.Errorf("execute safe read: %w", err)
	}
	fmt.Println("PASS: JSON Schema argument validation and safe read execution")

	secondParams, err := secondRequest(tools, *first.Reasoning, *first.ToolCall, toolOutput)
	if err != nil {
		return err
	}
	secondCtx, cancelSecond := context.WithTimeout(ctx, requestTimeout)
	second, err := streamTurn(secondCtx, client, secondParams)
	cancelSecond()
	if err != nil {
		return fmt.Errorf("second Responses turn: %w", err)
	}
	if !second.IsCompleted || second.TextDeltas == 0 {
		return fmt.Errorf("second Responses SSE stream did not produce streamed text")
	}
	if !strings.Contains(second.Text, sampleFileValue) {
		return fmt.Errorf("final response did not contain the safe tool marker")
	}
	fmt.Println("PASS: tool-result continuation and encrypted reasoning replay")
	fmt.Println("PASS: Codex OAuth, refresh, SSE, strict tool call, and encrypted reasoning replay")

	return nil
}

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
		_, _ = io.WriteString(writer, "<!doctype html><html><body><p>Authentication completed. Return to Glyph.</p></body></html>")
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
		return credentialStore{}, false, fmt.Errorf("Glyph credential store has mode %04o, expected 0600", info.Mode().Perm())
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
		return credentialStore{}, false, fmt.Errorf("Glyph credential store version is %d, expected %d", store.Version, credentialsVersion)
	}
	if store.Providers == nil {
		store.Providers = make(map[string]json.RawMessage)
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
			Providers: make(map[string]json.RawMessage),
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

// compileReadSchema verifies the selected validator and returns both provider and runtime forms.
func compileReadSchema() (*jsonschema.Schema, map[string]any, error) {
	var schemaMap map[string]any
	if err := json.Unmarshal([]byte(readSchemaJSON), &schemaMap); err != nil {
		return nil, nil, fmt.Errorf("decode read schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource(readSchemaURL, schemaMap); err != nil {
		return nil, nil, fmt.Errorf("add read schema resource: %w", err)
	}
	compiled, err := compiler.Compile(readSchemaURL)
	if err != nil {
		return nil, nil, fmt.Errorf("compile read schema: %w", err)
	}
	return compiled, schemaMap, nil
}

// newCodexClient configures openai-go only as the Codex SSE transport.
func newCodexClient(accessToken, accountID string) codexClient {
	errorTransport := &errorCaptureTransport{base: http.DefaultTransport}
	httpClient := &http.Client{Transport: errorTransport}
	return codexClient{
		responses: responses.NewResponseService(
			option.WithBaseURL(codexBaseURL),
			option.WithAPIKey(accessToken),
			option.WithHeader("chatgpt-account-id", accountID),
			option.WithHeader("OpenAI-Beta", "responses=experimental"),
			option.WithHeader("originator", "glyph"),
			option.WithHeader("User-Agent", "glyph-codex-spike/1"),
			option.WithMaxRetries(0),
			option.WithHTTPClient(httpClient),
		),
		errors: errorTransport,
	}
}

// readTools constructs the exact strict function tool advertised in both turns.
func readTools(schema map[string]any) []responses.ToolUnionParam {
	return []responses.ToolUnionParam{{
		OfFunction: &responses.FunctionToolParam{
			Name:        "read",
			Description: param.NewOpt("Read the complete contents of the generated sample text file."),
			Parameters:  schema,
			Strict:      param.NewOpt(true),
		},
	}}
}

// baseRequest applies the stateless Codex policy shared by both model turns.
func baseRequest(tools []responses.ToolUnionParam) responses.ResponseNewParams {
	return responses.ResponseNewParams{
		Model:             shared.ResponsesModel(modelID),
		Instructions:      param.NewOpt("You are validating a safe Glyph tool-calling integration. Follow the requested tool flow exactly."),
		Store:             param.NewOpt(false),
		ParallelToolCalls: param.NewOpt(false),
		Include: []responses.ResponseIncludable{
			responses.ResponseIncludableReasoningEncryptedContent,
		},
		Reasoning: shared.ReasoningParam{
			Effort:  shared.ReasoningEffortHigh,
			Summary: shared.ReasoningSummaryAuto,
		},
		Tools: tools,
	}
}

// firstRequest forces one read call so the experiment deterministically exercises tool streaming.
func firstRequest(tools []responses.ToolUnionParam) responses.ResponseNewParams {
	request := baseRequest(tools)
	request.Input = responses.ResponseNewParamsInputUnion{
		OfInputItemList: responses.ResponseInputParam{{
			OfMessage: &responses.EasyInputMessageParam{
				Role: responses.EasyInputMessageRoleUser,
				Content: responses.EasyInputMessageContentUnionParam{
					OfString: param.NewOpt(firstPrompt),
				},
			},
		}},
	}
	request.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
		OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsRequired),
	}
	return request
}

// secondRequest replays the encrypted reasoning item and returns the tool result statelessly.
func secondRequest(
	tools []responses.ToolUnionParam,
	reasoning responses.ResponseReasoningItem,
	toolCall responses.ResponseFunctionToolCall,
	toolOutput string,
) (responses.ResponseNewParams, error) {
	if reasoning.ID == "" || reasoning.EncryptedContent == "" {
		return responses.ResponseNewParams{}, fmt.Errorf("reasoning replay data is incomplete")
	}

	summaries := make([]responses.ResponseReasoningItemSummaryParam, len(reasoning.Summary))
	for index, summary := range reasoning.Summary {
		summaries[index] = responses.ResponseReasoningItemSummaryParam{Text: summary.Text}
	}

	input := responses.ResponseInputParam{
		{
			OfMessage: &responses.EasyInputMessageParam{
				Role: responses.EasyInputMessageRoleUser,
				Content: responses.EasyInputMessageContentUnionParam{
					OfString: param.NewOpt(firstPrompt),
				},
			},
		},
		{
			OfReasoning: &responses.ResponseReasoningItemParam{
				ID:               reasoning.ID,
				Summary:          summaries,
				EncryptedContent: param.NewOpt(reasoning.EncryptedContent),
			},
		},
		{
			OfFunctionCall: &responses.ResponseFunctionToolCallParam{
				Arguments: toolCall.Arguments,
				CallID:    toolCall.CallID,
				Name:      toolCall.Name,
			},
		},
		{
			OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
				CallID: toolCall.CallID,
				Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
					OfString: param.NewOpt(toolOutput),
				},
			},
		},
	}

	request := baseRequest(tools)
	request.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: input}
	request.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
		OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsNone),
	}
	request.Instructions = param.NewOpt("Report the exact tool output marker in the final answer. Do not call another tool.")
	return request, nil
}

// streamTurn consumes Responses SSE events and captures terminal replay data.
func streamTurn(
	ctx context.Context,
	client codexClient,
	request responses.ResponseNewParams,
) (streamedTurn, error) {
	stream := client.responses.NewStreaming(ctx, request)
	defer stream.Close()

	var result streamedTurn
	var text strings.Builder
	for stream.Next() {
		event := stream.Current()
		result.EventCount++
		switch event.Type {
		case "response.output_text.delta":
			delta := event.AsResponseOutputTextDelta().Delta
			text.WriteString(delta)
			result.TextDeltas++
		case "response.output_item.done":
			captureOutputItem(&result, event.AsResponseOutputItemDone().Item)
		case "response.completed":
			completed := event.AsResponseCompleted().Response
			for _, item := range completed.Output {
				captureOutputItem(&result, item)
			}
			result.IsCompleted = true
		case "response.failed":
			failed := event.AsResponseFailed().Response
			return streamedTurn{}, fmt.Errorf("Codex response failed with status %s", failed.Status)
		case "error":
			providerEvent := event.AsError()
			return streamedTurn{}, fmt.Errorf("Codex stream error %s: %s", providerEvent.Code, providerEvent.Message)
		}
	}
	if err := stream.Err(); err != nil {
		return streamedTurn{}, normalizeProviderError(err, client.errors.ErrorBody())
	}
	result.Text = text.String()
	return result, nil
}

// captureOutputItem retains one reasoning item and one function call from completed events.
func captureOutputItem(result *streamedTurn, item responses.ResponseOutputItemUnion) {
	result.OutputTypes = append(result.OutputTypes, item.Type)
	switch item.Type {
	case "reasoning":
		reasoning := item.AsReasoning()
		if result.Reasoning == nil || reasoning.EncryptedContent != "" {
			result.Reasoning = &reasoning
		}
	case "function_call":
		toolCall := item.AsFunctionCall()
		result.ToolCall = &toolCall
	}
}

// executeSafeRead validates model JSON before reading the single generated sample file.
func executeSafeRead(workDir string, schema *jsonschema.Schema, rawArguments string) (string, error) {
	var instance any
	if err := json.Unmarshal([]byte(rawArguments), &instance); err != nil {
		return "", fmt.Errorf("decode arguments for schema validation: %w", err)
	}
	if err := schema.Validate(instance); err != nil {
		return "", fmt.Errorf("validate arguments against read schema: %w", err)
	}

	decoder := json.NewDecoder(strings.NewReader(rawArguments))
	decoder.DisallowUnknownFields()
	var arguments readArguments
	if err := decoder.Decode(&arguments); err != nil {
		return "", fmt.Errorf("decode typed read arguments: %w", err)
	}
	if arguments.Path != sampleFileName {
		return "", fmt.Errorf("read path %q is outside the safe spike contract", arguments.Path)
	}
	content, err := os.ReadFile(filepath.Join(workDir, sampleFileName))
	if err != nil {
		return "", fmt.Errorf("read generated sample file: %w", err)
	}
	return string(content), nil
}

// RoundTrip captures a bounded failed response body and restores it for the SDK.
func (transport *errorCaptureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.body = nil
	transport.mu.Unlock()

	response, err := transport.base.RoundTrip(request)
	if err != nil || response.StatusCode < http.StatusBadRequest {
		return response, err
	}
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxProviderErrorBody))
	closeErr := response.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("capture failed provider response: %w", readErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close failed provider response: %w", closeErr)
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	transport.mu.Lock()
	transport.body = append([]byte(nil), body...)
	transport.mu.Unlock()
	return response, nil
}

// ErrorBody returns a defensive copy of the last failed provider response.
func (transport *errorCaptureTransport) ErrorBody() []byte {
	transport.mu.Lock()
	defer transport.mu.Unlock()
	return append([]byte(nil), transport.body...)
}

// normalizeProviderError surfaces the backend detail shape without exposing request credentials.
func normalizeProviderError(err error, capturedBody []byte) error {
	var apiError *openai.Error
	if !errors.As(err, &apiError) {
		return err
	}
	body := []byte(apiError.RawJSON())
	if len(body) == 0 {
		body = capturedBody
	}
	detail := providerErrorDetail(body)
	if detail == "" {
		detail = apiError.Message
	}
	if len(detail) > maxProviderErrorDetail {
		detail = detail[:maxProviderErrorDetail]
	}
	return fmt.Errorf("Codex API HTTP %d: %s", apiError.StatusCode, detail)
}

// providerErrorDetail extracts common provider error shapes and preserves unknown bounded JSON.
func providerErrorDetail(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return strings.TrimSpace(string(body))
	}
	if detail, ok := payload["detail"].(string); ok {
		return detail
	}
	if providerError, ok := payload["error"].(map[string]any); ok {
		if message, ok := providerError["message"].(string); ok {
			return message
		}
	}
	return strings.TrimSpace(string(body))
}
