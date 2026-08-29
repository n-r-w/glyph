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
	"sort"
	"strings"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"

	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

const (
	directoryMode  = 0o700
	fileMode       = 0o600
	readBufferSize = 32 * 1024
	formatVersion  = 2
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

// Apply synchronously appends one complete tree mutation.
func (s *Service) Apply(_ context.Context, command hostsessions.ApplyCommand) (hostsessions.ApplyResult, error) {
	mutation, err := encodeMutation(command.Mutation)
	if err != nil {
		return hostsessions.ApplyResult{}, err
	}
	path, err := s.persistPayload(command.Header, command.StoragePath, mutation)
	if err != nil {
		return hostsessions.ApplyResult{}, err
	}
	return hostsessions.ApplyResult{StoragePath: path}, nil
}

// CreateSnapshot synchronously writes one complete replacement session.
func (s *Service) CreateSnapshot(
	_ context.Context,
	command hostsessions.CreateSnapshotCommand,
) (hostsessions.CreateSnapshotResult, error) {
	if command.Header.Version != formatVersion || command.Header.ID == "" ||
		command.Header.WorkingDirectory != s.workingDirectory {
		return hostsessions.CreateSnapshotResult{}, errors.New("invalid session header")
	}
	header, err := encodeHeader(command.Header)
	if err != nil {
		return hostsessions.CreateSnapshotResult{}, err
	}
	payload := append([]byte(nil), header...)
	entries := command.Tree.Entries()
	for index := range entries {
		line, encodeErr := encodeMutation(hostsessions.Mutation{
			Entry: mo.Some(entries[index]), Navigation: mo.None[hostsessions.NavigationMutation](),
			Label: mo.None[hostsessions.LabelMutation](), SessionInformation: mo.None[hostsessions.SessionInformationMutation](),
		})
		if encodeErr != nil {
			return hostsessions.CreateSnapshotResult{}, encodeErr
		}
		payload = append(payload, line...)
	}
	labelIDs := make([]string, 0, len(command.Tree.Labels()))
	for id := range command.Tree.Labels() {
		labelIDs = append(labelIDs, id)
	}
	sort.Strings(labelIDs)
	labels := command.Tree.Labels()
	for _, id := range labelIDs {
		line, encodeErr := encodeMutation(hostsessions.Mutation{
			Entry: mo.None[session.Entry](), Navigation: mo.None[hostsessions.NavigationMutation](),
			Label:              mo.Some(hostsessions.LabelMutation{TargetID: id, Label: labels[id]}),
			SessionInformation: mo.None[hostsessions.SessionInformationMutation](),
		})
		if encodeErr != nil {
			return hostsessions.CreateSnapshotResult{}, encodeErr
		}
		payload = append(payload, line...)
	}
	informationLine, err := encodeSnapshotInformation(command.Information, command.InformationUpdatedAt)
	if err != nil {
		return hostsessions.CreateSnapshotResult{}, err
	}
	payload = append(payload, informationLine...)
	line, err := encodeMutation(hostsessions.Mutation{
		Entry: mo.None[session.Entry](), Navigation: mo.Some(hostsessions.NavigationMutation{
			DestinationID: command.Tree.ActiveLeafID(), BranchSummary: mo.None[session.Entry](),
		}),
		Label: mo.None[hostsessions.LabelMutation](), SessionInformation: mo.None[hostsessions.SessionInformationMutation](),
	})
	if err != nil {
		return hostsessions.CreateSnapshotResult{}, err
	}
	payload = append(payload, line...)
	name := filename(command.Header)
	file, err := s.fileSystem.OpenFile(s.projectDirectory, name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fileMode)
	if err != nil {
		return hostsessions.CreateSnapshotResult{}, fmt.Errorf("open session snapshot: %w", err)
	}
	if err = persist(file, true, payload); err != nil {
		return hostsessions.CreateSnapshotResult{}, fmt.Errorf("persist session snapshot: %w", err)
	}
	return hostsessions.CreateSnapshotResult{StoragePath: filepath.Join(s.projectDirectory, name)}, nil
}

// encodeSnapshotInformation preserves optional metadata and its exact mutation timestamp together.
func encodeSnapshotInformation(
	information mo.Option[session.Information],
	updatedAt mo.Option[time.Time],
) ([]byte, error) {
	value, hasInformation := information.Get()
	timestamp, hasTimestamp := updatedAt.Get()
	if hasInformation != hasTimestamp {
		return nil, errors.New("session information and timestamp must both be present or absent")
	}
	if !hasInformation {
		return nil, nil
	}
	return encodeMutation(hostsessions.Mutation{
		Entry: mo.None[session.Entry](), Navigation: mo.None[hostsessions.NavigationMutation](),
		Label: mo.None[hostsessions.LabelMutation](), SessionInformation: mo.Some(hostsessions.SessionInformationMutation{
			Name: value.Name, CreatedAt: timestamp,
		}),
	})
}

// persistPayload writes one initial or subsequent payload through the same durability sequence.
func (s *Service) persistPayload(header session.Header, storagePath string, mutation []byte) (string, error) {
	path := storagePath
	name := filepath.Base(path)
	flags := os.O_WRONLY | os.O_APPEND
	payload := mutation
	created := path == ""
	if created {
		if header.Version != formatVersion || header.ID == "" || header.WorkingDirectory != s.workingDirectory {
			return "", errors.New("invalid session header")
		}
		name = filename(header)
		path = filepath.Join(s.projectDirectory, name)
		encodedHeader, err := encodeHeader(header)
		if err != nil {
			return "", err
		}
		payload = append(encodedHeader, payload...)
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	} else if filepath.Clean(filepath.Dir(path)) != s.projectDirectory || !strings.HasSuffix(name, ".jsonl") {
		return "", errors.New("session storage path is outside the project directory")
	}
	file, err := s.fileSystem.OpenFile(s.projectDirectory, name, flags, fileMode)
	if err != nil {
		return "", fmt.Errorf("open session file: %w", err)
	}
	if err = persist(file, created, payload); err != nil {
		return "", fmt.Errorf("persist session mutation: %w", err)
	}
	return path, nil
}

// encodeHeader encodes one validated session header.
func encodeHeader(header session.Header) ([]byte, error) {
	return encodeLine(headerRecord{
		Type: recordTypeSession, Version: header.Version, ID: string(header.ID),
		CreatedAt: header.CreatedAt.Format(time.RFC3339Nano), CWD: header.WorkingDirectory,
	})
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
