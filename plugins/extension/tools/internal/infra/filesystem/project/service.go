// Package project reads files relative to the extension process working directory.
package project

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"

	"github.com/samber/mo"

	edittool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/edit"
	readtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/read"
	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
	writetool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/write"
)

const (
	fileWritePermissions     = 0o600
	directoryPermissions     = 0o700
	contentTypeHeaderSize    = 512
	continuationReserveBytes = 128
	pngChunkOverhead         = 12
	// pngMediaType identifies PNG image data.
	pngMediaType = "image/png"
)

// isAnimatedPNG reports whether a PNG animation-control chunk is present.
func isAnimatedPNG(data []byte) bool {
	const pngSignatureSize = 8
	for offset := pngSignatureSize; offset+8 <= len(data); {
		chunkSize := int(binary.BigEndian.Uint32(data[offset : offset+4]))
		if string(data[offset+4:offset+8]) == "acTL" {
			return true
		}
		offset += pngChunkOverhead + chunkSize
	}
	return false
}

// readImageData reads exact image bytes and observes cancellation after the read completes.
func readImageData(ctx context.Context, reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return data, nil
}

// isSupportedImage reports a media type allowed in typed read results.
func isSupportedImage(mediaType string) bool {
	switch mediaType {
	case "image/bmp", "image/gif", "image/jpeg", pngMediaType, "image/webp":
		return true
	default:
		return false
	}
}

// Service provides working-project file access.
type Service struct {
	// locks serializes mutations by canonical project path.
	locks pathLocks
}

var (
	_ edittool.ProjectEditor  = (*Service)(nil)
	_ readtool.ProjectReader  = (*Service)(nil)
	_ writetool.ProjectWriter = (*Service)(nil)
	_ searchtool.ProjectFiles = (*Service)(nil)
)

// New creates a working-project filesystem service.
func New() *Service {
	return &Service{locks: pathLocks{mutex: sync.Mutex{}, locks: nil}}
}

// ReadFile returns bounded project-file content.
func (s *Service) ReadFile(
	ctx context.Context,
	path string,
	offset, limit mo.Option[uint],
) (readtool.Content, error) {
	if err := ctx.Err(); err != nil {
		return readtool.Content{}, fmt.Errorf("read project file %q: %w", path, err)
	}
	cleanPath := filepath.Clean(path)
	// fileInfo prevents reads from blocking on pipes, devices, directories, and other nonregular paths.
	fileInfo, err := os.Stat(cleanPath)
	if err != nil {
		return readtool.Content{}, fmt.Errorf("read project file %q: %w", path, err)
	}
	if !fileInfo.Mode().IsRegular() {
		return readtool.Content{}, fmt.Errorf(
			"read project file %q: path is not a regular project file: type %s",
			path,
			fileInfo.Mode().Type(),
		)
	}
	file, err := os.Open(cleanPath)
	if err != nil {
		return readtool.Content{}, fmt.Errorf("read project file %q: %w", path, err)
	}
	defer func() { _ = file.Close() }()
	header := make([]byte, contentTypeHeaderSize)
	n, _ := io.ReadFull(file, header)
	mediaType := http.DetectContentType(header[:n])
	if isSupportedImage(mediaType) {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return readtool.Content{}, fmt.Errorf("read project file %q: %w", path, ctxErr)
		}
		if _, seekErr := file.Seek(0, io.SeekStart); seekErr != nil {
			return readtool.Content{}, fmt.Errorf("seek project file %q: %w", path, seekErr)
		}
		data, readErr := readImageData(ctx, file)
		if readErr != nil {
			return readtool.Content{}, fmt.Errorf("read project file %q: %w", path, readErr)
		}
		if mediaType != pngMediaType || !isAnimatedPNG(data) {
			return readtool.Content{
				Text: mo.None[string](), Image: mo.Some(readtool.Image{MediaType: mediaType, Data: data}),
				Start: mo.None[uint](), End: mo.None[uint](), Total: mo.None[uint](),
				Next: mo.None[uint](), OversizedSize: mo.None[int64](),
			}, nil
		}
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return readtool.Content{}, fmt.Errorf("seek project file %q: %w", path, err)
	}
	return readTextContent(ctx, bufio.NewReader(file), path, offset, limit)
}

// WriteFile replaces complete project-file content directly.
func (s *Service) WriteFile(ctx context.Context, path, content string) error {
	unlock, lockErr := s.locks.lock(path)
	if lockErr != nil {
		return lockErr
	}
	defer unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write project file %q: %w", path, err)
	}
	cleanPath := filepath.Clean(path)
	if err := os.MkdirAll(filepath.Dir(cleanPath), directoryPermissions); err != nil {
		return fmt.Errorf("create parent directories for project file %q: %w", path, err)
	}
	if err := os.WriteFile(cleanPath, []byte(content), fileWritePermissions); err != nil {
		return fmt.Errorf("write project file %q: %w", path, err)
	}
	return nil
}

// UpdateFile reads, transforms, and writes one file while holding its mutation lock.
func (s *Service) UpdateFile(ctx context.Context, path string, update func([]byte) ([]byte, error)) error {
	unlock, lockErr := s.locks.lock(path)
	if lockErr != nil {
		return lockErr
	}
	defer unlock()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return fmt.Errorf("update project file %q: %w", path, ctxErr)
	}
	cleanPath := filepath.Clean(path)
	original, readErr := os.ReadFile(cleanPath)
	if readErr != nil {
		return fmt.Errorf("read project file %q: %w", path, readErr)
	}
	updated, updateErr := update(original)
	if updateErr != nil {
		return updateErr
	}
	if writeErr := os.WriteFile(cleanPath, updated, fileWritePermissions); writeErr != nil {
		return fmt.Errorf("write project file %q: %w", path, writeErr)
	}
	return nil
}
