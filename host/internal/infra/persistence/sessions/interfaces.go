package sessions

import "os"

//go:generate go tool mockgen -source=interfaces.go -destination=interfaces_mock.go -package=sessions

// FileSystem opens one session file relative to the confined project directory.
type FileSystem interface {
	// OpenFile opens a project-relative file without allowing path escape.
	OpenFile(projectDirectory, name string, flags int, mode os.FileMode) (File, error)
}

// File exposes the ordered operations required by repository persistence.
type File interface {
	// ReadPayload reads stored bytes through the confined file descriptor.
	ReadPayload([]byte) (int, error)
	// WritePayload writes one buffered header-plus-entry payload.
	WritePayload([]byte) (int, error)
	// Stat reports the opened file type and mode.
	Stat() (os.FileInfo, error)
	// Truncate removes an interrupted append at a validated byte offset.
	Truncate(int64) error
	// Chmod applies the exact session file mode.
	Chmod(os.FileMode) error
	// Sync commits file changes before repository success.
	Sync() error
	// Close releases the file and its confined root.
	Close() error
}
