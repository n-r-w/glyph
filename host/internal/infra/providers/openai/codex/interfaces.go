package codex

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=codex

// Credentials provides Codex with only its opaque provider payload.
type Credentials interface {
	Load() ([]byte, bool, error)
	Save(payload []byte) error
	Delete() error
}

// Interaction presents browser authentication through Glyph Host.
type Interaction interface {
	PresentAuthorizationURL(ctx context.Context, authorizationURL string) error
	OpenBrowser(ctx context.Context, authorizationURL string) error
}
