package compatible

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=compatible

// APIKeyResolver resolves the current API key for one provider request.
type APIKeyResolver interface {
	ResolveAPIKey(ctx context.Context) (string, error)
}
