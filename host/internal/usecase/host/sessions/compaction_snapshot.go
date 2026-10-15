package sessions

import (
	"errors"
	"time"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
)

// ephemeralCompactionID identifies the temporary marker used only while projecting compacted history.
const ephemeralCompactionID = "__glyph_compaction_projection__"

// projectCompactionHistory validates one captured branch and projects a candidate replacement.
func projectCompactionHistory(
	entries []session.Entry,
	leaf mo.Option[string],
	compaction session.CompactionEntry,
) ([]agent.HistoryEntry, error) {
	if len(entries) == 0 {
		return nil, errors.New("captured active branch is empty")
	}
	if err := validateCompactionPayload(compaction); err != nil {
		return nil, err
	}
	owned := cloneEntries(entries)
	tree, err := session.NewTree(owned, leaf, nil)
	if err != nil {
		return nil, err
	}
	if boundaryErr := tree.ValidateCompactionBoundary(compaction.FirstKeptEntryID, leaf); boundaryErr != nil {
		return nil, boundaryErr
	}
	leafID := owned[len(owned)-1].ID
	owned = append(owned, session.Entry{
		ID: ephemeralCompactionID, ParentID: mo.Some(leafID), CreatedAt: time.Time{},
		Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
		Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
		Extension: mo.None[session.ExtensionEnvelope](), ExtensionMessage: mo.None[session.ExtensionMessage](),
		EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.BranchSummaryEntry](),
		Compaction: mo.Some(compaction.Clone()),
	})
	return agentrun.ProjectHistory(compactedHistoryFromEntries(owned)), nil
}
