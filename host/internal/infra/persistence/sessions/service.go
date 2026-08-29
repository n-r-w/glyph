// Package sessions stores session lifecycle records as JSONL files.
package sessions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"

	"errors"
	"fmt"
	"io"

	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/n-r-w/glyph/host/internal/domain/session"

	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

const (
	directoryMode  = 0o700
	fileMode       = 0o600
	readBufferSize = 32 * 1024
)

// Service stores sessions for one canonical project directory.
type Service struct {
	// root contains every project partition and receives private directory permissions.
	root string
	// workingDirectory is the canonical project path required in every loaded header.
	workingDirectory string
	// projectDirectory is the SHA-256 partition used for all path-confined file access.
	projectDirectory string
	// fileSystem performs ordered file-descriptor durability operations.
	fileSystem FileSystem
}

var _ hostsessions.Repository = (*Service)(nil)

// New creates a session repository with explicit filesystem operations.
func New(root, workingDirectory string, fileSystem FileSystem) *Service {
	digest := sha256.Sum256([]byte(workingDirectory))
	return &Service{
		root: root, workingDirectory: workingDirectory,
		projectDirectory: filepath.Join(root, hex.EncodeToString(digest[:])),
		fileSystem:       fileSystem,
	}
}

// CanonicalWorkingDirectory resolves an absolute, symlink-free project path.
func CanonicalWorkingDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("make working directory absolute: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve working directory: %w", err)
	}
	return filepath.Clean(resolved), nil
}

// Initialize creates the session root and project directory.
func (s *Service) Initialize(_ context.Context) error {
	if err := os.MkdirAll(s.root, directoryMode); err != nil {
		return fmt.Errorf("create session root: %w", err)
	}
	if err := os.Chmod(s.root, directoryMode); err != nil {
		return fmt.Errorf("set session root mode: %w", err)
	}
	if err := os.MkdirAll(s.projectDirectory, directoryMode); err != nil {
		return fmt.Errorf("create project session directory: %w", err)
	}
	if err := os.Chmod(s.projectDirectory, directoryMode); err != nil {
		return fmt.Errorf("set project session directory mode: %w", err)
	}
	return nil
}

// Append synchronously appends one entry.
func (s *Service) Append(_ context.Context, command hostsessions.AppendCommand) (hostsessions.AppendResult, error) {
	entry, err := encodeEntry(command.Entry)
	if err != nil {
		return hostsessions.AppendResult{}, err
	}
	path := command.StoragePath
	name := filepath.Base(path)
	flags := os.O_WRONLY | os.O_APPEND
	payload := entry
	created := path == ""
	if created {
		if command.Header.Version != 1 || command.Header.ID == "" || command.Header.WorkingDirectory != s.workingDirectory {
			return hostsessions.AppendResult{}, errors.New("invalid session header")
		}
		name = filename(command.Header)
		path = filepath.Join(s.projectDirectory, name)
		header, encodeErr := encodeLine(headerRecord{
			Type:      "session",
			Version:   command.Header.Version,
			ID:        string(command.Header.ID),
			CreatedAt: command.Header.CreatedAt.Format(time.RFC3339Nano),
			CWD:       command.Header.WorkingDirectory,
		})
		if encodeErr != nil {
			return hostsessions.AppendResult{}, fmt.Errorf("encode session header: %w", encodeErr)
		}
		payload = make([]byte, 0, len(header)+len(entry))
		payload = append(payload, header...)
		payload = append(payload, entry...)
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	} else if filepath.Clean(filepath.Dir(path)) != s.projectDirectory || !strings.HasSuffix(name, ".jsonl") {
		return hostsessions.AppendResult{}, errors.New("session storage path is outside the project directory")
	}

	file, err := s.fileSystem.OpenFile(s.projectDirectory, name, flags, fileMode)
	if err != nil {
		return hostsessions.AppendResult{}, fmt.Errorf("open session file: %w", err)
	}
	if err = persist(file, created, payload); err != nil {
		return hostsessions.AppendResult{}, fmt.Errorf("persist session entry: %w", err)
	}
	return hostsessions.AppendResult{StoragePath: path}, nil
}

// persist applies creation mode before one write, synchronization, and close.
func persist(file File, created bool, payload []byte) error {
	if created {
		if err := file.Chmod(fileMode); err != nil {
			return errors.Join(err, file.Close())
		}
	}
	written, writeErr := file.WritePayload(payload)
	if writeErr == nil && written != len(payload) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		return errors.Join(writeErr, file.Close())
	}
	syncErr := file.Sync()
	return errors.Join(syncErr, file.Close())
}

// filename keeps creation ordering visible while retaining the opaque session ID.
func filename(header session.Header) string {
	return header.CreatedAt.UTC().Format("20060102T150405.000000000Z") + "-" + string(header.ID) + ".jsonl"
}
