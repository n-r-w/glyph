package sessions

import (
	"bytes"
	"context"
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

// loadPurpose limits one file descriptor to discovery, preparation, or tail mutation.
type loadPurpose uint8

const (
	loadForList loadPurpose = iota
	loadForProbe
	loadForMatchedResume
	loadForTailRecovery
)

// loadedPath carries validated session data and tail classification between resume steps.
type loadedPath struct {
	// loaded contains validated session data.
	loaded hostsessions.LoadedSession
	// interrupted reports whether the file has an incomplete tail.
	interrupted bool
}

// List loads all valid stored sessions.
func (s *Service) List(ctx context.Context) ([]hostsessions.LoadedSession, error) {
	entries, err := os.ReadDir(s.projectDirectory)
	if err != nil {
		return nil, fmt.Errorf("read project session directory: %w", err)
	}
	result := make([]hostsessions.LoadedSession, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		candidatePath := filepath.Join(s.projectDirectory, entry.Name())
		if !entry.Type().IsRegular() {
			nonregularErr := nonregularSessionFileError{}
			warnUnavailableSession(ctx, "list", candidatePath, listDiagnosticNonregularSessionFile, nonregularErr)
			continue
		}
		pathResult, loadErr := s.loadPath(ctx, entry.Name(), loadForList)
		if loadErr != nil {
			warnUnavailableSession(ctx, "list", candidatePath, classifyListWarningDiagnostic(loadErr), loadErr)
			continue
		}
		result = append(result, pathResult.loaded)
	}
	return result, nil
}

// Load loads one stored session by validated header ID.
func (s *Service) Load(ctx context.Context, id session.ID) (hostsessions.LoadedSession, error) {
	entries, err := os.ReadDir(s.projectDirectory)
	if err != nil {
		return hostsessions.LoadedSession{}, fmt.Errorf("read project session directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		candidate, loadErr := s.loadPath(ctx, entry.Name(), loadForProbe)
		if candidate.loaded.Header.ID != id {
			continue
		}
		if loadErr != nil {
			return hostsessions.LoadedSession{}, fmt.Errorf("%w: %w", session.ErrUnavailable, loadErr)
		}
		prepared, loadErr := s.loadPath(ctx, entry.Name(), loadForMatchedResume)
		if loadErr != nil {
			if prepared.interrupted {
				return hostsessions.LoadedSession{}, fmt.Errorf("%w: %w", session.ErrPersistenceUnavailable, loadErr)
			}
			return hostsessions.LoadedSession{}, fmt.Errorf("%w: %w", session.ErrUnavailable, loadErr)
		}
		if !prepared.interrupted {
			return prepared.loaded, nil
		}
		recovered, loadErr := s.loadPath(ctx, entry.Name(), loadForTailRecovery)
		if loadErr != nil {
			return hostsessions.LoadedSession{}, fmt.Errorf("%w: %w", session.ErrPersistenceUnavailable, loadErr)
		}
		return recovered.loaded, nil
	}
	return hostsessions.LoadedSession{}, os.ErrNotExist
}

// loadPath revalidates one descriptor before applying operations allowed for its purpose.
func (s *Service) loadPath(
	ctx context.Context,
	name string,
	purpose loadPurpose,
) (pathResult loadedPath, resultErr error) {
	flags := os.O_RDONLY
	if purpose == loadForTailRecovery {
		flags = os.O_RDWR
	}
	file, err := s.fileSystem.OpenFile(s.projectDirectory, name, flags, 0)
	if err != nil {
		return loadedPath{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()

	info, err := file.Stat()
	if err != nil {
		return loadedPath{}, fmt.Errorf("inspect session file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return loadedPath{}, nonregularSessionFileError{}
	}
	payload, err := readPayload(file)
	if err != nil {
		return loadedPath{}, fmt.Errorf("read session file: %w", err)
	}
	scanned, err := s.scan(payload)
	pathResult.loaded.Header = scanned.header
	if err != nil {
		return pathResult, err
	}
	pathResult.loaded.StoragePath = filepath.Join(s.projectDirectory, name)
	pathResult.loaded.Tree = scanned.tree
	pathResult.loaded.Information = scanned.information
	pathResult.loaded.InformationUpdatedAt = scanned.informationUpdatedAt
	pathResult.interrupted = scanned.interrupted

	if purpose == loadForTailRecovery && scanned.interrupted {
		if err = file.Truncate(scanned.completeSize); err != nil {
			return pathResult, fmt.Errorf("truncate interrupted session record: %w", err)
		}
		if err = file.Sync(); err != nil {
			return pathResult, fmt.Errorf("sync recovered session file: %w", err)
		}
		if err = file.Chmod(fileMode); err != nil {
			return pathResult, fmt.Errorf("enforce recovered session mode: %w", err)
		}
		warnRecoveredSession(ctx, scanned.header.ID)
		return pathResult, nil
	}
	shouldRepairMode := purpose == loadForMatchedResume ||
		purpose == loadForList && !scanned.interrupted
	if repairErr := repairSessionFileMode(file, info.Mode(), shouldRepairMode); repairErr != nil {
		return pathResult, repairErr
	}
	return pathResult, nil
}

// repairSessionFileMode changes mode only after the caller validates a mutation-safe path.
func repairSessionFileMode(file File, currentMode os.FileMode, enabled bool) error {
	if !enabled || currentMode.Perm() == fileMode {
		return nil
	}
	if err := file.Chmod(fileMode); err != nil {
		return fmt.Errorf("set session file mode: %w", err)
	}
	return nil
}

// scanResult retains the validated prefix and the byte boundary for optional recovery.
type scanResult struct {
	// header contains the validated immutable session header.
	header session.Header
	// tree contains the validated aggregate replay result.
	tree session.Tree
	// information contains the latest session-information mutation.
	information mo.Option[session.Information]
	// informationUpdatedAt contains the latest metadata mutation timestamp.
	informationUpdatedAt mo.Option[time.Time]
	// completeSize is the byte boundary after the last complete record.
	completeSize int64
	// interrupted reports whether nonempty incomplete tail bytes remain.
	interrupted bool
}

// scan classifies only final nonempty bytes as interrupted after every completed record validates.
func (s *Service) scan(payload []byte) (scanResult, error) {
	headerEnd := bytes.IndexByte(payload, '\n')
	if headerEnd < 0 {
		return scanResult{}, errors.New("session header is missing or interrupted")
	}
	header, err := s.decodeHeader(payload[:headerEnd])
	result := scanResult{
		header: header, tree: session.Tree{}, information: mo.None[session.Information](),
		informationUpdatedAt: mo.None[time.Time](), completeSize: int64(len(payload)), interrupted: false,
	}
	if err != nil {
		return result, err
	}

	completeSize := len(payload)
	if len(payload) > 0 && payload[len(payload)-1] != '\n' {
		lastNewline := bytes.LastIndexByte(payload, '\n')
		completeSize = lastNewline + 1
		result.completeSize = int64(completeSize)
		result.interrupted = true
	}
	state, err := decodeMutations(payload[headerEnd+1 : completeSize])
	if err != nil {
		return result, err
	}
	result.tree = state.tree
	result.information = state.information
	result.informationUpdatedAt = state.informationUpdatedAt
	return result, nil
}

// readPayload reads one confined file without imposing a session record size limit.
func readPayload(file File) ([]byte, error) {
	result := make([]byte, 0)
	buffer := make([]byte, readBufferSize)
	for {
		count, err := file.ReadPayload(buffer)
		if count > 0 {
			result = append(result, buffer[:count]...)
		}
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		if count == 0 {
			return nil, io.ErrNoProgress
		}
	}
}

// decodeHeader rejects schema, version, identity, and project-binding mismatches.
func (s *Service) decodeHeader(data []byte) (session.Header, error) {
	var record headerRecord
	decodeErr := decodeRecord(data, &record)
	header := session.Header{
		Version:          record.Version,
		ID:               session.ID(record.ID),
		CreatedAt:        time.Time{},
		WorkingDirectory: record.CWD,
	}
	if decodeErr != nil {
		return header, fmt.Errorf("decode session header: %w", decodeErr)
	}
	if requiredErr := validateHeaderRequiredFields(data); requiredErr != nil {
		return header, fmt.Errorf("decode session header: %w", requiredErr)
	}
	createdAt, timeErr := time.Parse(time.RFC3339Nano, record.CreatedAt)
	header.CreatedAt = createdAt
	if timeErr != nil {
		return header, fmt.Errorf("decode session header record timestamp: %w", timeErr)
	}
	if record.Type != recordTypeSession || record.ID == "" || record.CWD != s.workingDirectory {
		return header, errors.New("invalid session header")
	}
	if record.Version != formatVersion {
		return header, fmt.Errorf("unsupported session version %d", record.Version)
	}
	return header, nil
}

type listWarningDiagnostic string

const (
	listDiagnosticInvalidSessionFile    listWarningDiagnostic = "invalid_session_file"
	listDiagnosticNonregularSessionFile listWarningDiagnostic = "nonregular_session_file"
)

type nonregularSessionFileError struct{}

func (nonregularSessionFileError) Error() string { return "session file is not regular" }

// classifyListWarningDiagnostic maps a list failure to its closed diagnostic category.
func classifyListWarningDiagnostic(err error) listWarningDiagnostic {
	if _, ok := errors.AsType[nonregularSessionFileError](err); ok {
		return listDiagnosticNonregularSessionFile
	}
	return listDiagnosticInvalidSessionFile
}

// warnUnavailableSession records the closed category and the error that caused the session file to be skipped.
func warnUnavailableSession(
	ctx context.Context,
	operation string,
	path string,
	diagnostic listWarningDiagnostic,
	err error,
) {
	slog.WarnContext(
		ctx,
		"session file is unavailable",
		"operation", operation,
		"path", path,
		"diagnostic", diagnostic,
		"error", err,
	)
}

// warnRecoveredSession records a completed tail repair without stored record content.
func warnRecoveredSession(ctx context.Context, id session.ID) {
	slog.WarnContext(
		ctx,
		"recovered interrupted session tail",
		"operation", "recover_interrupted_tail",
		"session_id", id,
	)
}
