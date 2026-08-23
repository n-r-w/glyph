package edit

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=edit

// ProjectEditor updates one complete project-file mutation.
type ProjectEditor interface {
	UpdateFile(context.Context, string, func([]byte) ([]byte, error)) error
}
