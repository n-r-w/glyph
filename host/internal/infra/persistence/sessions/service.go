// Package sessions stores session lifecycle records as JSONL files.
package sessions

import (
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

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

const (
	directoryMode  = 0o700
	fileMode       = 0o600
	readBufferSize = 32 * 1024
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

// userRecord stores ordered provider-neutral user content.
type userRecord struct {
	Type      string         `json:"type"`
	ID        string         `json:"id"`
	CreatedAt string         `json:"createdAt"`
	Message   *messageRecord `json:"message"`
}

type messageRecord struct {
	Content []inputContentRecord `json:"content"`
}

type inputContentRecord struct {
	Kind      model.InputContentKind `json:"kind"`
	Text      *string                `json:"text,omitempty"`
	MediaType *string                `json:"mediaType,omitempty"`
	Data      json.RawMessage        `json:"data,omitempty"`
}

// modelRecord stores one provider-neutral terminal model response.
type modelRecord struct {
	Type          string               `json:"type"`
	ID            string               `json:"id"`
	CreatedAt     string               `json:"createdAt"`
	Response      modelResponseRecord  `json:"response"`
	EstimatedCost *estimatedCostRecord `json:"estimatedCost,omitempty"`
}

type estimatedCostRecord struct {
	Input      *float64 `json:"input"`
	Output     *float64 `json:"output"`
	CacheRead  *float64 `json:"cacheRead"`
	CacheWrite *float64 `json:"cacheWrite"`
	Total      *float64 `json:"total"`
}

type modelResponseRecord struct {
	Content       []modelContentRecord `json:"content"`
	Outcome       model.Outcome        `json:"outcome"`
	ErrorMessage  *string              `json:"errorMessage,omitempty"`
	Provider      *string              `json:"provider,omitempty"`
	Model         *string              `json:"model,omitempty"`
	ResponseModel *string              `json:"responseModel,omitempty"`
	ResponseID    *string              `json:"responseId,omitempty"`
	Usage         *usageRecord         `json:"usage,omitempty"`
	Diagnostics   []diagnosticRecord   `json:"diagnostics"`
}

type diagnosticRecord struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type modelContentRecord struct {
	Kind            model.ContentKind      `json:"kind"`
	Text            *string                `json:"text,omitempty"`
	ProviderContext *providerContextRecord `json:"providerContext,omitempty"`
	ToolCall        *toolCallRecord        `json:"toolCall,omitempty"`
}

type providerContextRecord struct {
	ProviderID       string  `json:"providerId"`
	API              string  `json:"api"`
	Model            string  `json:"model"`
	CompatibilityKey *string `json:"compatibilityKey,omitempty"`
	Payload          []byte  `json:"payload"`
}

type toolCallRecord struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type usageRecord struct {
	InputTokens       int64 `json:"inputTokens"`
	OutputTokens      int64 `json:"outputTokens"`
	CachedInputTokens int64 `json:"cachedInputTokens"`
	CacheWriteTokens  int64 `json:"cacheWriteTokens"`
	ReasoningTokens   int64 `json:"reasoningTokens"`
	TotalTokens       int64 `json:"totalTokens"`
}

type toolResultRecord struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	CreatedAt string          `json:"createdAt"`
	Result    toolResultValue `json:"result"`
}

type toolResultValue struct {
	CallID   string                    `json:"callId"`
	ToolName string                    `json:"toolName"`
	Contents []toolResultContentRecord `json:"contents"`
	IsError  bool                      `json:"isError"`
}

type toolResultContentRecord struct {
	Kind      tool.ResultContentKind `json:"kind"`
	Text      *string                `json:"text,omitempty"`
	MediaType *string                `json:"mediaType,omitempty"`
	Data      json.RawMessage        `json:"data,omitempty"`
}

// extensionRecord stores compact extension-owned JSON without interpreting it.
type extensionRecord struct {
	Type        string          `json:"type"`
	ID          string          `json:"id"`
	CreatedAt   string          `json:"createdAt"`
	ExtensionID string          `json:"extensionId"`
	EntryType   string          `json:"entryType"`
	Data        json.RawMessage `json:"data"`
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
	loaded      hostsessions.LoadedSession
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
		if !entry.Type().IsRegular() {
			warnUnavailableSession(ctx, "list", listDiagnosticNonregularSessionFile)
			continue
		}
		pathResult, loadErr := s.loadPath(ctx, entry.Name(), loadForList)
		if loadErr != nil {
			warnUnavailableSession(ctx, "list", safeListWarningDiagnostic(loadErr))
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
	pathResult.loaded.Entries = scanned.entries
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
	header       session.Header
	entries      []session.Entry
	completeSize int64
	interrupted  bool
}

// scan classifies only final nonempty bytes as interrupted after every completed record validates.
func (s *Service) scan(payload []byte) (scanResult, error) {
	headerEnd := bytes.IndexByte(payload, '\n')
	if headerEnd < 0 {
		return scanResult{}, errors.New("session header is missing or interrupted")
	}
	header, err := s.decodeHeader(payload[:headerEnd])
	result := scanResult{header: header, entries: nil, completeSize: int64(len(payload)), interrupted: false}
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
	entries, err := decodeEntries(payload[headerEnd+1 : completeSize])
	if err != nil {
		return result, err
	}
	result.entries = entries
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
	decodeErr := errors.Join(decodeRecord(data, &record), validateHeaderRequiredFields(data))
	createdAt, timeErr := time.Parse(time.RFC3339Nano, record.CreatedAt)
	header := session.Header{
		Version:          record.Version,
		ID:               session.ID(record.ID),
		CreatedAt:        createdAt,
		WorkingDirectory: record.CWD,
	}
	if timeErr != nil || record.Type != "session" || record.ID == "" || record.CWD != s.workingDirectory {
		return header, errors.New("invalid session header")
	}
	if decodeErr != nil {
		return header, fmt.Errorf("decode session header: %w", decodeErr)
	}
	if record.Version != 1 {
		return header, fmt.Errorf("unsupported session version %d", record.Version)
	}
	return header, nil
}

// decodeEntries preserves file order and rejects duplicate entry identities.
func decodeEntries(payload []byte) ([]session.Entry, error) {
	entries := make([]session.Entry, 0)
	identities := make(map[string]struct{})
	for len(payload) > 0 {
		lineEnd := bytes.IndexByte(payload, '\n')
		if lineEnd < 0 {
			return nil, errors.New("completed session record is missing a newline")
		}
		entry, err := decodeEntry(payload[:lineEnd])
		if err != nil {
			return nil, err
		}
		if _, exists := identities[entry.ID]; exists {
			return nil, errors.New("duplicate session entry ID")
		}
		identities[entry.ID] = struct{}{}
		entries = append(entries, entry)
		payload = payload[lineEnd+1:]
	}
	return entries, nil
}

type listWarningDiagnostic string

const (
	listDiagnosticInvalidSessionFile    listWarningDiagnostic = "invalid_session_file"
	listDiagnosticNonregularSessionFile listWarningDiagnostic = "nonregular_session_file"
)

type nonregularSessionFileError struct{}

func (nonregularSessionFileError) Error() string { return "session file is not regular" }

// safeListWarningDiagnostic classifies failures without allowing raw storage or decode errors into logs.
func safeListWarningDiagnostic(err error) listWarningDiagnostic {
	var nonregular nonregularSessionFileError
	if errors.As(err, &nonregular) {
		return listDiagnosticNonregularSessionFile
	}
	return listDiagnosticInvalidSessionFile
}

// warnUnavailableSession accepts only closed diagnostics so raw errors and IDs cannot enter list logs.
func warnUnavailableSession(ctx context.Context, operation string, diagnostic listWarningDiagnostic) {
	slog.WarnContext(
		ctx,
		"session file is unavailable",
		"operation", operation,
		"diagnostic", diagnostic,
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

// decodeEntry selects one strict version 1 record without exposing repository DTOs.
func decodeEntry(data []byte) (session.Entry, error) {
	var kind recordType
	// The discriminator selects the closed record DTO. The selected DTO then performs strict decoding.
	if err := json.Unmarshal(data, &kind); err != nil {
		return session.Entry{}, errors.New("invalid session entry")
	}
	if err := validateEntryRequiredFields(data, kind.Type); err != nil {
		return session.Entry{}, fmt.Errorf("validate required session fields: %w", err)
	}
	switch kind.Type {
	case "session_info":
		var record informationRecord
		if err := decodeRecord(data, &record); err != nil {
			return session.Entry{}, err
		}
		entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
		if err != nil || record.ID == "" || record.Name == "" {
			return session.Entry{}, errors.New("invalid session information entry")
		}
		return session.Entry{
			ID: record.ID, CreatedAt: entryTime, Information: mo.Some(session.Information{Name: record.Name}),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			EstimatedCost: mo.None[session.EstimatedCost](),
		}, nil
	case "user":
		return decodeUser(data)
	case "model":
		return decodeModel(data)
	case "tool_result":
		return decodeToolResult(data)
	case "extension":
		return decodeExtension(data)
	default:
		return session.Entry{}, errors.New("invalid session entry")
	}
}

func decodeUser(data []byte) (session.Entry, error) {
	var record userRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil || record.ID == "" {
		return session.Entry{}, errors.New("invalid user entry")
	}
	if record.Message == nil {
		return session.Entry{}, errors.New("invalid user entry")
	}
	var content []model.InputContent
	if record.Message.Content != nil {
		content = make([]model.InputContent, 0, len(record.Message.Content))
	}
	for index := range record.Message.Content {
		item, decodeErr := decodeInputContent(record.Message.Content[index])
		if decodeErr != nil {
			return session.Entry{}, decodeErr
		}
		content = append(content, item)
	}
	return session.Entry{
		ID: record.ID, CreatedAt: entryTime, Information: mo.None[session.Information](),
		User: mo.Some(model.Message{Content: content}), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		EstimatedCost: mo.None[session.EstimatedCost](),
	}, nil
}

func decodeInputContent(record inputContentRecord) (model.InputContent, error) {
	switch record.Kind {
	case model.InputContentText:
		if record.Text == nil || record.MediaType != nil || record.Data != nil {
			return model.InputContent{}, errors.New("invalid user text content")
		}
		return model.InputContent{
			Kind: model.InputContentText, Text: mo.Some(*record.Text),
			MediaType: mo.None[string](), Data: mo.None[[]byte](),
		}, nil
	case model.InputContentImage:
		if record.Text != nil || record.MediaType == nil || *record.MediaType == "" || record.Data == nil {
			return model.InputContent{}, errors.New("invalid user image content")
		}
		image, decodeErr := decodeBytes(record.Data)
		if decodeErr != nil {
			return model.InputContent{}, errors.New("invalid user image content")
		}
		return model.InputContent{
			Kind: model.InputContentImage, Text: mo.None[string](),
			MediaType: mo.Some(*record.MediaType), Data: mo.Some(image),
		}, nil
	default:
		return model.InputContent{}, errors.New("invalid user content")
	}
}

func decodeExtension(data []byte) (session.Entry, error) {
	var record extensionRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil || record.ID == "" || record.ExtensionID == "" || record.EntryType == "" ||
		len(record.Data) == 0 || !json.Valid(record.Data) {
		return session.Entry{}, errors.New("invalid extension entry")
	}
	return session.Entry{
		ID: record.ID, CreatedAt: entryTime, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.Some(session.ExtensionEnvelope{
			ExtensionID: record.ExtensionID, EntryType: record.EntryType, Data: bytes.Clone(record.Data),
		}), EstimatedCost: mo.None[session.EstimatedCost](),
	}, nil
}

func decodeModel(data []byte) (session.Entry, error) {
	var record modelRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil || record.ID == "" || !validOutcome(record.Response.Outcome) {
		return session.Entry{}, errors.New("invalid model entry")
	}
	response, err := decodeModelResponse(record.Response)
	if err != nil {
		return session.Entry{}, err
	}
	estimatedCost, err := decodeEstimatedCost(record.EstimatedCost)
	if err != nil {
		return session.Entry{}, err
	}
	return session.Entry{
		ID: record.ID, CreatedAt: entryTime, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.Some(response), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: estimatedCost,
	}, nil
}

func decodeToolResult(data []byte) (session.Entry, error) {
	var record toolResultRecord
	if err := decodeRecord(data, &record); err != nil {
		return session.Entry{}, err
	}
	entryTime, err := time.Parse(time.RFC3339Nano, record.CreatedAt)
	if err != nil || record.ID == "" || record.Result.CallID == "" || record.Result.ToolName == "" {
		return session.Entry{}, errors.New("invalid tool result entry")
	}
	var contents []tool.ResultContent
	if record.Result.Contents != nil {
		contents = make([]tool.ResultContent, 0, len(record.Result.Contents))
	}
	for index := range record.Result.Contents {
		content, decodeErr := decodeToolResultContent(record.Result.Contents[index])
		if decodeErr != nil {
			return session.Entry{}, decodeErr
		}
		contents = append(contents, content)
	}
	return session.Entry{
		ID: record.ID, CreatedAt: entryTime, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: record.Result.CallID, ToolName: record.Result.ToolName,
			Contents: contents, IsError: record.Result.IsError,
		}),
		Extension: mo.None[session.ExtensionEnvelope](), EstimatedCost: mo.None[session.EstimatedCost](),
	}, nil
}

// encodeEntry writes one compact record so one append always occupies one JSONL line.
func encodeEntry(entry session.Entry) ([]byte, error) {
	variants := []bool{
		entry.Information.IsSome(), entry.User.IsSome(), entry.Model.IsSome(),
		entry.ToolResult.IsSome(), entry.Extension.IsSome(),
	}
	selected := 0
	for _, present := range variants {
		if present {
			selected++
		}
	}
	if entry.ID == "" || selected != 1 {
		return nil, errors.New("invalid session entry")
	}
	if information, ok := entry.Information.Get(); ok {
		if information.Name == "" {
			return nil, errors.New("invalid session entry")
		}
		return encodeLine(informationRecord{
			Type: "session_info", ID: entry.ID,
			CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano), Name: information.Name,
		})
	}
	if user, ok := entry.User.Get(); ok {
		message, err := encodeUserMessage(user)
		if err != nil {
			return nil, err
		}
		return encodeLine(userRecord{
			Type: "user", ID: entry.ID, CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano), Message: &message,
		})
	}
	if response, ok := entry.Model.Get(); ok {
		return encodeModelEntry(entry, response)
	}
	if result, ok := entry.ToolResult.Get(); ok {
		return encodeToolResultEntry(entry, result)
	}
	extension := entry.Extension.MustGet()
	if extension.ExtensionID == "" || extension.EntryType == "" || !json.Valid(extension.Data) {
		return nil, errors.New("invalid extension entry")
	}
	// Clone opaque extension bytes before framing them as compact JSON.
	return encodeLine(extensionRecord{
		Type: "extension", ID: entry.ID, CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano),
		ExtensionID: extension.ExtensionID, EntryType: extension.EntryType,
		Data: json.RawMessage(bytes.Clone(extension.Data)),
	})
}

