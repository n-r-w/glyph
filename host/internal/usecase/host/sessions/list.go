package sessions

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/samber/lo"
	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// ListUISessions projects validated stored sessions into the UI list contract.
func (s *Service) ListUISessions(ctx context.Context) ([]ui.StoredSession, error) {
	loaded, err := s.repository.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	// Derive metadata once per session rather than scanning trees during each sort comparison.
	result := lo.Map(loaded, func(item LoadedSession, _ int) ui.StoredSession { return item.uiListItem() })
	sort.Slice(result, func(left, right int) bool { return sessionInfoBefore(result[left].Info, result[right].Info) })
	return result, nil
}

// ListProgrammaticSessions projects validated stored sessions into the Programmatic list contract.
func (s *Service) ListProgrammaticSessions(ctx context.Context) ([]programmatic.StoredSession, error) {
	loaded, err := s.repository.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	// Each row owns its metadata and exact selected text before client preview normalization.
	result := lo.Map(loaded, func(item LoadedSession, _ int) programmatic.StoredSession {
		return item.programmaticListItem()
	})
	sort.Slice(result, func(left, right int) bool { return sessionInfoBefore(result[left].Info, result[right].Info) })
	return result, nil
}

// uiListItem derives the UI query facts from one validated stored tree.
func (loaded LoadedSession) uiListItem() ui.StoredSession {
	return ui.StoredSession{
		Info: loaded.Info(), FirstUserText: loaded.firstUserText(),
		TotalMessages: countSessionEntries(loaded.Tree.Entries()).totalMessages,
	}
}

// programmaticListItem derives the Programmatic query facts from one validated stored tree.
func (loaded LoadedSession) programmaticListItem() programmatic.StoredSession {
	return programmatic.StoredSession{
		Info: loaded.Info(), FirstUserText: loaded.firstUserText(),
		TotalMessages: countSessionEntries(loaded.Tree.Entries()).totalMessages,
	}
}

// sessionInfoBefore orders newer sessions first, then uses opaque ID to break timestamp ties.
func sessionInfoBefore(left, right session.Info) bool {
	if left.UpdatedAt.Equal(right.UpdatedAt) {
		return left.ID < right.ID
	}
	return left.UpdatedAt.After(right.UpdatedAt)
}

// firstUserText selects the first user text whose normalized preview is nonempty.
func (loaded LoadedSession) firstUserText() mo.Option[string] {
	branch := loaded.Tree.ActiveBranch()
	for index := range branch {
		if user, present := branch[index].User.Get(); present {
			// Normalize only to skip empty previews. The client receives exact stored text.
			text := strings.TrimSpace(lineBreaks.ReplaceAllString(user.Text(""), " "))
			if text != "" {
				return mo.Some(user.Text(""))
			}
		}
	}
	return mo.None[string]()
}
