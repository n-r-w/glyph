package providers

import (
	"context"

	"github.com/n-r-w/glyph/host/internal/domain/authentication"
)

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=providers

// CredentialChecker checks whether request credentials are available.
// Implementations must return errors that contain no API keys or other secret values.
type CredentialChecker interface {
	// CheckCredentials checks whether request credentials can be resolved.
	CheckCredentials(ctx context.Context) error
}

// ProviderAuthentication exposes provider-owned credential checks and interactive sign-in to the catalog.
type ProviderAuthentication interface {
	CredentialChecker
	// SignIn runs interactive provider authentication.
	SignIn(ctx context.Context, method authentication.Method) error
	// IsSignInRequired reports whether an error requires interactive authentication.
	IsSignInRequired(err error) bool
}
