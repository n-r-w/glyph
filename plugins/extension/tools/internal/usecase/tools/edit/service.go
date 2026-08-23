// Package edit replaces one uniquely occurring text fragment in a project file.
package edit

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	extensioncontroller "github.com/n-r-w/glyph/plugins/extension/tools/internal/controller/extension"
)

// Service coordinates exact project-file replacement.
type Service struct {
	projectEditor ProjectEditor
}

var _ extensioncontroller.EditTool = (*Service)(nil)

// New creates an edit service backed by project file access.
func New(projectEditor ProjectEditor) *Service { return &Service{projectEditor: projectEditor} }

// overlappingCount returns every exact source occurrence, including overlapping positions.
func overlappingCount(content, source string) int {
	count := 0
	for start := 0; start <= len(content)-len(source); {
		index := strings.Index(content[start:], source)
		if index < 0 {
			break
		}
		count++
		start += index + 1
	}
	return count
}

// Edit applies ordered unique exact replacements through one atomic update.
func (s *Service) Edit(ctx context.Context, path string, replacements []extensioncontroller.Replacement) error {
	if len(replacements) == 0 {
		return errors.New("at least one replacement is required")
	}
	return s.projectEditor.UpdateFile(ctx, path, func(original []byte) ([]byte, error) {
		content := string(original)
		type match struct {
			start int
			end   int
			text  string
		}
		matches := make([]match, 0, len(replacements))
		for _, replacement := range replacements {
			if replacement.OldText == "" || overlappingCount(content, replacement.OldText) != 1 {
				return nil, fmt.Errorf("source fragment must occur exactly once in %q", path)
			}
			start := strings.Index(content, replacement.OldText)
			matches = append(matches, match{start: start, end: start + len(replacement.OldText), text: replacement.NewText})
		}
		sort.Slice(matches, func(left, right int) bool { return matches[left].start > matches[right].start })
		for index := 1; index < len(matches); index++ {
			if matches[index-1].start < matches[index].end {
				return nil, fmt.Errorf("source fragments overlap in %q", path)
			}
		}
		updated := content
		for _, replacement := range matches {
			updated = updated[:replacement.start] + replacement.text + updated[replacement.end:]
		}
		return []byte(updated), nil
	})
}
