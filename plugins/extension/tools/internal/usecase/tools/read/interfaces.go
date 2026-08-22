package read

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=read

// ProjectReader reads complete file content from the working project.
type ProjectReader interface {
	ReadFile(ctx context.Context, path string) (string, error)
}
