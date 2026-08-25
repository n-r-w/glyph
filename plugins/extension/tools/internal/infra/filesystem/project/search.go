package project

import (
	"bufio"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

const (
	grepLineCharacters = 500
	directoryBatchSize = 128
)

var _ searchtool.ProjectFiles = (*Service)(nil)

// searchOutput retains complete output lines within the shared text budget.
type searchOutput struct {
	lines     []string
	bytes     int
	truncated bool
}

// newSearchOutput creates an empty bounded output accumulator.
func newSearchOutput() *searchOutput { return &searchOutput{lines: nil, bytes: 0, truncated: false} }

// add appends one complete line or records that the output budget was reached.
func (o *searchOutput) add(line string) {
	if o.truncated || len(o.lines) == textbudget.MaximumLines || o.bytes+len(line) > textbudget.MaximumBytes {
		o.truncated = true
		return
	}
	o.lines = append(o.lines, line)
	o.bytes += len(line)
}

// notice reserves room by removing trailing output before appending one complete notice.
func (o *searchOutput) notice(line string) {
	for len(o.lines) > 0 && (len(o.lines) == textbudget.MaximumLines || o.bytes+len(line) > textbudget.MaximumBytes) {
		last := len(o.lines) - 1
		o.bytes -= len(o.lines[last])
		o.lines = o.lines[:last]
	}
	if len(o.lines) < textbudget.MaximumLines && o.bytes+len(line) <= textbudget.MaximumBytes {
		o.lines = append(o.lines, line)
		o.bytes += len(line)
	}
}

// text joins the retained complete lines and notices.
func (o *searchOutput) text() string { return strings.Join(o.lines, "") }

// walkProject visits directories in bounded batches and never follows symbolic links.
func walkProject(ctx context.Context, root string, visit func(string, fs.DirEntry) error) error {
	info, statErr := os.Lstat(root)
	if statErr != nil {
		return statErr
	}
	entry := fs.FileInfoToDirEntry(info)
	if visitErr := visit(root, entry); visitErr != nil {
		return visitErr
	}
	if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
		return nil
	}
	return walkDirectory(ctx, root, visit)
}