func encodeUserMessage(message model.Message) (messageRecord, error) {
	var content []inputContentRecord
	if message.Content != nil {
		content = make([]inputContentRecord, 0, len(message.Content))
	}
	for index := range message.Content {
		item := message.Content[index]
		switch item.Kind {
		case model.InputContentText:
			text, present := item.Text.Get()
			if !present || item.MediaType.IsSome() || item.Data.IsSome() {
				return messageRecord{}, errors.New("invalid user text content")
			}
			content = append(content, inputContentRecord{
				Kind: model.InputContentText, Text: new(text), MediaType: nil, Data: nil,
			})
		case model.InputContentImage:
			mediaType, hasMediaType := item.MediaType.Get()
			data, hasData := item.Data.Get()
			if item.Text.IsSome() || !hasMediaType || mediaType == "" || !hasData {
				return messageRecord{}, errors.New("invalid user image content")
			}
			encodedData, err := encodeBytes(data)
			if err != nil {
				return messageRecord{}, errors.New("invalid user image content")
			}
			content = append(content, inputContentRecord{
				Kind: model.InputContentImage, Text: nil, MediaType: new(mediaType), Data: encodedData,
			})
		default:
			return messageRecord{}, errors.New("invalid user content")
		}
	}
	return messageRecord{Content: content}, nil
}

