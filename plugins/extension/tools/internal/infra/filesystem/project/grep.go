package project

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/n-r-w/glyph/plugins/extension/tools/internal/core/textbudget"
	searchtool "github.com/n-r-w/glyph/plugins/extension/tools/internal/usecase/tools/search"
)

// grepSearch holds bounded state shared across one project traversal.
type grepSearch struct {
	// ctx controls the search traversal.
	ctx context.Context
	// command contains validated grep options.
	command searchtool.GrepCommand
	// matcher evaluates each source line.
	matcher *regexp.Regexp
	// root is the canonical project root.
	root string
	// limit is the maximum number of matches.
	limit uint
	// contextLines is the number of surrounding lines to include.
	contextLines uint
	// output accumulates bounded model-visible lines.
	output *searchOutput
	// matches counts recorded matches.
	matches uint
	// limited reports whether the match limit was reached.
	limited bool
	// longLine reports whether any source line was shortened.
	longLine bool
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
	limit := cmd.Limit.OrElse(grepDefaultLimit)
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
	// ctx controls source reading.
	ctx context.Context
	// reader provides buffered source runes.
	reader *bufio.Reader
	// shown retains the bounded displayed line prefix.
	shown strings.Builder
	// ended reports whether the source line ended.
	ended bool
	// seen reports whether any rune was read.
	seen bool
	// long reports whether the displayed line was shortened.
	long bool
	// sourceErr retains a source read failure.
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
	// ctx controls source reading.
	ctx context.Context
	// path identifies the displayed project-relative file.
	path string
	// reader provides buffered source bytes.
	reader *bufio.Reader
	// matcher evaluates each source line.
	matcher *regexp.Regexp
	// contextLines is the number of surrounding lines to include.
	contextLines uint
	// remaining is the available match count.
	remaining uint
	// output accumulates bounded model-visible lines.
	output *searchOutput
	// previous retains bounded lines before the current match.
	previous []string
	// lineNumber identifies the current source line.
	lineNumber uint
	// after counts remaining lines after a match.
	after uint
	// matches counts recorded matches in this source.
	matches uint
	// longLine reports whether any source line was shortened.
	longLine bool
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
		if appendErr := s.output.appendLine(s.path, formatted); appendErr != nil {
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
	if appendErr := s.output.appendLine(s.path, formatted); appendErr != nil {
		return false, appendErr
	}
	s.remember(formatted)
	return false, nil
}

// appendPrevious emits the bounded lines retained before a match.
func (s *grepReaderState) appendPrevious() error {
	for _, prior := range s.previous {
		if appendErr := s.output.appendLine(s.path, prior); appendErr != nil {
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

// appendLine formats and appends one complete grep output line.
func (output *searchOutput) appendLine(path, line string) error {
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
