package project

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
	readtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/read"
)

// textReadState tracks bounded text accumulation across reader fragments.
type textReadState struct {
	start         uint
	maxLines      uint
	line          uint
	selected      uint
	end           uint
	lineSize      int
	outputSize    int
	stopped       bool
	oversizedSize int64
	selectedLines []string
	lineBuffer    strings.Builder
}

// newTextReadState initializes a bounded line-selection state.
func newTextReadState(offset, limit uint) *textReadState {
	start := offset
	if start == 0 {
		start = 1
	}
	maxLines := uint(textbudget.MaximumLines)
	if limit > 0 && limit < maxLines {
		maxLines = limit
	}
	return &textReadState{
		start:         start,
		maxLines:      maxLines,
		line:          0,
		selected:      0,
		end:           0,
		lineSize:      0,
		outputSize:    0,
		stopped:       false,
		oversizedSize: 0,
		selectedLines: make([]string, 0, maxLines),
		lineBuffer:    strings.Builder{},
	}
}

// readTextContent scans text without accumulating bytes beyond the output budget.
func readTextContent(
	ctx context.Context,
	reader *bufio.Reader,
	path string,
	offset, limit uint,
) (readtool.Content, error) {
	state := newTextReadState(offset, limit)
	for {
		done, err := state.consume(ctx, reader, path)
		if err != nil {
			return readtool.Content{}, err
		}
		if done {
			break
		}
	}
	return state.content(path)
}

// consume processes one reader fragment and reports end of input.
func (s *textReadState) consume(ctx context.Context, reader *bufio.Reader, path string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, fmt.Errorf("read project file %q: %w", path, err)
	}
	fragment, readErr := reader.ReadSlice('\n')
	if readErr == io.EOF && len(fragment) == 0 {
		return true, nil
	}
	s.append(fragment)
	switch {
	case readErr == nil:
		s.finish()
		return false, nil
	case readErr == io.EOF:
		s.finish()
		return true, nil
	case errors.Is(readErr, bufio.ErrBufferFull):
		return false, nil
	default:
		return false, fmt.Errorf("read project file %q: %w", path, readErr)
	}
}

// append records a fragment only while it can belong to bounded output.
func (s *textReadState) append(fragment []byte) {
	s.lineSize += len(fragment)
	candidate := s.line + 1
	if !s.stopped && candidate >= s.start && s.selected < s.maxLines && s.lineSize <= textbudget.MaximumBytes {
		s.lineBuffer.Write(fragment)
	}
}

// finish commits the current complete line to bounded output.
func (s *textReadState) finish() {
	s.line++
	if s.line == s.start && s.lineSize > textbudget.MaximumBytes {
		s.oversizedSize = int64(s.lineSize)
	}
	canAppend := !s.stopped && s.oversizedSize == 0 && s.line >= s.start && s.selected < s.maxLines
	if canAppend && s.outputSize+s.lineSize <= textbudget.MaximumBytes {
		s.selectedLines = append(s.selectedLines, s.lineBuffer.String())
		s.outputSize += s.lineSize
		s.selected++
		s.end = s.line
	} else if s.line >= s.start && s.selected < s.maxLines {
		s.stopped = true
	}
	s.lineBuffer.Reset()
	s.lineSize = 0
}

// content returns accumulated text with continuation metadata.
func (s *textReadState) content(path string) (readtool.Content, error) {
	if s.line == 0 && s.start == 1 {
		return readtool.Content{
			Text: "", Image: nil, Start: s.start, End: 0, Total: 0, Next: 0, OversizedSize: 0,
		}, nil
	}
	if s.start > s.line {
		return readtool.Content{}, fmt.Errorf("read project file %q: offset %d is beyond end of file", path, s.start)
	}
	if s.oversizedSize > 0 {
		return readtool.Content{
			Text:          "",
			Image:         nil,
			Start:         s.start,
			End:           0,
			Total:         s.line,
			Next:          0,
			OversizedSize: s.oversizedSize,
		}, nil
	}
	if s.end < s.line {
		firstLineSize := 0
		for len(s.selectedLines) > 0 && s.needsMoreNoticeSpace() {
			last := len(s.selectedLines) - 1
			firstLineSize = len(s.selectedLines[last])
			s.outputSize -= firstLineSize
			s.selectedLines = s.selectedLines[:last]
			s.end--
		}
		if len(s.selectedLines) == 0 {
			return readtool.Content{
				Text: "", Image: nil, Start: s.start, End: 0, Total: s.line, Next: s.start + 1,
				OversizedSize: int64(firstLineSize),
			}, nil
		}
	}
	result := readtool.Content{
		Text:          strings.Join(s.selectedLines, ""),
		Image:         nil,
		Start:         s.start,
		End:           s.end,
		Total:         s.line,
		Next:          0,
		OversizedSize: 0,
	}
	if s.end < s.line {
		result.Next = s.end + 1
	}
	return result, nil
}

// needsMoreNoticeSpace reports whether one notice would exceed a result budget.
func (s *textReadState) needsMoreNoticeSpace() bool {
	return len(s.selectedLines) >= textbudget.MaximumLines ||
		s.outputSize+continuationReserveBytes > textbudget.MaximumBytes
}
