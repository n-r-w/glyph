package sessions

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestActiveStatisticsReturnsAvailableZeroTokensForEmptySession verifies empty-session token completeness.
func TestActiveStatisticsReturnsAvailableZeroTokensForEmptySession(t *testing.T) {
	t.Parallel()

	// Arrange an active session with no durable entries.
	service := serviceWithEntries(nil)

	// Act by deriving statistics from the active snapshot.
	statistics := service.ActiveStatistics()

	// Assert all counts and available token totals are zero.
	assert.Equal(t, session.Statistics{
		UserMessages: 0, ModelResponses: 0, ToolCalls: 0, ToolResults: 0, TotalMessages: 0,
		TokenUsage: mo.Some(session.TokenUsage{}),
	}, statistics)
}

// TestActiveStatisticsCountsTerminalEntriesAndCompleteUsage verifies counts and normalized token aggregation.
func TestActiveStatisticsCountsTerminalEntriesAndCompleteUsage(t *testing.T) {
	t.Parallel()

	// Arrange public entries, excluded entries, a failed model response, and two finalized tool calls.
	usage := model.Usage{
		InputTokens: 1, OutputTokens: 4, CachedInputTokens: 2,
		CacheWriteTokens: 3, ReasoningTokens: 2, TotalTokens: 10,
	}
	entries := []session.Entry{
		testStatisticsUserEntry(),
		testStatisticsModelEntry(model.OutcomeFailed, mo.Some(usage), 2),
		testStatisticsToolResultEntry(),
		{
			ID: "information", CreatedAt: time.Time{}, Information: mo.Some(session.Information{Name: "name"}),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		},
		{
			ID: "extension", CreatedAt: time.Time{}, Information: mo.None[session.Information](),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.Some(session.ExtensionEnvelope{
				ExtensionID: "extension", EntryType: "event", Data: []byte(`{}`),
			}),
		},
	}
	service := serviceWithEntries(entries)

	// Act by deriving statistics from all durable entry kinds.
	statistics := service.ActiveStatistics()

	// Assert counts exclude information and extension entries and reasoning is not added twice.
	assert.Equal(t, session.Statistics{
		UserMessages: 1, ModelResponses: 1, ToolCalls: 2, ToolResults: 1, TotalMessages: 3,
		TokenUsage: mo.Some(session.TokenUsage{
			InputTokens: 1, OutputTokens: 4, CacheReadTokens: 2,
			CacheWriteTokens: 3, ReasoningTokens: 2, TotalTokens: 10,
		}),
	}, statistics)
}

// TestStoredSummaryAndActiveStatisticsShareMessageCounts verifies both views apply one durable-entry count rule.
func TestStoredSummaryAndActiveStatisticsShareMessageCounts(t *testing.T) {
	t.Parallel()

	// Arrange one user, failed model, tool result, information, and extension entry.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	entries := []session.Entry{
		testStatisticsUserEntry(),
		testStatisticsModelEntry(model.OutcomeFailed, mo.None[model.Usage](), 0),
		testStatisticsToolResultEntry(),
		{
			ID: "information", CreatedAt: time.Time{}, Information: mo.Some(session.Information{Name: "name"}),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
		},
		{
			ID: "extension", CreatedAt: time.Time{}, Information: mo.None[session.Information](),
			User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.Some(session.ExtensionEnvelope{
				ExtensionID: "extension", EntryType: "event", Data: []byte(`{}`),
			}),
		},
	}
	loaded := LoadedSession{
		Header: session.Header{
			Version: 1, ID: "stored", CreatedAt: time.Time{}, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/stored.jsonl", Entries: entries,
	}
	repository.EXPECT().List(gomock.Any()).Return([]LoadedSession{loaded}, nil)
	service := New(repository, nil, nil, "/project")
	service.active = loaded

	// Act by reading the stored summary and active statistics for the same entries.
	listed, err := service.ListStored(t.Context())
	require.NoError(t, err)
	statistics := service.ActiveStatistics()

	// Assert both views count only the three public message entries.
	require.Len(t, listed, 1)
	assert.Equal(t, mo.Some("request"), listed[0].FirstUserText)
	assert.Equal(t, statistics.TotalMessages, listed[0].TotalMessages)
	assert.Equal(t, 1, statistics.UserMessages)
	assert.Equal(t, 1, statistics.ModelResponses)
	assert.Equal(t, 1, statistics.ToolResults)
	assert.Equal(t, 3, statistics.TotalMessages)
}

