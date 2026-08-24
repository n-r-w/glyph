package providers

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=providers

// SelectionCredentialValidator checks credentials before a model selection commits.
// Implementations must return errors that contain no API keys or other secret values.
type SelectionCredentialValidator interface {
	ValidateSelectionCredentials(ctx context.Context) error
}

// ProviderAuthentication exposes provider-owned interactive authentication to the catalog.
type ProviderAuthentication interface {
	CheckProviderAuthentication(ctx context.Context) error
	SignInProvider(ctx context.Context) error
	IsProviderSignInRequired(err error) bool
}
