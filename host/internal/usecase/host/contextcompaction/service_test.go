//go:build !integration

package contextcompaction

import (
	"errors"
	"testing"

	"github.com/samber/lo"
	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// TestRecordEstimateAppliesApprovedSizingFormula verifies byte rounding and fixed image weight.
func TestRecordEstimateAppliesApprovedSizingFormula(t *testing.T) {
	t.Parallel()

	// Arrange exact record byte and image counts.
	tests := []struct {
		name   string
		bytes  int64
		images int64
		want   int64
	}{
		{name: "four hundred bytes", bytes: 400, images: 0, want: 108},
		{name: "round one record once", bytes: 401, images: 0, want: 109},
		{name: "one image", bytes: 0, images: 1, want: 2056},
		{name: "two images", bytes: 0, images: 2, want: 4104},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Act by estimating one provider-neutral record.
			got := estimateRecord(test.bytes, test.images)

			// Assert the deterministic sizing policy.
			require.Equal(t, test.want, got)
		})
	}
}

// TestFallbackEstimateMeasuresEveryProviderNeutralInputCategory verifies exact included bytes and images.
func TestFallbackEstimateMeasuresEveryProviderNeutralInputCategory(t *testing.T) {
	t.Parallel()

	// Arrange one request containing every approved sizing category.
	request := modelexecution.ProviderRequest{
		Instructions: "é",
		Model: model.Descriptor{
			Provider: "provider", Model: "model", Input: []model.InputModality{model.InputModalityText},
			ContextWindow: 100_000, MaxTokens: 10_000,
			ReasoningCapabilities: model.ReasoningCapabilities{
				Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceLow},
				Default: model.ReasoningChoiceLow,
			},
			ToolCapabilities: model.ToolCapabilities{
				StrictJSONSchema: true, Grammar: model.GrammarCapabilities{Lark: true, Regex: true},
			},
			Pricing: mo.None[model.Pricing](),
		},
		ReasoningChoice: model.ReasoningChoiceLow,
		History: []agent.HistoryEntry{
			{
				Kind: agent.HistoryEntryUser,
				User: mo.Some(model.Message{Content: []model.InputContent{
					{
						Kind:      model.InputContentText,
						Text:      mo.Some("user"),
						MediaType: mo.None[string](),
						Data:      mo.None[[]byte](),
					},
					{
						Kind:      model.InputContentImage,
						Text:      mo.None[string](),
						MediaType: mo.Some("image/png"),
						Data:      mo.Some([]byte("ignored")),
					},
				}}),
				Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
			},
			{
				Kind: agent.HistoryEntryModel, User: mo.None[model.Message](),
				Model: mo.Some(model.Response{
					Content: []model.Content{
						{
							Kind:            model.ContentText,
							Text:            mo.Some("text"),
							Final:           true,
							ProviderContext: mo.None[model.ProviderContext](),
							ToolCall:        mo.None[model.ToolCall](),
						},
						{
							Kind:  model.ContentReasoning,
							Text:  mo.Some("reason"),
							Final: true,
							ProviderContext: mo.Some(model.ProviderContext{
								Source: model.ProviderContextSource{
									ProviderID:       "provider",
									API:              "api",
									Model:            "model",
									CompatibilityKey: mo.Some("key"),
								},
								Payload: []byte("opaque"),
							}),
							ToolCall: mo.None[model.ToolCall](),
						},
						{
							Kind:            model.ContentRefusal,
							Text:            mo.Some("no"),
							Final:           true,
							ProviderContext: mo.None[model.ProviderContext](),
							ToolCall:        mo.None[model.ToolCall](),
						},
						{
							Kind:            model.ContentToolCall,
							Text:            mo.None[string](),
							Final:           true,
							ProviderContext: mo.None[model.ProviderContext](),
							ToolCall: mo.Some(
								model.ToolCall{
									ID:        "call",
									Name:      "tool",
									Arguments: testToolCallArguments(`{ "x":"\u0079", "n":1.00 }`),
								},
							),
						},
					},
					Outcome:       mo.Some(model.OutcomeToolUse),
					ErrorMessage:  mo.None[string](),
					Provider:      mo.Some(model.ProviderID("provider")),
					Model:         mo.Some(model.ID("model")),
					ResponseModel: mo.None[model.ID](),
					ResponseID:    mo.None[string](),
					Usage:         mo.None[model.Usage](),
					Diagnostics:   nil,
				}),
				ToolResult: mo.None[agent.ToolResult](),
			},
			{
				Kind: agent.HistoryEntryToolResult, User: mo.None[model.Message](), Model: mo.None[model.Response](),
				ToolResult: mo.Some(agent.ToolResult{
					CallID: "call", ToolName: "not-counted", IsError: false,
					Contents: []tool.ResultContent{
						{Kind: tool.ResultContentText, Text: mo.Some("result"), Image: mo.None[tool.ResultImage]()},
						{
							Kind:  tool.ResultContentImage,
							Text:  mo.None[string](),
							Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte("ignored")}),
						},
					},
				}),
			},
		},
		Tools: []tool.Descriptor{{
			Name: "tool", Description: "description", InputSchemaJSON: []byte(`{"type":"object"}`),
			ConstrainedSampling: mo.Some(tool.ConstrainedSampling{
				Kind:                 tool.ConstrainedSamplingGrammar,
				JSONSchemaStrictness: mo.Some(tool.JSONSchemaStrictPrefer),
				Grammar: mo.Some(
					tool.GrammarVariants{Lark: mo.Some("start: WORD"), Regex: mo.Some("[a-z]+")},
				),
				GrammarInputProperty: mo.Some("input"),
			}),
		}},
	}
	instructionBytes := int64(len("é"))
	toolBytes := int64(
		len("tool") + len("description") + len(`{"type":"object"}`) + len("start: WORD") + len("[a-z]+") + len("input"),
	)
	userBytes := int64(len("user"))
	argumentBytes := int64(len(`{ "x":"\u0079", "n":1.00 }`))
	modelBytes := int64(len("text")+len("reason")+len("no")+len("call")+len("tool")+len(`opaque`)) + argumentBytes
	resultBytes := int64(len("call") + len("result"))
	want := estimateRecord(instructionBytes, 0) + estimateRecord(toolBytes, 0) +
		estimateRecord(userBytes, 1) + estimateRecord(modelBytes, 0) + estimateRecord(resultBytes, 1)

	// Act by estimating the complete request.
	got, err := fallbackEstimate(request)

	// Assert exact category inclusion, UTF-8 byte counting, and JSON byte measurement.
	require.NoError(t, err)
	require.Equal(t, want, got)
}

