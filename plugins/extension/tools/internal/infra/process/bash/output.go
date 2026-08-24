package bash

//go:generate go tool mockgen -source=output.go -destination=output_mock_test.go -package=bash

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
)

// OutputFile is one temporary complete-output writer.
type OutputFile interface {
	io.WriteCloser
}

// outputStore preserves complete raw output only after the model-visible budget is exceeded.
type outputStore struct {
	buffer       bytes.Buffer
	textBuffer   bytes.Buffer
	tail         []byte
	file         OutputFile
	path         string
	totalBytes   int64
	totalLines   int64
	textBytes    int64
	textLines    int64
	textComplete bool
	captureErr   error
}

// newOutputStore creates an in-memory store that spills after the shared text budget.
func newOutputStore() *outputStore {
	return &outputStore{
		buffer: bytes.Buffer{}, textBuffer: bytes.Buffer{}, tail: make([]byte, 0, textbudget.MaximumBytes),
		file: nil, path: "", totalBytes: 0, totalLines: 0, textBytes: 0, textLines: 0,
		textComplete: true, captureErr: nil,
	}
}

// append records one serialized stdout or stderr fragment without unbounded memory growth.
func (s *outputStore) append(content []byte) error {
	if s.captureErr != nil {
		return s.captureErr
	}
	additionalLines := int64(bytes.Count(content, []byte{'\n'}))
	nextBytes := s.totalBytes + int64(len(content))
	nextLines := s.totalLines + additionalLines
	s.totalBytes = nextBytes
	s.totalLines = nextLines
	if s.file == nil && (nextBytes > textbudget.MaximumBytes || nextLines > textbudget.MaximumLines) {
		if err := s.spill(); err != nil {
			return s.failCapture(err)
		}
	}
	if s.file == nil {
		_, _ = s.buffer.Write(content)
	} else if _, err := s.file.Write(content); err != nil {
		return s.failCapture(fmt.Errorf("write complete bash output: %w", err))
	}
	return nil
}

// finish closes retained output and builds one bounded terminal result.
func (s *outputStore) finish(exitCode int, cause error) (string, textbudget.Truncation, error) {
	status := fmt.Sprintf("[Exit code: %d]\n", exitCode)
	if cause != nil {
		status = fmt.Sprintf("[%s]\n", cause)
	}
	if s.captureErr != nil {
		return s.captureFailure(status), s.metadata(true), s.captureErr
	}
	if s.textComplete {
		candidate := joinBashOutput(s.textBuffer.String(), status)
		if withinTextBudget(candidate) {
			cleanupErr := s.discard()
			return candidate, s.metadata(false), cleanupErr
		}
	}
	if s.file == nil {
		if err := s.spill(); err != nil {
			captureErr := s.failCapture(err)
			return s.captureFailure(status), s.metadata(true), captureErr
		}
	}
	if err := s.closeRetained(); err != nil {
		return s.captureFailure(status), s.metadata(true), err
	}

	notice := fmt.Sprintf("[Output truncated. Full output: %s]\n", s.path)
	footer := notice + status
	separator := "\n\n"
	availableBytes := textbudget.MaximumBytes - len(separator) - len(footer)
	availableLines := textbudget.MaximumLines - strings.Count(separator+footer, "\n")
	visible := boundedTail(s.tail, availableBytes, availableLines)
	return visible + separator + footer, s.metadata(true), nil
}

// captureFailure builds bounded terminal text without advertising incomplete output.
func (s *outputStore) captureFailure(status string) string {
	notice := "[Complete output capture failed; showing bounded tail.]\n"
	footer := notice + status
	separator := "\n\n"
	availableBytes := textbudget.MaximumBytes - len(separator) - len(footer)
	availableLines := textbudget.MaximumLines - strings.Count(separator+footer, "\n")
	visible := boundedTail(s.tail, availableBytes, availableLines)
	return visible + separator + footer
}

