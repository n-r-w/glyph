package codex

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=codex
//go:generate go tool mockgen -destination=net_listener_mock.go -package=codex -mock_names=Listener=MockNetListener net Listener
//go:generate go tool mockgen -destination=http_roundtripper_mock.go -package=codex -mock_names=RoundTripper=MockHTTPRoundTripper net/http RoundTripper
//go:generate go tool mockgen -destination=io_readcloser_mock.go -package=codex -mock_names=ReadCloser=MockIOReadCloser io ReadCloser

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