// TestNewRequestCarriesHostFallbackEstimateForEveryInputEntry verifies complete compaction input sizing.
func TestNewRequestCarriesHostFallbackEstimateForEveryInputEntry(t *testing.T) {
	t.Parallel()
	// Arrange text, image, model-private replay, tool-result, summary, and model-hidden entries.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	entries := []session.Entry{
		compactionUserEntry("text", mo.None[string](), "text"),
		compactionUserEntry("image", mo.Some("text"), ""),
		compactionMarkerEntry("summary", "image", Result{
			Summary: "previous", FirstKeptEntryID: "text",
			Source: session.CompactionSource{
				ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
			},
			Details: mo.None[[]byte](),
		}),
		compactionUserEntry("tail", mo.Some("summary"), "tail"),
	}
	entries[1].User = mo.Some(model.Message{Content: []model.InputContent{{
		Kind: model.InputContentImage, Text: mo.None[string](), MediaType: mo.Some("image/png"),
		Data: mo.Some([]byte("ignored")),
	}}})
	entries[2].Compaction = mo.None[session.CompactionEntry]()
	entries[2].BranchSummary = mo.Some(session.BranchSummaryEntry{
		Summary: "summary", FirstEntryID: "text", LastEntryID: "image",
		Source: session.BranchSummarySource{
			ExtensionID: mo.Some("extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](),
	})
	modelEntry := compactionUserEntry("model", mo.Some("tail"), "unused")
	modelEntry.User = mo.None[session.UserMessage]()
	modelEntry.Model = mo.Some(model.Response{
		Content: []model.Content{{
			Kind: model.ContentReasoning, Text: mo.Some("model"), Final: true,
			ProviderContext: mo.Some(model.ProviderContext{
				Source: model.ProviderContextSource{
					ProviderID: "provider", API: "api", Model: "model", CompatibilityKey: mo.None[string](),
				},
				Payload: []byte("opaque"),
			}),
			ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](),
		Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	})
	entries = append(entries, modelEntry)
	hidden := compactionUserEntry("hidden", mo.Some("model"), "unused")
	hidden.User = mo.None[session.UserMessage]()
	hidden.Extension = mo.Some(
		session.ExtensionEnvelope{ExtensionID: "extension", EntryType: "opaque", Data: []byte("secret")},
	)
	entries = append(entries, hidden)
	sessions.EXPECT().ProjectSuffix(gomock.Any(), gomock.Any()).DoAndReturn(
		func(projected []session.Entry, _ string) ([]agent.HistoryEntry, error) {
			if len(projected) == 1 && projected[0].BranchSummary.IsSome() {
				return []agent.HistoryEntry{textHistory("rendered summary")}, nil
			}
			return []agent.HistoryEntry{textHistory("tail")}, nil
		},
	).AnyTimes()
	service := New(sessions)
	service.BindOrchestration(nil, nil, 1)
	providerRequest := baselineRequest()
	snapshot := Snapshot{
		Identity:     session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1},
		ActiveLeafID: mo.Some("hidden"), Entries: entries, Context: providerRequest.History,
		Previous: mo.None[session.CompactionEntry](),
	}

	// Act by creating the immutable Host request.
	request, err := service.newRequest(snapshot, providerRequest, TriggerManual, mo.None[string](), false)

	// Assert every public entry has its exact Host estimate and hidden replay contributes without being exposed.
	require.NoError(t, err)
	all := append(cloneInputEntries(request.Prefix), request.Suffix...)
	require.Equal(t, []int64{
		estimateRecord(4, 0), estimateRecord(0, 1), estimateRecord(int64(len("rendered summary")), 0),
		estimateRecord(4, 0), estimateRecord(int64(len("model")+len("opaque")), 0), 0,
	}, lo.Map(all, func(entry InputEntry, _ int) int64 { return entry.EstimatedTokens }))
	require.True(t, all[4].Model.MustGet().Content[0].ProviderContext.IsSome())
}

// TestReplacementEstimateRestoresImmutableOriginalReplayWeight verifies A-to-B-to-A handler composition.
func TestReplacementEstimateRestoresImmutableOriginalReplayWeight(t *testing.T) {
	t.Parallel()
	// Arrange an original model entry whose opaque replay is hidden from the public replacement payload.
	service := New(NewMockSessionState(gomock.NewController(t)))
	response := model.Response{
		Content: []model.Content{{
			Kind: model.ContentReasoning, Text: mo.Some("visible"), Final: true,
			ProviderContext: mo.Some(model.ProviderContext{
				Source: model.ProviderContextSource{
					ProviderID: "provider", API: "api", Model: "model", CompatibilityKey: mo.None[string](),
				},
				Payload: []byte("hidden replay"),
			}),
			ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](), Provider: mo.None[model.ProviderID](),
		Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](),
		Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	originalEstimate, err := estimateModelResponse(response)
	require.NoError(t, err)
	entry := compactionUserEntry("model", mo.None[string](), "unused")
	entry.User = mo.None[session.UserMessage]()
	entry.Model = mo.Some(response)
	original := Request{
		Trigger: TriggerManual, RetryIntent: false, Instructions: mo.None[string](),
		Model: baselineRequest().Model, ReasoningChoice: model.ReasoningChoiceOff,
		Prefix: nil, Suffix: []InputEntry{{Entry: entry, EstimatedTokens: originalEstimate}},
		Previous: mo.None[session.CompactionEntry](), ContextTokens: originalEstimate,
		ContextTokensEstimated: true, ContextWindow: 1000, ResponseBudget: 100, RetainedBudget: 20,
	}
	changed := cloneRequest(original)
	changedResponse := changed.Suffix[0].Model.MustGet()
	changedResponse.Content[0].Text = mo.Some("changed")
	changedResponse.Content[0].ProviderContext = mo.None[model.ProviderContext]()
	changed.Suffix[0].Model = mo.Some(changedResponse)
	changed, err = service.deriveReplacementEstimates(original, original, changed)
	require.NoError(t, err)
	restored := cloneRequest(original)
	restoredResponse := restored.Suffix[0].Model.MustGet()
	restoredResponse.Content[0].ProviderContext = mo.None[model.ProviderContext]()
	restored.Suffix[0].Model = mo.Some(restoredResponse)
	restored.Suffix[0].EstimatedTokens = 1

	// Act by restoring the original visible content after an intermediate replacement.
	restored, err = service.deriveReplacementEstimates(original, changed, restored)

	// Assert Host restores its trusted opaque replay weight and never mutates immutable original state.
	require.NoError(t, err)
	require.Equal(t, originalEstimate, restored.Suffix[0].EstimatedTokens)
	require.Equal(t, originalEstimate, original.Suffix[0].EstimatedTokens)
	require.True(t, original.Suffix[0].Model.MustGet().Content[0].ProviderContext.IsSome())
}

// TestNewRequestMarksReportedUsageEstimate verifies baseline reuse keeps the required public estimate marker.
func TestNewRequestMarksReportedUsageEstimate(t *testing.T) {
	t.Parallel()
	// Arrange one eligible reported-usage baseline and exact continuation.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	service := New(sessions)
	request := baselineRequest()
	response := baselineResponse(mo.Some(model.Usage{
		InputTokens: 5_500, OutputTokens: 3_000, CachedInputTokens: 1_000,
		CacheWriteTokens: 500, ReasoningTokens: 200, TotalTokens: 10_000,
	}))
	service.ObserveCompletedConversation(request, response)
	next := request
	next.History = append(
		append([]agent.HistoryEntry(nil), request.History...),
		modelHistory(response), textHistory(string(make([]byte, 400))),
	)
	entries := []session.Entry{compactionUserEntry("kept", mo.None[string](), "new")}
	snapshot := Snapshot{
		Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
		Context: next.History, Previous: mo.None[session.CompactionEntry](),
	}
	sessions.EXPECT().ProjectSuffix(entries, "kept").Return(next.History, nil).AnyTimes()

	// Act through complete compaction request creation.
	compactionRequest, err := service.newRequest(
		snapshot, next, TriggerThreshold, mo.None[string](), false,
	)

	// Assert reported sizing remains explicitly identified as an estimate.
	require.NoError(t, err)
	require.Equal(t, int64(10_108), compactionRequest.ContextTokens)
	require.True(t, compactionRequest.ContextTokensEstimated)
}

// TestReportedUsageBaselineReusesOnlyAnExactCompletedConversationPrefix verifies reuse and invalidation rules.
func TestReportedUsageBaselineReusesOnlyAnExactCompletedConversationPrefix(t *testing.T) {
	// Arrange a delivered completed response with normalized usage totaling ten thousand tokens.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 3}
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	service := New(sessions)
	request := baselineRequest()
	response := baselineResponse(mo.Some(model.Usage{
		InputTokens: 5_500, OutputTokens: 3_000, CachedInputTokens: 1_000,
		CacheWriteTokens: 500, ReasoningTokens: 200, TotalTokens: 10_000,
	}))
	service.ObserveCompletedConversation(request, response)
	next := request
	next.History = append(next.History, modelHistory(response), textHistory(string(make([]byte, 400))))

	// Act by estimating an exact continuation.
	got, err := service.EstimateContext(next)

	// Assert reported usage is reused and only the new record is added.
	require.NoError(t, err)
	require.Equal(t, int64(10_108), got)

	// Arrange each invalidating difference from the approved baseline identity.
	mutations := []struct {
		name   string
		mutate func(*modelexecution.ProviderRequest)
	}{
		{name: "instructions", mutate: func(value *modelexecution.ProviderRequest) { value.Instructions = "changed" }},
		{name: "descriptor", mutate: func(value *modelexecution.ProviderRequest) { value.Model.MaxTokens++ }},
		{
			name:   "reasoning",
			mutate: func(value *modelexecution.ProviderRequest) { value.ReasoningChoice = model.ReasoningChoiceHigh },
		},
		{name: "tools", mutate: func(value *modelexecution.ProviderRequest) { value.Tools[0].Description = "changed" }},
		{
			name:   "prefix",
			mutate: func(value *modelexecution.ProviderRequest) { value.History[0] = textHistory("changed") },
		},
		{
			name: "completed response",
			mutate: func(value *modelexecution.ProviderRequest) {
				changed := response.Clone()
				changed.Content[0].Text = mo.Some("changed")
				value.History[len(request.History)] = modelHistory(changed)
			},
		},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			// Arrange an independently detached changed request.
			changed := cloneTestProviderRequest(next)
			mutation.mutate(&changed)

			// Act by estimating after one baseline condition changes.
			estimate, estimateErr := service.EstimateContext(changed)

			// Assert the complete fallback estimate is used.
			require.NoError(t, estimateErr)
			require.Equal(t, mustFallbackEstimate(t, changed), estimate)
		})
	}

	// Act by explicitly invalidating after compaction.
	service.InvalidateBaseline()

	// Assert the same continuation now uses complete fallback sizing.
	estimate, estimateErr := service.EstimateContext(next)
	require.NoError(t, estimateErr)
	require.Equal(t, mustFallbackEstimate(t, next), estimate)
}

