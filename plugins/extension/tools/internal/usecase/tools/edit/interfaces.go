package edit

import "context"

// ProjectEditor reads and writes complete project files.
//
//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=edit
type ProjectEditor interface {
	ReadFile(ctx context.Context, path string) (string, error)
	WriteFile(ctx context.Context, path string, content string) error
}