func encodeModelEntry(entry session.Entry, response model.Response) ([]byte, error) {
	record, err := encodeModelResponse(response)
	if err != nil {
		return nil, err
	}
	var estimatedCost *estimatedCostRecord
	if cost, present := entry.EstimatedCost.Get(); present {
		estimatedCost = &estimatedCostRecord{
			Input: new(cost.Input), Output: new(cost.Output), CacheRead: new(cost.CacheRead),
			CacheWrite: new(cost.CacheWrite), Total: new(cost.Total),
		}
	}
	return encodeLine(modelRecord{
		Type: "model", ID: entry.ID, CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano),
		Response: record, EstimatedCost: estimatedCost,
	})
}

// decodeEstimatedCost preserves configured zero and rejects incomplete persisted cost objects.
func decodeEstimatedCost(record *estimatedCostRecord) (mo.Option[session.EstimatedCost], error) {
	if record == nil {
		return mo.None[session.EstimatedCost](), nil
	}
	if record.Input == nil || record.Output == nil || record.CacheRead == nil ||
		record.CacheWrite == nil || record.Total == nil {
		return mo.None[session.EstimatedCost](), errors.New("invalid estimated cost")
	}
	return mo.Some(session.EstimatedCost{
		Input: *record.Input, Output: *record.Output, CacheRead: *record.CacheRead,
		CacheWrite: *record.CacheWrite, Total: *record.Total,
	}), nil
}