// TestCommittedCompactionInvalidatesBaselineDespitePublicationFailure verifies durable outcome handling.
func TestCommittedCompactionInvalidatesBaselineDespitePublicationFailure(t *testing.T) {
	t.Parallel()

	// Arrange one reusable baseline and a session commit that persisted before publication failed.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	sessions.EXPECT().ContextSession().Return(identity).AnyTimes()
	service := New(sessions)
	request := baselineRequest()
	response := baselineResponse(mo.Some(model.Usage{}))
	service.ObserveCompletedConversation(request, response)
	next := request
	next.History = append(next.History, modelHistory(response), textHistory("new"))
	publicationErr := errors.New("client publication failed")
	sessions.EXPECT().CommitCompaction(gomock.Any(), identity, mo.Some("leaf"), gomock.Any()).Return(
		session.Entry{ID: "compaction"}, publicationErr,
	)

	// Act by committing a durable compaction with a later delivery failure.
	committed, err := service.CommitCompaction(t.Context(), identity, mo.Some("leaf"), session.CompactionEntry{})

	// Assert the committed outcome is exposed and its stale usage baseline is cleared.
	require.ErrorIs(t, err, publicationErr)
	require.Equal(t, "compaction", committed.ID)
	estimate, estimateErr := service.EstimateContext(next)
	require.NoError(t, estimateErr)
	require.Equal(t, mustFallbackEstimate(t, next), estimate)
}