// TestActiveInformationWaitsForDurableAppendAndReturnsCommittedSnapshot verifies a bounded writer-lock observation.
func TestActiveInformationWaitsForDurableAppendAndReturnsCommittedSnapshot(t *testing.T) {
	t.Parallel()

	// Arrange a real user append whose repository write blocks while the service write lock is held.
	controller := gomock.NewController(t)
	repository := NewMockRepository(controller)
	ids := NewMockIDGenerator(controller)
	clock := NewMockClock(controller)
	createdAt := time.Date(2026, 8, 27, 9, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Minute)
	repositoryReached := make(chan struct{})
	releaseRepository := make(chan struct{})
	ids.EXPECT().NewID().Return("user-entry", nil)
	clock.EXPECT().Now().Return(updatedAt)
	repository.EXPECT().Append(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, command AppendCommand) (AppendResult, error) {
			assert.Equal(t, "request", command.Entry.User.MustGet().Content[0].Text.MustGet())
			close(repositoryReached)
			<-releaseRepository
			return AppendResult{StoragePath: "/sessions/active-updated.jsonl"}, nil
		},
	)
	service := New(repository, ids, clock, "/project")
	service.active = LoadedSession{
		Header: session.Header{
			Version: 1, ID: "active", CreatedAt: createdAt, WorkingDirectory: "/project",
		},
		StoragePath: "/sessions/active.jsonl", Entries: nil,
	}
	appendDone := make(chan error, 1)
	informationStarted := make(chan struct{})
	informationDone := make(chan session.InformationSnapshot, 1)

	// Act by blocking the writer in persistence, then starting the public information query.
	go func() {
		appendDone <- service.Append(t.Context(), agent.HistoryEntry{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("request")),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		})
	}()
	<-repositoryReached
	go func() {
		close(informationStarted)
		informationDone <- service.ActiveInformation()
	}()
	<-informationStarted
	snapshot := session.InformationSnapshot{}
	completedEarly := false
	// Assert the query does not complete during the bounded observation before repository release.
	select {
	case snapshot = <-informationDone:
		completedEarly = true
		assert.Fail(t, "information query completed before durable append release")
	case <-time.After(25 * time.Millisecond):
	}
	close(releaseRepository)
	require.NoError(t, <-appendDone)
	if !completedEarly {
		snapshot = <-informationDone
	}

	// Assert metadata and statistics both contain the committed durable user entry.
	assert.Equal(t, updatedAt, snapshot.Info.UpdatedAt)
	assert.Equal(t, mo.Some("/sessions/active-updated.jsonl"), snapshot.Info.StoragePath)
	assert.Equal(t, session.Statistics{
		UserMessages: 1, ModelResponses: 0, ToolCalls: 0, ToolResults: 0, TotalMessages: 1,
		TokenUsage: mo.Some(session.TokenUsage{}),
	}, snapshot.Statistics)
}

// TestActiveStatisticsMakesTokensUnavailableWhenAnyModelUsageIsAbsent verifies aggregate completeness.
func TestActiveStatisticsMakesTokensUnavailableWhenAnyModelUsageIsAbsent(t *testing.T) {
	t.Parallel()

	// Arrange one model response with usage and one model response without usage.
	entries := []session.Entry{
		testStatisticsModelEntry(model.OutcomeStop, mo.Some(model.Usage{}), 0),
		testStatisticsModelEntry(model.OutcomeFailed, mo.None[model.Usage](), 0),
	}
	service := serviceWithEntries(entries)

	// Act by deriving statistics from the mixed-availability entries.
	statistics := service.ActiveStatistics()

	// Assert token totals are unavailable while model and message counts remain available.
	assert.Equal(t, 2, statistics.ModelResponses)
	assert.Equal(t, 2, statistics.TotalMessages)
	assert.True(t, statistics.TokenUsage.IsNone())
}

// TestActiveStatisticsKeepsPresentZeroUsageAvailable verifies zero is not treated as absence.
func TestActiveStatisticsKeepsPresentZeroUsageAvailable(t *testing.T) {
	t.Parallel()

	// Arrange one terminal model response with explicitly present zero usage.
	service := serviceWithEntries([]session.Entry{
		testStatisticsModelEntry(model.OutcomeStop, mo.Some(model.Usage{}), 0),
	})

	// Act by deriving statistics from the active entries.
	statistics := service.ActiveStatistics()

	// Assert present-zero usage remains available with a counted model response.
	assert.Equal(t, 1, statistics.ModelResponses)
	assert.Equal(t, 1, statistics.TotalMessages)
	assert.Equal(t, session.TokenUsage{}, statistics.TokenUsage.OrEmpty())
	assert.True(t, statistics.TokenUsage.IsSome())
}

func serviceWithEntries(entries []session.Entry) *Service {
	service := New(nil, nil, nil, "")
	service.active = LoadedSession{
		Header: session.Header{}, StoragePath: "", Entries: entries,
	}
	return service
}

func testStatisticsUserEntry() session.Entry {
	return session.Entry{
		ID: "user", CreatedAt: time.Time{}, Information: mo.None[session.Information](),
		User: mo.Some(model.TextMessage("request")), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
	}
}

func testStatisticsModelEntry(outcome model.Outcome, usage mo.Option[model.Usage], toolCalls int) session.Entry {
	content := make([]model.Content, 0, toolCalls)
	for index := range toolCalls {
		content = append(content, model.Content{
			Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(model.ToolCall{
				ID: "call", Name: "tool", Arguments: map[string]any{"index": index},
			}),
		})
	}
	return session.Entry{
		ID: "model", CreatedAt: time.Time{}, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.Some(model.Response{
			Content: content, Outcome: mo.Some(outcome), ErrorMessage: mo.None[string](),
			Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](),
			ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: usage, Diagnostics: nil,
		}),
		ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
	}
}

func testStatisticsToolResultEntry() session.Entry {
	return session.Entry{
		ID: "tool", CreatedAt: time.Time{}, Information: mo.None[session.Information](),
		User: mo.None[session.UserMessage](), Model: mo.None[session.ModelResponse](),
		ToolResult: mo.Some(agent.ToolResult{
			CallID: "call", ToolName: "tool", Contents: nil, IsError: false,
		}),
		Extension: mo.None[session.ExtensionEnvelope](),
	}
}