// appendText records one valid UTF-8 fragment in terminal delivery order.
func (s *outputStore) appendText(content string) {
	data := []byte(content)
	s.appendTail(data)
	s.textBytes += int64(len(data))
	s.textLines += int64(bytes.Count(data, []byte{'\n'}))
	if !s.textComplete {
		return
	}
	if s.textBytes > textbudget.MaximumBytes || s.textLines > textbudget.MaximumLines {
		s.textBuffer.Reset()
		s.textComplete = false
		return
	}
	_, _ = s.textBuffer.Write(data)
}

// discard removes retained output when no terminal result can expose it.
func (s *outputStore) discard() error {
	var closeErr error
	if s.file != nil {
		closeErr = s.file.Close()
		s.file = nil
		if closeErr != nil {
			closeErr = fmt.Errorf("close discarded bash output: %w", closeErr)
		}
	}
	removeErr := s.removePath()
	return errors.Join(closeErr, removeErr)
}

// closeRetained closes a complete file and removes it when finalization fails.
func (s *outputStore) closeRetained() error {
	if s.file == nil {
		return nil
	}
	closeErr := s.file.Close()
	s.file = nil
	if closeErr == nil {
		return nil
	}
	captureErr := errors.Join(fmt.Errorf("close complete bash output: %w", closeErr), s.removePath())
	s.captureErr = captureErr
	return captureErr
}

// removePath removes a temporary output path even after file close fails.
func (s *outputStore) removePath() error {
	if s.path == "" {
		return nil
	}
	path := s.path
	s.path = ""
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove bash output: %w", err)
	}
	return nil
}

// failCapture removes incomplete retained output and records the storage error.
func (s *outputStore) failCapture(cause error) error {
	cleanupErr := s.discard()
	s.captureErr = errors.Join(cause, cleanupErr)
	return s.captureErr
}

// spill creates the retained file and writes the bounded in-memory prefix.
func (s *outputStore) spill() error {
	file, err := os.CreateTemp("", "glyph-bash-*.log")
	if err != nil {
		return fmt.Errorf("create complete bash output: %w", err)
	}
	s.file = file
	s.path = file.Name()
	if _, err = file.Write(s.buffer.Bytes()); err != nil {
		return fmt.Errorf("write initial bash output: %w", err)
	}
	s.buffer.Reset()
	return nil
}

// appendTail keeps only the bytes that can contribute to the terminal tail.
func (s *outputStore) appendTail(content []byte) {
	if len(content) >= textbudget.MaximumBytes {
		s.tail = append(s.tail[:0], content[len(content)-textbudget.MaximumBytes:]...)
		return
	}
	s.tail = append(s.tail, content...)
	if excess := len(s.tail) - textbudget.MaximumBytes; excess > 0 {
		copy(s.tail, s.tail[excess:])
		s.tail = s.tail[:textbudget.MaximumBytes]
	}
}

// metadata reports the terminal text totals and retained file location.
func (s *outputStore) metadata(truncated bool) textbudget.Truncation {
	path := ""
	if truncated {
		path = s.path
	}
	return textbudget.Truncation{
		Truncated:      truncated,
		TotalBytes:     s.textBytes,
		TotalLines:     s.textLines,
		FullOutputPath: path,
	}
}

// joinBashOutput separates command output from its terminal status.
func joinBashOutput(output, status string) string {
	if output == "" {
		return status
	}
	return output + "\n\n" + status
}

// withinTextBudget checks the complete terminal text against both limits.
func withinTextBudget(content string) bool {
	return len(content) <= textbudget.MaximumBytes && strings.Count(content, "\n") <= textbudget.MaximumLines
}

// boundedTail keeps the newest valid UTF-8 text within the remaining budget.
func boundedTail(content []byte, maximumBytes, maximumLines int) string {
	if maximumBytes <= 0 || maximumLines < 0 {
		return ""
	}
	if len(content) > maximumBytes {
		content = content[len(content)-maximumBytes:]
	}
	content = bytes.ToValidUTF8(content, []byte("?"))
	for len(content) > maximumBytes {
		content = content[1:]
		content = bytes.ToValidUTF8(content, []byte("?"))
	}
	for bytes.Count(content, []byte{'\n'}) > maximumLines {
		newline := bytes.IndexByte(content, '\n')
		if newline < 0 {
			return ""
		}
		content = content[newline+1:]
	}
	return string(content)
}