// TestReportedUsageBaselineRejectsChangedSessionIncarnation verifies navigation and replacement identity checks.
func TestReportedUsageBaselineRejectsChangedSessionIncarnation(t *testing.T) {
	t.Parallel()

	// Arrange one observed baseline and a process-local session identity that later changes.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	sessions.EXPECT().ContextSession().DoAndReturn(func() session.Identity { return identity }).AnyTimes()
	service := New(sessions)
	request := baselineRequest()
	response := baselineResponse(mo.Some(model.Usage{}))
	service.ObserveCompletedConversation(request, response)
	next := request
	next.History = append(next.History, modelHistory(response), textHistory("new"))
	identity.Incarnation++

	// Act by estimating after active-session replacement or navigation changed the incarnation.
	got, err := service.EstimateContext(next)

	// Assert identity mismatch invalidates reported usage and selects full fallback sizing.
	require.NoError(t, err)
	require.Equal(t, mustFallbackEstimate(t, next), got)
}

// TestReportedUsageBaselineRejectsChangedSessionID verifies a different durable conversation uses fallback sizing.
func TestReportedUsageBaselineRejectsChangedSessionID(t *testing.T) {
	t.Parallel()

	// Arrange one observed baseline and a later different durable session identity.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	identity := session.Identity{ID: "session-a", WorkingDirectory: "/project", Incarnation: 1}
	sessions.EXPECT().ContextSession().DoAndReturn(func() session.Identity { return identity }).AnyTimes()
	service := New(sessions)
	request := baselineRequest()
	response := baselineResponse(mo.Some(model.Usage{}))
	service.ObserveCompletedConversation(request, response)
	next := request
	next.History = append(next.History, modelHistory(response), textHistory("new"))
	identity.ID = "session-b"

	// Act by estimating the identical content under another durable session.
	got, err := service.EstimateContext(next)

	// Assert session mismatch invalidates reported usage and selects full fallback sizing.
	require.NoError(t, err)
	require.Equal(t, mustFallbackEstimate(t, next), got)
}

