// Command codex-oauth-spike verifies the prototype Codex integration against the live backend.
package main

import (
	"context"

	"encoding/json/jsontext"
	"errors"
	"fmt"

	"net/http"
	"os"

	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/openai/openai-go/v3/responses"
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
	Version   int                       `json:"version"`
	Providers map[string]jsontext.Value `json:"providers"`
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