func encodeToolResultEntry(entry session.Entry, result agent.ToolResult) ([]byte, error) {
	if result.CallID == "" || result.ToolName == "" || entry.Information.IsSome() ||
		entry.User.IsSome() || entry.Model.IsSome() {
		return nil, errors.New("invalid tool result entry")
	}
	var contents []toolResultContentRecord
	if result.Contents != nil {
		contents = make([]toolResultContentRecord, 0, len(result.Contents))
	}
	for index := range result.Contents {
		content, err := encodeToolResultContent(result.Contents[index])
		if err != nil {
			return nil, err
		}
		contents = append(contents, content)
	}
	return encodeLine(toolResultRecord{
		Type: "tool_result", ID: entry.ID, CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano),
		Result: toolResultValue{
			CallID: result.CallID, ToolName: result.ToolName, Contents: contents, IsError: result.IsError,
		},
	})
}

func encodeToolResultContent(content tool.ResultContent) (toolResultContentRecord, error) {
	switch content.Kind {
	case tool.ResultContentText:
		text, present := content.Text.Get()
		if !present || content.Image.IsSome() {
			return toolResultContentRecord{}, errors.New("invalid tool result content")
		}
		return toolResultContentRecord{
			Kind: tool.ResultContentText, Text: new(text), MediaType: nil, Data: nil,
		}, nil
	case tool.ResultContentImage:
		image, present := content.Image.Get()
		if !present || content.Text.IsSome() || image.MediaType == "" {
			return toolResultContentRecord{}, errors.New("invalid tool result content")
		}
		encodedData, err := encodeBytes(image.Data)
		if err != nil {
			return toolResultContentRecord{}, errors.New("invalid tool result content")
		}
		return toolResultContentRecord{
			Kind: tool.ResultContentImage, Text: nil, MediaType: new(image.MediaType), Data: encodedData,
		}, nil
	default:
		return toolResultContentRecord{}, errors.New("invalid tool result content")
	}
}

