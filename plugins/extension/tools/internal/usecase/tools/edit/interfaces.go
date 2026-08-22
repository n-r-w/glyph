package edit

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=edit

// ProjectEditor reads and writes complete project files.
type ProjectEditor interface {
	ReadFile(ctx context.Context, path string) (string, error)
	WriteFile(ctx context.Context, path string, content string) error
}
