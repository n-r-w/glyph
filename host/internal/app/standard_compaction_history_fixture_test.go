//go:build integration

package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/infra/persistence"
	"github.com/n-r-w/glyph/host/internal/infra/persistence/sessionfilesystem"
	sessionstore "github.com/n-r-w/glyph/host/internal/infra/persistence/sessions"
	hostsessions "github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
)

// appendPreparedBranchFixture persists an interrupted tool batch through the ordinary session adapter.
func appendPreparedBranchFixture(
	t *testing.T,
	paths persistence.Paths,
	sessionID, recent string,
	includeExcludedResponses bool,
) {
	t.Helper()
	canonical, err := sessionstore.CanonicalWorkingDirectory("")
	require.NoError(t, err)
	repository := sessionstore.New(filepath.Join(paths.Directory, "sessions"), canonical, sessionfilesystem.New())
	require.NoError(t, repository.Initialize(t.Context()))
	loaded, err := repository.Load(t.Context(), session.ID(sessionID))
	require.NoError(t, err)
	entries := make([]session.Entry, 0, 6)
	for _, id := range []string{
		"excluded-failed", "excluded-aborted", "calls", "intervening", "stored-result", "recent-source",
	} {
		entries = append(
			entries,
			session.Entry{
				ID:               id,
				ParentID:         mo.None[string](),
				CreatedAt:        time.Time{},
				Information:      mo.None[session.Information](),
				User:             mo.None[session.UserMessage](),
				Model:            mo.None[session.ModelResponse](),
				ToolResult:       mo.None[session.ToolResult](),
				Extension:        mo.None[session.ExtensionEnvelope](),
				ExtensionMessage: mo.None[session.ExtensionMessage](),
				EstimatedCost:    mo.None[session.EstimatedCost](),
				BranchSummary:    mo.None[session.BranchSummaryEntry](),
				Compaction:       mo.None[session.CompactionEntry](),
			},
		)
	}
	for index, outcome := range []model.Outcome{model.OutcomeFailed, model.OutcomeAborted} {
		response := preparedFixtureResponse(outcome)
		response.Content = []model.Content{
			{
				Kind:            model.ContentText,
				Text:            mo.Some(entries[index].ID + strings.Repeat("x", 3000)),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall:        mo.None[model.ToolCall](),
			},
		}
		entries[index].Model = mo.Some(response)
	}
	response := preparedFixtureResponse(model.OutcomeToolUse)
	for _, id := range []string{"source-a", "source-b"} {
		response.Content = append(
			response.Content,
			model.Content{
				Kind:            model.ContentToolCall,
				Text:            mo.None[string](),
				Final:           true,
				ProviderContext: mo.None[model.ProviderContext](),
				ToolCall: mo.Some(
					model.ToolCall{ID: id, Name: "fixture-tool", Arguments: testToolCallArguments(`{}`)},
				),
			},
		)
	}
	entries[2].Model = mo.Some(response)
	entries[3].ExtensionMessage = mo.Some(
		session.ExtensionMessage{
			ExtensionID: "fixture",
			EntryType:   "during-tool",
			Text:        "intervening source",
			Visibility:  session.ClientVisibilityVisible,
		},
	)
	entries[4].ToolResult = mo.Some(
		agent.ToolResult{
			CallID:   "source-a",
			ToolName: "fixture-tool",
			Contents: tool.TextContents("stored source " + strings.Repeat("r", 600)),
			IsError:  false,
		},
	)
	entries[5].User = mo.Some(model.TextMessage(recent))
	if !includeExcludedResponses {
		entries = entries[2:]
	}
	parent := loaded.Tree.ActiveLeafID()
	created := loaded.Tree.Entries()[len(loaded.Tree.Entries())-1].CreatedAt
	for index := range entries {
		entries[index].ParentID = parent
		entries[index].CreatedAt = created.Add(time.Duration(index+1) * time.Second)
		result, applyErr := repository.Apply(
			t.Context(),
			hostsessions.ApplyCommand{
				Header:      loaded.Header,
				StoragePath: loaded.StoragePath,
				Mutation: hostsessions.Mutation{
					Entry:              mo.Some(entries[index]),
					Navigation:         mo.None[hostsessions.NavigationMutation](),
					Label:              mo.None[hostsessions.LabelMutation](),
					SessionInformation: mo.None[hostsessions.SessionInformationMutation](),
				},
			},
		)
		require.NoError(t, applyErr)
		loaded.StoragePath = result.StoragePath
		parent = mo.Some(entries[index].ID)
	}
}

// preparedFixtureResponse supplies persisted provider-neutral terminal metadata.
func preparedFixtureResponse(outcome model.Outcome) model.Response {
	return model.Response{
		Content:       nil,
		Outcome:       mo.Some(outcome),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
}