func decodeToolResultContent(record toolResultContentRecord) (tool.ResultContent, error) {
	switch record.Kind {
	case tool.ResultContentText:
		if record.Text == nil || record.MediaType != nil || record.Data != nil {
			return tool.ResultContent{}, errors.New("invalid tool result text content")
		}
		return tool.ResultContent{
			Kind: tool.ResultContentText, Text: mo.Some(*record.Text), Image: mo.None[tool.ResultImage](),
		}, nil
	case tool.ResultContentImage:
		if record.Text != nil || record.MediaType == nil || *record.MediaType == "" || record.Data == nil {
			return tool.ResultContent{}, errors.New("invalid tool result image content")
		}
		data, err := decodeBytes(record.Data)
		if err != nil {
			return tool.ResultContent{}, errors.New("invalid tool result image content")
		}
		return tool.ResultContent{
			Kind: tool.ResultContentImage, Text: mo.None[string](),
			Image: mo.Some(tool.ResultImage{MediaType: *record.MediaType, Data: data}),
		}, nil
	default:
		return tool.ResultContent{}, errors.New("invalid tool result content")
	}
}

// encodeBytes keeps present nil and empty byte slices distinct in repository JSON.
func encodeBytes(data []byte) (json.RawMessage, error) {
	encoded, err := json.Marshal(bytes.Clone(data))
	if err != nil {
		return nil, err
	}
	return json.RawMessage(encoded), nil
}

