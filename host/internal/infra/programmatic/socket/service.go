// Package socket creates local listeners for programmatic control.
package socket

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

const (
	socketName                = "control.sock"
	socketMode    os.FileMode = 0o600
	directoryMode             = 0o700
)

// Service owns a Unix socket listener and its Glyph-created paths.
type Service struct {
	net.Listener
	path               string
	automaticDirectory string
}

var _ net.Listener = (*Service)(nil)

// New creates a Unix socket listener at an automatic or explicit path.
func New(ctx context.Context, path string) (*Service, error) {
	resolvedPath, automaticDirectory, err := preparePath(path)
	if err != nil {
		return nil, err
	}

	var listenerConfig net.ListenConfig
	listener, err := listenerConfig.Listen(ctx, "unix", resolvedPath)
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("listen on Unix socket %q: %w", resolvedPath, err),
			removeAutomaticDirectory(automaticDirectory),
		)
	}
	if unixListener, ok := listener.(*net.UnixListener); ok {
		unixListener.SetUnlinkOnClose(false)
	}
	service := &Service{
		Listener:           listener,
		path:               resolvedPath,
		automaticDirectory: automaticDirectory,
	}
	if chmodErr := os.Chmod(resolvedPath, socketMode); chmodErr != nil {
		return nil, errors.Join(
			fmt.Errorf("set Unix socket mode: %w", chmodErr),
			service.Close(),
		)
	}
	return service, nil
}

// Path returns the absolute socket path.
func (service *Service) Path() string {
	return service.path
}

// Close closes the listener and removes paths owned by the service.
func (service *Service) Close() error {
	closeErr := service.Listener.Close()
	removeSocketErr := removeIfExists(service.path)
	removeDirectoryErr := removeAutomaticDirectory(service.automaticDirectory)
	return errors.Join(closeErr, removeSocketErr, removeDirectoryErr)
}

// preparePath validates caller ownership and creates an automatic directory when needed.
func preparePath(path string) (resolvedPath, automaticDirectory string, err error) {
	if path == "" {
		directory, mkdirErr := os.MkdirTemp("", "glyph-rpc-")
		if mkdirErr != nil {
			return "", "", fmt.Errorf("create automatic socket directory: %w", mkdirErr)
		}
		// The socket directory must permit access only to its owner.
		if chmodErr := os.Chmod(directory, directoryMode); chmodErr != nil {
			return "", "", errors.Join(
				fmt.Errorf("set automatic socket directory mode: %w", chmodErr),
				removeAutomaticDirectory(directory),
			)
		}
		return filepath.Join(directory, socketName), directory, nil
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve Unix socket path: %w", err)
	}
	parent := filepath.Dir(absolutePath)
	parentInfo, err := os.Stat(parent)
	if err != nil {
		return "", "", fmt.Errorf("inspect socket parent %q: %w", parent, err)
	}
	if !parentInfo.IsDir() {
		return "", "", fmt.Errorf("socket parent %q is not a directory", parent)
	}
	if _, lstatErr := os.Lstat(absolutePath); lstatErr == nil {
		return "", "", fmt.Errorf("unix socket path %q already exists", absolutePath)
	} else if !errors.Is(lstatErr, os.ErrNotExist) {
		return "", "", fmt.Errorf("inspect Unix socket path %q: %w", absolutePath, lstatErr)
	}
	return absolutePath, "", nil
}

// removeAutomaticDirectory removes only a directory created by this package.
func removeAutomaticDirectory(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove automatic socket directory %q: %w", path, err)
	}
	return nil
}

// removeIfExists removes one owned path without following it.
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove Unix socket %q: %w", path, err)
	}
	return nil
}
