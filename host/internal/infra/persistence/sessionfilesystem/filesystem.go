// Package sessionfilesystem implements confined filesystem operations for session persistence.
package sessionfilesystem

import (
	"errors"
	"os"

	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
)

// New creates filesystem operations backed by project-confined os.Root access.
func New() FileSystem { return FileSystem{} }

// FileSystem opens session files through project-confined roots.
type FileSystem struct{}

var _ sessionstore.FileSystem = FileSystem{}

// OpenFile opens a file through os.Root and transfers both handles to one close operation.
func (FileSystem) OpenFile(
	projectDirectory, name string,
	flags int,
	mode os.FileMode,
) (sessionstore.File, error) {
	root, err := os.OpenRoot(projectDirectory)
	if err != nil {
		return nil, err
	}
	file, err := root.OpenFile(name, flags, mode)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return &rootedFile{file: file, root: root}, nil
}

// rootedFile owns one open file and the root that confined its path.
type rootedFile struct {
	file *os.File
	root *os.Root
}

var _ sessionstore.File = (*rootedFile)(nil)

// ReadPayload reads stored bytes through the open descriptor.
func (file *rootedFile) ReadPayload(payload []byte) (int, error) { return file.file.Read(payload) }

// WritePayload writes one complete append payload.
func (file *rootedFile) WritePayload(payload []byte) (int, error) { return file.file.Write(payload) }

// Stat reports the opened file type and mode.
func (file *rootedFile) Stat() (os.FileInfo, error) { return file.file.Stat() }

// Truncate changes the file size through the open descriptor.
func (file *rootedFile) Truncate(size int64) error { return file.file.Truncate(size) }

// Chmod applies an exact mode through the open descriptor.
func (file *rootedFile) Chmod(mode os.FileMode) error { return file.file.Chmod(mode) }

// Sync commits file changes before repository success.
func (file *rootedFile) Sync() error { return file.file.Sync() }

// Close releases the file before its root and retains both failures.
func (file *rootedFile) Close() error {
	return errors.Join(file.file.Close(), file.root.Close())
}