func decodeBytes(data json.RawMessage) ([]byte, error) {
	var decoded []byte
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

// encodeModelResponse preserves terminal continuation fields and their option presence.
func encodeModelResponse(response model.Response) (modelResponseRecord, error) {
	outcome, present := response.Outcome.Get()
	if !present || !validOutcome(outcome) {
		return modelResponseRecord{}, errors.New("invalid model entry")
	}
	var content []modelContentRecord
	if response.Content != nil {
		content = make([]modelContentRecord, 0, len(response.Content))
	}
	for index := range response.Content {
		record, err := encodeModelContent(&response.Content[index])
		if err != nil {
			return modelResponseRecord{}, err
		}
		content = append(content, record)
	}
	var diagnostics []diagnosticRecord
	if response.Diagnostics != nil {
		diagnostics = make([]diagnosticRecord, len(response.Diagnostics))
		for index := range response.Diagnostics {
			diagnostics[index] = diagnosticRecord{
				Code: response.Diagnostics[index].Code, Message: response.Diagnostics[index].Message,
			}
		}
	}
	result := modelResponseRecord{
		Content: content, Outcome: outcome,
		ErrorMessage:  optionStringPointer(response.ErrorMessage),
		Provider:      optionProviderIDPointer(response.Provider),
		Model:         optionModelIDPointer(response.Model),
		ResponseModel: optionModelIDPointer(response.ResponseModel),
		ResponseID:    optionStringPointer(response.ResponseID), Usage: nil, Diagnostics: diagnostics,
	}
	if usage, ok := response.Usage.Get(); ok {
		result.Usage = &usageRecord{
			InputTokens: usage.InputTokens, OutputTokens: usage.OutputTokens,
			CachedInputTokens: usage.CachedInputTokens, CacheWriteTokens: usage.CacheWriteTokens,
			ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens,
		}
	}
	return result, nil
}

// encodeModelContent stores public text, tool calls, and opaque replay context in response order.
func encodeModelContent(item *model.Content) (modelContentRecord, error) {
	if !item.Final {
		return modelContentRecord{}, errors.New("model content is not final")
	}
	if err := validateModelContentShape(
		item.Kind, item.Text.IsSome(), item.ProviderContext.IsSome(), item.ToolCall.IsSome(),
	); err != nil {
		return modelContentRecord{}, err
	}
	record := modelContentRecord{
		Kind: item.Kind, Text: optionStringPointer(item.Text), ProviderContext: nil, ToolCall: nil,
	}
	if contextValue, ok := item.ProviderContext.Get(); ok {
		record.ProviderContext = &providerContextRecord{
			ProviderID: string(contextValue.Source.ProviderID), API: contextValue.Source.API,
			Model:            string(contextValue.Source.Model),
			CompatibilityKey: optionStringPointer(contextValue.Source.CompatibilityKey),
			Payload:          bytes.Clone(contextValue.Payload),
		}
	}
	if call, ok := item.ToolCall.Get(); ok {
		if call.ID == "" || call.Name == "" {
			return modelContentRecord{}, errors.New("invalid model tool call content")
		}
		record.ToolCall = &toolCallRecord{ID: call.ID, Name: call.Name, Arguments: call.Arguments}
	}
	return record, nil
}

func validateModelContentShape(
	kind model.ContentKind,
	hasText bool,
	hasProviderContext bool,
	hasToolCall bool,
) error {
	var valid bool
	switch kind {
	case model.ContentText, model.ContentRefusal:
		valid = hasText && !hasProviderContext && !hasToolCall
	case model.ContentToolCall:
		valid = !hasText && !hasProviderContext && hasToolCall
	case model.ContentReasoning:
		valid = !hasToolCall && (hasText || hasProviderContext)
	default:
		return errors.New("unsupported model content")
	}
	if !valid {
		return errors.New("invalid model content shape")
	}
	return nil
}

// decodeModelResponse reconstructs provider history without exposing persistence DTOs.
func decodeModelResponse(record modelResponseRecord) (model.Response, error) {
	var content []model.Content
	if record.Content != nil {
		content = make([]model.Content, 0, len(record.Content))
	}
	for index := range record.Content {
		value, err := decodeModelContent(&record.Content[index])
		if err != nil {
			return model.Response{}, err
		}
		content = append(content, value)
	}
	var diagnostics []model.Diagnostic
	if record.Diagnostics != nil {
		diagnostics = make([]model.Diagnostic, len(record.Diagnostics))
		for index := range record.Diagnostics {
			diagnostics[index] = model.Diagnostic{
				Code: record.Diagnostics[index].Code, Message: record.Diagnostics[index].Message,
			}
		}
	}
	result := model.Response{
		Content: content, Outcome: mo.Some(record.Outcome), ErrorMessage: pointerStringOption(record.ErrorMessage),
		Provider:      pointerProviderIDOption(record.Provider),
		Model:         pointerModelIDOption(record.Model),
		ResponseModel: pointerModelIDOption(record.ResponseModel),
		ResponseID:    pointerStringOption(record.ResponseID), Usage: mo.None[model.Usage](), Diagnostics: diagnostics,
	}
	if record.Usage != nil {
		result.Usage = mo.Some(model.Usage{
			InputTokens: record.Usage.InputTokens, OutputTokens: record.Usage.OutputTokens,
			CachedInputTokens: record.Usage.CachedInputTokens, CacheWriteTokens: record.Usage.CacheWriteTokens,
			ReasoningTokens: record.Usage.ReasoningTokens, TotalTokens: record.Usage.TotalTokens,
		})
	}
	return result, nil
}

// decodeModelContent rebuilds one owned continuation item from its stored representation.
func decodeModelContent(item *modelContentRecord) (model.Content, error) {
	value := model.Content{
		Kind: item.Kind, Text: pointerStringOption(item.Text), Final: true,
		ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
	}
	if item.ProviderContext != nil {
		value.ProviderContext = mo.Some(model.ProviderContext{
			Source: model.ProviderContextSource{
				ProviderID: model.ProviderID(item.ProviderContext.ProviderID), API: item.ProviderContext.API,
				Model:            model.ID(item.ProviderContext.Model),
				CompatibilityKey: pointerStringOption(item.ProviderContext.CompatibilityKey),
			},
			Payload: bytes.Clone(item.ProviderContext.Payload),
		})
	}
	if err := validateModelContentShape(
		item.Kind, item.Text != nil, item.ProviderContext != nil, item.ToolCall != nil,
	); err != nil {
		return model.Content{}, err
	}
	if item.ToolCall != nil {
		if item.ToolCall.ID == "" || item.ToolCall.Name == "" {
			return model.Content{}, errors.New("invalid model tool call content")
		}
		value.ToolCall = mo.Some(model.ToolCall{
			ID: item.ToolCall.ID, Name: item.ToolCall.Name, Arguments: item.ToolCall.Arguments,
		})
	}
	return value, nil
}

func validOutcome(outcome model.Outcome) bool {
	return outcome >= model.OutcomeStop && outcome <= model.OutcomeFailed
}

func optionStringPointer(option mo.Option[string]) *string {
	value, present := option.Get()
	if !present {
		return nil
	}
	return &value
}

func pointerStringOption(value *string) mo.Option[string] {
	if value == nil {
		return mo.None[string]()
	}
	return mo.Some(*value)
}

func optionProviderIDPointer(option mo.Option[model.ProviderID]) *string {
	value, present := option.Get()
	if !present {
		return nil
	}
	result := string(value)
	return &result
}

func optionModelIDPointer(option mo.Option[model.ID]) *string {
	value, present := option.Get()
	if !present {
		return nil
	}
	result := string(value)
	return &result
}

func pointerProviderIDOption(value *string) mo.Option[model.ProviderID] {
	if value == nil {
		return mo.None[model.ProviderID]()
	}
	return mo.Some(model.ProviderID(*value))
}

func pointerModelIDOption(value *string) mo.Option[model.ID] {
	if value == nil {
		return mo.None[model.ID]()
	}
	return mo.Some(model.ID(*value))
}

// encodeLine adds the record delimiter included in each synchronized append.
func encodeLine(value any) ([]byte, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

// decodeRecord accepts exactly one JSON value whose core fields match the selected DTO.
func decodeRecord(data []byte, target any) error {
	// Core records use a closed schema so format changes require a new version.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON values in one session record")
		}
		return err
	}
	return nil
}

// filename keeps creation ordering visible while retaining the opaque session ID.
func filename(header session.Header) string {
	return header.CreatedAt.UTC().Format("20060102T150405.000000000Z") + "-" + string(header.ID) + ".jsonl"
}
