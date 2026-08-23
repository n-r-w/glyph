package write

import "context"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=write

// ProjectWriter replaces one project file.
type ProjectWriter interface {
	WriteFile(context.Context, string, string) error
}
