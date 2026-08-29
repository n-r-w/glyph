// Package codex implements ChatGPT Codex authentication and the Agent Core model-provider contract.
package codex

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/hooks"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
)

const (
	// ProviderID identifies Codex-owned opaque context and credential payloads.
	ProviderID            = "openai-codex"
	signInRequiredMessage = "OpenAI Codex sign-in required."
)

// Config contains provider-owned Codex configuration.
type Config struct {
	// Hooks applies configured provider request and response hooks.
	Hooks hooks.ProviderRunner
	// Models lists configured Codex models.
	Models []model.ID
	// ReasoningCompatibilityKeys contains replay contracts by model.
	ReasoningCompatibilityKeys map[model.ID]mo.Option[string]
}

// driverOptions contains provider-owned protocol endpoints and deterministic seams.
type driverOptions struct {
	// authorizationURL is the browser OAuth endpoint.
	authorizationURL string
	// tokenURL is the OAuth token endpoint.
	tokenURL string
	// modelBaseURL is the Codex Responses endpoint base URL.
	modelBaseURL string
	// httpClient sends OAuth and model requests.
	httpClient *http.Client
	// listen opens the OAuth loopback listener.
	listen func(network, address string) (net.Listener, error)
	// now returns the current time for credential validation.
	now func() time.Time
}

// modelConfig contains provider-owned wire metadata for one configured model.
type modelConfig struct {
	// api identifies the provider request contract.
	api string
	// reasoningCompatibilityKey identifies the replay contract.
	reasoningCompatibilityKey mo.Option[string]
}

// Driver owns Codex OAuth credentials and Responses translation.
type Driver struct {
	// hooks applies configured provider hooks.
	hooks hooks.ProviderRunner
	// models contains provider wire metadata by model.
	models map[model.ID]modelConfig
	// credentials loads and stores Codex OAuth credentials.
	credentials Credentials
	// interaction presents browser authorization URLs.
	interaction Interaction
	// options contains provider endpoints and deterministic seams.
	options driverOptions
}

var (
	_ run.ModelProvider                = (*Driver)(nil)
	_ providers.ProviderAuthentication = (*Driver)(nil)
)

// New creates the production ChatGPT Codex provider.
func New(config Config, credentials Credentials, interaction Interaction) *Driver {
	return newDriver(config, credentials, interaction, defaultDriverOptions())
}

// newDriver creates a provider with internal protocol seams used by package tests.
func newDriver(config Config, credentials Credentials, interaction Interaction, options driverOptions) *Driver {
	models := lo.SliceToMap(config.Models, func(modelID model.ID) (model.ID, modelConfig) {
		return modelID, modelConfig{
			api:                       "responses",
			reasoningCompatibilityKey: config.ReasoningCompatibilityKeys[modelID],
		}
	})
	return &Driver{
		hooks: config.Hooks, models: models,
		credentials: credentials, interaction: interaction, options: options,
	}
}

// CheckProviderAuthentication validates or refreshes persisted credentials without starting OAuth.
func (s *Driver) CheckProviderAuthentication(ctx context.Context) error {
	_, err := s.resolveCredentials(ctx)
	if err == nil {
		return nil
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return fmt.Errorf("check OpenAI Codex authentication: %w", contextErr)
	}
	if errors.Is(err, ErrSignInRequired) {
		return fmt.Errorf("check OpenAI Codex authentication: %w", err)
	}
	return fmt.Errorf("%w: %w", ErrSignInRequired, err)
}

// IsProviderSignInRequired reports the provider-owned authentication classification.
func (*Driver) IsProviderSignInRequired(err error) bool {
	return errors.Is(err, ErrSignInRequired)
}

// defaultDriverOptions returns the approved ChatGPT Codex endpoints and system dependencies.
func defaultDriverOptions() driverOptions {
	return driverOptions{ //nolint:gosec // These are approved public protocol endpoints, not credentials.
		authorizationURL: "https://auth.openai.com/oauth/authorize",
		tokenURL:         "https://auth.openai.com/oauth/token",
		modelBaseURL:     "https://chatgpt.com/backend-api/codex",
		httpClient: &http.Client{
			Transport: nil, CheckRedirect: nil, Jar: nil, Timeout: 0,
		},
		listen: net.Listen,
		now:    time.Now,
	}
}