// TestReportedUsageBaselineRejectsIneligibleResponsesAndAcceptsPresentZero verifies outcome, presence, and validity rules.
func TestReportedUsageBaselineRejectsIneligibleResponsesAndAcceptsPresentZero(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		response model.Response
		reused   bool
	}{
		{name: "absent usage", response: baselineResponse(mo.None[model.Usage]()), reused: false},
		{name: "failed outcome", response: func() model.Response {
			value := baselineResponse(mo.Some(model.Usage{}))
			value.Outcome = mo.Some(model.OutcomeFailed)
			return value
		}(), reused: false},
		{name: "negative bucket", response: baselineResponse(mo.Some(model.Usage{InputTokens: -1})), reused: false},
		{
			name: "reasoning exceeds output",
			response: baselineResponse(mo.Some(model.Usage{
				InputTokens: 0, OutputTokens: 1, CachedInputTokens: 0,
				CacheWriteTokens: 0, ReasoningTokens: 2, TotalTokens: 1,
			})),
			reused: false,
		},
		{
			name: "inconsistent total",
			response: baselineResponse(mo.Some(model.Usage{
				InputTokens: 1, OutputTokens: 1, CachedInputTokens: 1,
				CacheWriteTokens: 1, ReasoningTokens: 0, TotalTokens: 3,
			})),
			reused: false,
		},
		{name: "present zero", response: baselineResponse(mo.Some(model.Usage{})), reused: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			// Arrange one active session and one candidate completed response.
			controller := gomock.NewController(t)
			sessions := NewMockSessionState(controller)
			sessions.EXPECT().
				ContextSession().
				Return(session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}).
				AnyTimes()
			service := New(sessions)
			request := baselineRequest()
			service.ObserveCompletedConversation(request, test.response)
			next := request
			next.History = append(next.History, modelHistory(test.response), textHistory(string(make([]byte, 400))))

			// Act by estimating the next exact continuation.
			got, err := service.EstimateContext(next)

			// Assert only eligible present usage establishes a reusable baseline.
			require.NoError(t, err)
			if test.reused {
				require.Equal(t, int64(108), got)
			} else {
				require.Equal(t, mustFallbackEstimate(t, next), got)
			}
		})
	}
}

