// Package codex implements ChatGPT Codex authentication and the Agent Core model-provider contract.
package codex

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/n-r-w/glyph/host/internal/hooks"
	"github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

const (
	// ProviderID identifies Codex-owned opaque context and credential payloads.
	ProviderID            = "openai-codex"
	signInRequiredMessage = "OpenAI Codex sign-in required."
)

// Config contains provider-owned Codex configuration.
type Config struct {
	Hooks hooks.ProviderRunner
}

// serviceOptions contains provider-owned protocol endpoints and deterministic seams.
type serviceOptions struct {
	authorizationURL string
	tokenURL         string
	modelBaseURL     string
	httpClient       *http.Client
	listen           func(network, address string) (net.Listener, error)
	now              func() time.Time
}

// Service owns Codex OAuth credentials and Responses translation.
type Service struct {
	config      Config
	credentials Credentials
	interaction Interaction
	options     serviceOptions
}

var (
	_ run.ModelProvider    = (*Service)(nil)
	_ hostui.Authenticator = (*Service)(nil)
)

// New creates the production ChatGPT Codex provider.
func New(config Config, credentials Credentials, interaction Interaction) *Service {
	return newService(config, credentials, interaction, defaultServiceOptions())
}

// newService creates a provider with internal protocol seams used by package tests.
func newService(config Config, credentials Credentials, interaction Interaction, options serviceOptions) *Service {
	return &Service{config: config, credentials: credentials, interaction: interaction, options: options}
}

// CheckAuthentication validates or refreshes persisted credentials without starting OAuth.
func (s *Service) CheckAuthentication(ctx context.Context) error {
	_, err := s.resolveCredentials(ctx)
	if err == nil {
		return nil
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return fmt.Errorf("check OpenAI Codex authentication: %w", contextErr)
	}
	if errors.Is(err, ErrSignInRequired) {
		return ErrSignInRequired
	}
	return fmt.Errorf("%w: %s", ErrSignInRequired, safeErrorMessage(err))
}

// IsSignInRequired reports the provider-owned authentication classification.
func (*Service) IsSignInRequired(err error) bool {
	return errors.Is(err, ErrSignInRequired)
}

// defaultServiceOptions returns the approved ChatGPT Codex endpoints and system dependencies.
func defaultServiceOptions() serviceOptions {
	return serviceOptions{ //nolint:gosec // These are approved public protocol endpoints, not credentials.
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
