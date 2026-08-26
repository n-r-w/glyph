// Package sessions stores session lifecycle records as JSONL files.
package sessions

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

const (
	directoryMode = 0o700
	fileMode      = 0o600
)

// headerRecord is the immutable first JSONL record that binds a file to one project.
type headerRecord struct {
	// Type must be "session".
	Type string `json:"type"`
	// Version selects the strict record schema.
	Version int `json:"version"`
	// ID is the opaque session identifier.
	ID string `json:"id"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// CWD is the canonical project path.
	CWD string `json:"cwd"`
}

// informationRecord stores one normalized user-assigned name change.
type informationRecord struct {
	// Type must be "session_info".
	Type string `json:"type"`
	// ID uniquely identifies this append.
	ID string `json:"id"`
	// CreatedAt uses RFC3339 nanosecond precision.
	CreatedAt string `json:"createdAt"`
	// Name is already normalized by the use case.
	Name string `json:"name"`
}

// recordType reads only the discriminator used to select the current record shape.
type recordType struct {
	// Type identifies the session header or session information record shape.
	Type string `json:"type"`
}

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

// List loads all valid stored sessions.
func (s *Service) List(ctx context.Context) ([]hostsessions.LoadedSession, error) {
	entries, err := os.ReadDir(s.projectDirectory)
	if err != nil {
		return nil, fmt.Errorf("read project session directory: %w", err)
	}
	result := make([]hostsessions.LoadedSession, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		loaded, loadErr := s.loadPath(entry.Name())
		if loadErr != nil {
			slog.WarnContext(ctx, "skipping unavailable session", "operation", "list", "error", loadErr)
			continue
		}
		result = append(result, loaded)
	}
	return result, nil
}

// Load loads one stored session by validated header ID.
func (s *Service) Load(_ context.Context, id session.ID) (hostsessions.LoadedSession, error) {
	entries, err := os.ReadDir(s.projectDirectory)
	if err != nil {
		return hostsessions.LoadedSession{}, fmt.Errorf("read project session directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		loaded, loadErr := s.loadPath(entry.Name())
		if loadErr == nil && loaded.Header.ID == id {
			return loaded, nil
		}
	}
	return hostsessions.LoadedSession{}, os.ErrNotExist
}

// loadPath confines file access to the project root and reports close failures to the caller.
func (s *Service) loadPath(name string) (loaded hostsessions.LoadedSession, resultErr error) {
	root, err := os.OpenRoot(s.projectDirectory)
	if err != nil {
		return hostsessions.LoadedSession{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	file, err := root.Open(name)
	if err != nil {
		return hostsessions.LoadedSession{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()

	header, entries, err := s.scan(file)
	if err != nil {
		return hostsessions.LoadedSession{}, err
	}
	return hostsessions.LoadedSession{
		Header:      header,
		StoragePath: filepath.Join(s.projectDirectory, name),
		Entries:     entries,
	}, nil
}

// scan validates the header before decoding ordered lifecycle records without a record-size limit.
func (s *Service) scan(source io.Reader) (session.Header, []session.Entry, error) {
	reader := bufio.NewReader(source)
	headerData, err := readRecord(reader)
	if errors.Is(err, io.EOF) {
		return session.Header{}, nil, errors.New("session header is missing")
	}
	if err != nil {
		return session.Header{}, nil, err
	}
	header, err := s.decodeHeader(headerData)
	if err != nil {
		return session.Header{}, nil, err
	}
	entries, err := decodeEntries(reader)
	if err != nil {
		return session.Header{}, nil, err
	}
	return header, entries, nil
}

// readRecord reads through a newline or returns the final unterminated value at EOF.
func readRecord(reader *bufio.Reader) ([]byte, error) {
	data, err := reader.ReadBytes('\n')
	if len(data) > 0 {
		return bytes.TrimSuffix(data, []byte{'\n'}), nil
	}
	return nil, err
}

// decodeHeader rejects schema, version, identity, and project-binding mismatches.
func (s *Service) decodeHeader(data []byte) (session.Header, error) {
	var record headerRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Header{}, fmt.Errorf("decode session header: %w", err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	valid := record.Type == "session" && record.Version == 1 && record.ID != "" && record.CWD == s.workingDirectory
	if err != nil || !valid {
		return session.Header{}, errors.New("invalid session header")
	}
	return session.Header{
		Version:          record.Version,
		ID:               session.ID(record.ID),
		CreatedAt:        createdAt,
		WorkingDirectory: record.CWD,
	}, nil
}

// decodeEntries preserves file order for session information records.
func decodeEntries(reader *bufio.Reader) ([]session.Entry, error) {
	entries := make([]session.Entry, 0)
	for {
		data, err := readRecord(reader)
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		entry, err := decodeEntry(data)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
}

// decodeEntry accepts only a complete session-information record.
func decodeEntry(data []byte) (session.Entry, error) {
	var kind recordType
	if err := decodeRecord(data, &kind); err != nil || kind.Type != "session_info" {
		return session.Entry{}, errors.New("invalid session entry")
	}
	var record informationRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil || record.ID == "" || record.Name == "" {
		return session.Entry{}, errors.New("invalid session information entry")
	}
	return session.Entry{
		ID:          record.ID,
		CreatedAt:   entryTime,
		Information: mo.Some(session.Information{Name: record.Name}),
	}, nil
}

// encodeEntry validates the active lifecycle variant before JSON encoding.
func encodeEntry(entry session.Entry) ([]byte, error) {
	information, ok := entry.Information.Get()
	if !ok || entry.ID == "" || information.Name == "" {
		return nil, errors.New("invalid session entry")
	}
	return encodeLine(informationRecord{
		Type:      "session_info",
		ID:        entry.ID,
		CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano),
		Name:      information.Name,
	})
}

// encodeLine adds the record delimiter included in each synchronized append.
func encodeLine(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// decodeRecord reads the first JSON value without rejecting unknown fields or remaining bytes.
func decodeRecord(data []byte, target any) error {
	return json.NewDecoder(bytes.NewReader(data)).Decode(target)
}

// filename keeps creation ordering visible while retaining the opaque session ID.
func filename(header session.Header) string {
	return header.CreatedAt.UTC().Format("20060102T150405.000000000Z") + "-" + string(header.ID) + ".jsonl"
}