// walkDirectory reads and visits one directory at a time in bounded entry batches.
func walkDirectory(ctx context.Context, path string, visit func(string, fs.DirEntry) error) error {
	// #nosec G304 -- traversal supplies project paths.
	dir, openErr := os.Open(path)
	if openErr != nil {
		return openErr
	}
	defer func() { _ = dir.Close() }()
	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		entries, readErr := dir.ReadDir(directoryBatchSize)
		slices.SortFunc(entries, func(left, right fs.DirEntry) int {
			return cmp.Compare(left.Name(), right.Name())
		})
		for _, entry := range entries {
			child := filepath.Join(path, entry.Name())
			if visitErr := visit(child, entry); visitErr != nil {
				return visitErr
			}
			if entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
				if walkErr := walkDirectory(ctx, child, visit); walkErr != nil {
					return walkErr
				}
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// grepSearch holds bounded state shared across one project traversal.
type grepSearch struct {
	ctx          context.Context
	command      searchtool.GrepCommand
	matcher      *regexp.Regexp
	root         string
	limit        uint
	contextLines uint
	output       *searchOutput
	matches      uint
	limited      bool
	longLine     bool
}

// Grep searches regular files without following symbolic links.
func (s *Service) Grep(ctx context.Context, cmd searchtool.GrepCommand) (searchtool.GrepResult, error) {
	search, createErr := newGrepSearch(ctx, cmd)
	if createErr != nil {
		return searchtool.GrepResult{}, createErr
	}
	walkErr := walkProject(ctx, search.root, search.visit)
	if walkErr != nil && !errors.Is(walkErr, io.EOF) {
		return searchtool.GrepResult{}, fmt.Errorf("grep project files: %w", walkErr)
	}
	return search.result(), nil
}

// newGrepSearch validates input and applies bounded grep defaults.
func newGrepSearch(ctx context.Context, cmd searchtool.GrepCommand) (*grepSearch, error) {
	pattern := cmd.Pattern
	if cmd.Literal {
		pattern = regexp.QuoteMeta(pattern)
	}
	if cmd.IgnoreCase {
		pattern = "(?i)" + pattern
	}
	matcher, compileErr := regexp.Compile(pattern)
	if compileErr != nil {
		return nil, fmt.Errorf("compile grep pattern: %w", compileErr)
	}
	if cmd.Glob != "" && !doublestar.ValidatePattern(filepath.ToSlash(cmd.Glob)) {
		return nil, errors.New("invalid grep glob")
	}
	root := cmd.Path
	if root == "" {
		root = "."
	}
	limit := cmd.Limit
	if limit == 0 {
		limit = 100
	}
	contextLines := min(cmd.Context, uint(textbudget.MaximumLines-1))
	return &grepSearch{
		ctx: ctx, command: cmd, matcher: matcher, root: root, limit: limit,
		contextLines: contextLines, output: newSearchOutput(), matches: 0,
		limited: false, longLine: false,
	}, nil
}

// visit searches one regular non-symbolic-link file selected by the traversal.
func (g *grepSearch) visit(path string, entry fs.DirEntry) error {
	if entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 {
		return nil
	}
	included, matchErr := g.includes(path)
	if matchErr != nil {
		return matchErr
	}
	if !included {
		return nil
	}
	count, fileLong, more, grepErr := grepFile(
		g.ctx, path, g.matcher, g.contextLines, g.limit-g.matches, g.output,
	)
	if grepErr != nil {
		return grepErr
	}
	g.matches += count
	g.longLine = g.longLine || fileLong
	if more {
		g.limited = true
		return io.EOF
	}
	if g.output.truncated {
		return io.EOF
	}
	return nil
}

// includes applies the optional project-relative glob to one traversed path.
func (g *grepSearch) includes(path string) (bool, error) {
	if g.command.Glob == "" {
		return true, nil
	}
	relative, relativeErr := projectRelative(path)
	if relativeErr != nil {
		return false, relativeErr
	}
	matched, matchErr := doublestar.Match(filepath.ToSlash(g.command.Glob), filepath.ToSlash(relative))
	if matchErr != nil {
		return false, fmt.Errorf("match grep glob: %w", matchErr)
	}
	return matched, nil
}

// result appends applicable notices and returns the complete bounded result.
func (g *grepSearch) result() searchtool.GrepResult {
	if g.limited {
		g.output.notice("[Match limit reached.]\n")
	}
	if g.output.truncated {
		g.output.notice("[Output limit reached.]\n")
	}
	if g.longLine {
		g.output.notice("[Long lines were truncated to 500 characters.]\n")
	}
	return searchtool.GrepResult{Text: g.output.text()}
}

// boundedLine is an io.RuneReader that exposes one line without storing it.
type boundedLine struct {
	ctx       context.Context
	reader    *bufio.Reader
	shown     strings.Builder
	ended     bool
	seen      bool
	long      bool
	sourceErr error
}

// ReadRune returns one rune while retaining only the displayed line prefix.
func (l *boundedLine) ReadRune() (r rune, size int, err error) {
	if ctxErr := l.ctx.Err(); ctxErr != nil {
		return 0, 0, ctxErr
	}
	if l.ended {
		return 0, 0, io.EOF
	}
	r, size, err = l.reader.ReadRune()
	if err != nil {
		l.ended = true
		if !errors.Is(err, io.EOF) {
			l.sourceErr = err
		}
		return 0, 0, err
	}
	l.seen = true
	if r == '\n' {
		l.ended = true
		return 0, 0, io.EOF
	}
	if l.shown.Len() < grepLineCharacters*utf8.UTFMax {
		if utf8.RuneCountInString(l.shown.String()) < grepLineCharacters {
			l.shown.WriteRune(r)
		} else {
			l.long = true
		}
	} else {
		l.long = true
	}
	return r, size, nil
}

// drain consumes the remainder of one line and preserves cancellation or source errors.
func (l *boundedLine) drain() error {
	if l.sourceErr != nil {
		return l.sourceErr
	}
	for {
		_, _, err := l.ReadRune()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

// grepFile opens one traversed file and delegates bounded line processing.
func grepFile(
	ctx context.Context,
	path string,
	matcher *regexp.Regexp,
	contextLines, remaining uint,
	output *searchOutput,
) (matches uint, longLine, more bool, readErr error) {
	file, openErr := os.Open(path) // #nosec G304 -- traversal supplies project paths.
	if openErr != nil {
		return 0, false, false, openErr
	}
	defer func() { _ = file.Close() }()
	return grepReader(ctx, path, file, matcher, contextLines, remaining, output)
}

// grepReaderState retains bounded context while processing one source.
type grepReaderState struct {
	ctx          context.Context
	path         string
	reader       *bufio.Reader
	matcher      *regexp.Regexp
	contextLines uint
	remaining    uint
	output       *searchOutput
	previous     []string
	lineNumber   uint
	after        uint
	matches      uint
	longLine     bool
}

// grepReader processes one source without retaining an unbounded line.
func grepReader(
	ctx context.Context,
	path string,
	source io.Reader,
	matcher *regexp.Regexp,
	contextLines, remaining uint,
	output *searchOutput,
) (matches uint, longLine, more bool, readErr error) {
	state := &grepReaderState{
		ctx: ctx, path: path, reader: bufio.NewReader(source), matcher: matcher,
		contextLines: contextLines, remaining: remaining, output: output,
		previous: nil, lineNumber: 0, after: 0, matches: 0, longLine: false,
	}
	for {
		done, foundMore, lineErr := state.readLine()
		if lineErr != nil {
			return 0, false, false, lineErr
		}
		if foundMore {
			return state.matches, state.longLine, true, nil
		}
		if done || output.truncated {
			return state.matches, state.longLine, false, nil
		}
	}
}

// readLine matches, drains, and records one complete source line.
func (s *grepReaderState) readLine() (done, more bool, readErr error) {
	if contextErr := s.ctx.Err(); contextErr != nil {
		return false, false, contextErr
	}
	line := &boundedLine{
		ctx: s.ctx, reader: s.reader, shown: strings.Builder{}, ended: false,
		seen: false, long: false, sourceErr: nil,
	}
	matched := s.matcher.FindReaderIndex(line) != nil
	if drainErr := line.drain(); drainErr != nil {
		return false, false, drainErr
	}
	if contextErr := s.ctx.Err(); contextErr != nil {
		return false, false, contextErr
	}
	if !line.seen {
		return true, false, nil
	}
	s.lineNumber++
	s.longLine = s.longLine || line.long
	formatted := fmt.Sprintf("%d:%s", s.lineNumber, line.shown.String())
	foundMore, recordErr := s.record(formatted, matched)
	return false, foundMore, recordErr
}

// record appends one matched or contextual line and updates bounded context state.
func (s *grepReaderState) record(formatted string, matched bool) (more bool, recordErr error) {
	if matched {
		return s.recordMatch(formatted)
	}
	if s.after > 0 {
		if appendErr := appendGrepLine(s.output, s.path, formatted); appendErr != nil {
			return false, appendErr
		}
		s.after--
	}
	s.remember(formatted)
	return false, nil
}

// recordMatch emits retained context and one match unless the match limit was already reached.
func (s *grepReaderState) recordMatch(formatted string) (more bool, recordErr error) {
	if s.matches == s.remaining {
		return true, nil
	}
	if appendErr := s.appendPrevious(); appendErr != nil {
		return false, appendErr
	}
	s.matches++
	s.after = s.contextLines
	if appendErr := appendGrepLine(s.output, s.path, formatted); appendErr != nil {
		return false, appendErr
	}
	s.remember(formatted)
	return false, nil
}

// appendPrevious emits the bounded lines retained before a match.
func (s *grepReaderState) appendPrevious() error {
	for _, prior := range s.previous {
		if appendErr := appendGrepLine(s.output, s.path, prior); appendErr != nil {
			return appendErr
		}
	}
	return nil
}

// remember retains at most the requested number of lines before the next match.
func (s *grepReaderState) remember(formatted string) {
	if s.contextLines == 0 {
		return
	}
	s.previous = append(s.previous, formatted)
	if uint(len(s.previous)) > s.contextLines {
		s.previous = s.previous[1:]
	}
}

// appendGrepLine formats and appends one complete grep output line.
func appendGrepLine(output *searchOutput, path, line string) error {
	formatted, formatErr := formatGrepLine(path, line)
	if formatErr != nil {
		return formatErr
	}
	output.add(formatted)
	return nil
}

// formatGrepLine prefixes one displayed line with its escaped project-relative path.
func formatGrepLine(path, line string) (string, error) {
	relative, relativeErr := projectRelative(path)
	if relativeErr != nil {
		return "", relativeErr
	}
	return escapeDisplayedName(relative) + ":" + line + "\n", nil
}

// Find returns matching files and symbolic links without entering linked directories.
func (s *Service) Find(ctx context.Context, cmd searchtool.FindCommand) (searchtool.FindResult, error) {
	if !doublestar.ValidatePattern(filepath.ToSlash(cmd.Pattern)) {
		return searchtool.FindResult{}, errors.New("invalid find glob")
	}
	root := cmd.Path
	if root == "" {
		root = "."
	}
	limit := cmd.Limit
	if limit == 0 {
		limit = 1000
	}
	output := newSearchOutput()
	count, limited := uint(0), false
	walkErr := walkProject(ctx, root, func(path string, entry fs.DirEntry) error {
		if entry.IsDir() {
			return nil
		}
		relative, relativeErr := projectRelative(path)
		if relativeErr != nil {
			return relativeErr
		}
		matched, matchErr := doublestar.Match(filepath.ToSlash(cmd.Pattern), filepath.ToSlash(relative))
		if matchErr != nil {
			return fmt.Errorf("match find glob: %w", matchErr)
		}
		if !matched {
			return nil
		}
		if count == limit {
			limited = true
			return io.EOF
		}
		count++
		output.add(escapeDisplayedName(relative) + "\n")
		if output.truncated {
			return io.EOF
		}
		return nil
	})
	if walkErr != nil && !errors.Is(walkErr, io.EOF) {
		return searchtool.FindResult{}, fmt.Errorf("find project files: %w", walkErr)
	}
	if limited {
		output.notice("[Result limit reached.]\n")
	}
	if output.truncated {
		output.notice("[Output limit reached.]\n")
	}
	return searchtool.FindResult{Text: output.text()}, nil
}

// List returns direct directory entries in bounded batches.
func (s *Service) List(ctx context.Context, cmd searchtool.ListCommand) (searchtool.ListResult, error) {
	path := cmd.Path
	if path == "" {
		path = "."
	}
	limit := cmd.Limit
	if limit == 0 {
		limit = 500
	}
	output := newSearchOutput()
	// #nosec G304 -- the caller selects a project directory.
	dir, openErr := os.Open(path)
	if openErr != nil {
		return searchtool.ListResult{}, fmt.Errorf("list project directory: %w", openErr)
	}
	defer func() { _ = dir.Close() }()
	count := uint(0)
	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return searchtool.ListResult{}, contextErr
		}
		entries, readErr := dir.ReadDir(directoryBatchSize)
		slices.SortFunc(entries, func(left, right fs.DirEntry) int {
			return cmp.Compare(left.Name(), right.Name())
		})
		for _, entry := range entries {
			if count == limit {
				output.notice("[Entry limit reached.]\n")
				return searchtool.ListResult{Text: output.text()}, nil
			}
			count++
			name := entry.Name()
			info, statErr := os.Stat(filepath.Join(path, name))
			if statErr == nil && info.IsDir() {
				name += "/"
			} else if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
				return searchtool.ListResult{}, statErr
			}
			output.add(escapeDisplayedName(name) + "\n")
			if output.truncated {
				output.notice("[Output limit reached.]\n")
				return searchtool.ListResult{Text: output.text()}, nil
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return searchtool.ListResult{}, readErr
		}
	}
	return searchtool.ListResult{Text: output.text()}, nil
}

// escapeDisplayedName keeps one filesystem name on one model-visible output line.
func escapeDisplayedName(name string) string {
	name = strings.ReplaceAll(name, "\r", `\r`)
	return strings.ReplaceAll(name, "\n", `\n`)
}

// projectRelative converts a filesystem path to a slash-separated project-relative path.
func projectRelative(path string) (string, error) {
	rel, err := filepath.Rel(".", path)
	return filepath.ToSlash(rel), err
}
