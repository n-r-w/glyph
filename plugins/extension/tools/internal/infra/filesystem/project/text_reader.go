package project

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
	readtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/read"
)

// textReadState tracks bounded text accumulation across reader fragments.
type textReadState struct {
	// start is the first requested one-based line.
	start uint
	// maxLines is the maximum number of selected lines.
	maxLines uint
	// line is the current one-based source line.
	line uint
	// selected counts selected complete lines.
	selected uint
	// end is the last selected one-based line.
	end uint
	// lineSize is the current source line byte count.
	lineSize int
	// outputSize is the selected text byte count.
	outputSize int
	// stopped reports whether the output budget was reached.
	stopped bool
	// oversizedSize contains one omitted oversized line byte count.
	oversizedSize int64
	// selectedLines contains complete bounded output lines.
	selectedLines []string
	// lineBuffer accumulates the current source line.
	lineBuffer strings.Builder
}

// newTextReadState initializes a bounded line-selection state.
func newTextReadState(offset, limit mo.Option[uint]) *textReadState {
	start := offset.OrElse(1)
	maxLines := min(limit.OrElse(uint(textbudget.MaximumLines)), uint(textbudget.MaximumLines))
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
	offset, limit mo.Option[uint],
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
			Text: mo.Some(""), Image: mo.None[readtool.Image](), Start: mo.Some(s.start),
			End: mo.Some(uint(0)), Total: mo.Some(uint(0)), Next: mo.None[uint](), OversizedSize: mo.None[int64](),
		}, nil
	}
	if s.start > s.line {
		return readtool.Content{}, fmt.Errorf("read project file %q: offset %d is beyond end of file", path, s.start)
	}
	if s.oversizedSize > 0 {
		return readtool.Content{
			Text:          mo.Some(""),
			Image:         mo.None[readtool.Image](),
			Start:         mo.Some(s.start),
			End:           mo.None[uint](),
			Total:         mo.Some(s.line),
			Next:          mo.None[uint](),
			OversizedSize: mo.Some(s.oversizedSize),
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
				Text: mo.Some(""), Image: mo.None[readtool.Image](), Start: mo.Some(s.start),
				End: mo.None[uint](), Total: mo.Some(s.line), Next: mo.Some(s.start + 1),
				OversizedSize: mo.Some(int64(firstLineSize)),
			}, nil
		}
	}
	result := readtool.Content{
		Text:          mo.Some(strings.Join(s.selectedLines, "")),
		Image:         mo.None[readtool.Image](),
		Start:         mo.Some(s.start),
		End:           mo.Some(s.end),
		Total:         mo.Some(s.line),
		Next:          mo.None[uint](),
		OversizedSize: mo.None[int64](),
	}
	if s.end < s.line {
		result.Next = mo.Some(s.end + 1)
	}
	return result, nil
}

// needsMoreNoticeSpace reports whether one notice would exceed a result budget.
func (s *textReadState) needsMoreNoticeSpace() bool {
	return len(s.selectedLines) >= textbudget.MaximumLines ||
		s.outputSize+continuationReserveBytes > textbudget.MaximumBytes
}