// TestReportedUsageBaselineReusesNilToolCatalogue verifies detached ownership preserves absence.
func TestReportedUsageBaselineReusesNilToolCatalogue(t *testing.T) {
	t.Parallel()

	// Arrange an exact completed conversation whose provider request has no tool catalogue.
	controller := gomock.NewController(t)
	sessions := NewMockSessionState(controller)
	sessions.EXPECT().
		ContextSession().
		Return(session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}).
		AnyTimes()
	service := New(sessions)
	request := baselineRequest()
	request.Tools = nil
	response := baselineResponse(mo.Some(model.Usage{
		InputTokens: 5_500, OutputTokens: 3_000, CachedInputTokens: 1_000,
		CacheWriteTokens: 500, ReasoningTokens: 200, TotalTokens: 10_000,
	}))
	service.ObserveCompletedConversation(request, response)
	next := request
	next.History = append(next.History, modelHistory(response), textHistory(string(make([]byte, 400))))

	// Act by estimating the exact continuation without tools.
	got, err := service.EstimateContext(next)

	// Assert nil remains nil in the detached baseline and reported usage is reused.
	require.NoError(t, err)
	require.Equal(t, int64(10_108), got)
}

// mustFallbackEstimate returns one complete estimate or fails its test.
func mustFallbackEstimate(t *testing.T, request modelexecution.ProviderRequest) int64 {
	t.Helper()
	estimate, err := fallbackEstimate(request)
	require.NoError(t, err)
	return estimate
}

