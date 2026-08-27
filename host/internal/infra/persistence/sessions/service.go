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

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
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

// userRecord stores ordered provider-neutral user content.
type userRecord struct {
	Type      string        `json:"type"`
	ID        string        `json:"id"`
	CreatedAt string        `json:"createdAt"`
	Message   messageRecord `json:"message"`
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

// decodeEntry selects one strict version 1 record without exposing repository DTOs.
func decodeEntry(data []byte) (session.Entry, error) {
	var kind recordType
	if err := decodeRecord(data, &kind); err != nil {
		return session.Entry{}, errors.New("invalid session entry")
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
			Type: "user", ID: entry.ID, CreatedAt: entry.CreatedAt.Format(time.RFC3339Nano), Message: message,
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

// decodeRecord reads the first JSON value without rejecting unknown fields or remaining bytes.
func decodeRecord(data []byte, target any) error {
	return json.NewDecoder(bytes.NewReader(data)).Decode(target)
}

// filename keeps creation ordering visible while retaining the opaque session ID.
func filename(header session.Header) string {
	return header.CreatedAt.UTC().Format("20060102T150405.000000000Z") + "-" + string(header.ID) + ".jsonl"
}
