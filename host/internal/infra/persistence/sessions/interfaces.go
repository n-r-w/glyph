package sessions

import "os"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessions

// FileSystem opens one session file relative to the confined project directory.
type FileSystem interface {
	// OpenFile opens a project-relative file without allowing path escape.
	OpenFile(projectDirectory, name string, flags int, mode os.FileMode) (File, error)
}

// File exposes the ordered durability operations required by repository append.
type File interface {
	// WritePayload writes one buffered header-plus-entry payload.
	WritePayload([]byte) (int, error)
	// Chmod applies the exact mode to a newly created file descriptor.
	Chmod(os.FileMode) error
	// Sync commits written data before repository success.
	Sync() error
	// Close releases the file and its confined root.
	Close() error
}
