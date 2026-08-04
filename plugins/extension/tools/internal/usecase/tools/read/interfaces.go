package read

import "context"

// ProjectReader reads complete file content from the working project.
//
//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=read
type ProjectReader interface {
	ReadFile(ctx context.Context, path string) (string, error)
}