// baselineRequest returns one detached conversation request for baseline tests.
func baselineRequest() modelexecution.ProviderRequest {
	return modelexecution.ProviderRequest{
		Instructions: "instructions",
		Model: model.Descriptor{
			Provider: "provider", Model: "model", Input: []model.InputModality{model.InputModalityText},
			ContextWindow: 100_000, MaxTokens: 10_000,
			ReasoningCapabilities: model.ReasoningCapabilities{
				Supported: true, Choices: []model.ReasoningChoice{model.ReasoningChoiceLow, model.ReasoningChoiceHigh},
				Default: model.ReasoningChoiceLow,
			},
			ToolCapabilities: model.ToolCapabilities{
				StrictJSONSchema: true, Grammar: model.GrammarCapabilities{Lark: false, Regex: false},
			},
			Pricing: mo.None[model.Pricing](),
		},
		ReasoningChoice: model.ReasoningChoiceLow,
		History:         []agent.HistoryEntry{textHistory("first")},
		Tools: []tool.Descriptor{{
			Name: "tool", Description: "description", InputSchemaJSON: []byte(`{"type":"object"}`),
			ConstrainedSampling: mo.None[tool.ConstrainedSampling](),
		}},
	}
}

// baselineResponse returns one terminal response with selectable usage presence.
func baselineResponse(usage mo.Option[model.Usage]) model.Response {
	return model.Response{
		Content: []model.Content{{
			Kind: model.ContentText, Text: mo.Some("answer"), Final: true,
			ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
		}},
		Outcome: mo.Some(model.OutcomeStop), ErrorMessage: mo.None[string](),
		Provider: mo.Some(model.ProviderID("provider")), Model: mo.Some(model.ID("model")),
		ResponseModel: mo.None[model.ID](), ResponseID: mo.None[string](), Usage: usage, Diagnostics: nil,
	}
}

// textHistory creates one text-only user history record.
func textHistory(text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(text)),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
}

// modelHistory creates one model history record.
func modelHistory(response model.Response) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryModel, User: mo.None[model.Message](), Model: mo.Some(response.Clone()),
		ToolResult: mo.None[agent.ToolResult](),
	}
}

// cloneTestProviderRequest detaches all mutable request fields used by mutation tests.
func cloneTestProviderRequest(request modelexecution.ProviderRequest) modelexecution.ProviderRequest {
	cloned := request
	cloned.Model = request.Model.Clone()
	cloned.History = make([]agent.HistoryEntry, len(request.History))
	for index := range request.History {
		cloned.History[index] = request.History[index].Clone()
	}
	cloned.Tools = make([]tool.Descriptor, len(request.Tools))
	for index := range request.Tools {
		cloned.Tools[index] = request.Tools[index]
		cloned.Tools[index].InputSchemaJSON = append([]byte(nil), request.Tools[index].InputSchemaJSON...)
	}
	return cloned
}
