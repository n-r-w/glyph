//go:build integration

package app

import (
	"path/filepath"
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

const (
	fullContentUserImageBase64 = "AQIDBA=="
	fullContentToolImageBase64 = "CQgHBg=="
)

// appendFullContentFixture adds one provider-compatible turn and one private extension entry.
func appendFullContentFixture(t *testing.T, paths persistence.Paths, sessionID string) {
	t.Helper()
	appendFullContentFixtureWithUsage(
		t, paths, sessionID, mo.None[model.Usage](), mo.None[session.EstimatedCost](),
	)
}

// appendFullContentFixtureWithUsage adds the full-content turn with explicit model usage presence.
func appendFullContentFixtureWithUsage(
	t *testing.T,
	paths persistence.Paths,
	sessionID string,
	usage mo.Option[model.Usage],
	estimatedCost mo.Option[session.EstimatedCost],
) {
	t.Helper()

	canonical, err := sessionstore.CanonicalWorkingDirectory("")
	require.NoError(t, err)
	repository := sessionstore.New(
		filepath.Join(paths.Directory, "sessions"), canonical, sessionfilesystem.New(),
	)
	require.NoError(t, repository.Initialize(t.Context()))
	loaded, err := repository.Load(t.Context(), session.ID(sessionID))
	require.NoError(t, err)
	require.NotEmpty(t, loaded.Tree.Entries())
	// Keep this fixture newest while later real-time interactions exercise other sessions.
	loadedEntries := loaded.Tree.Entries()
	createdAt := loadedEntries[len(loadedEntries)-1].CreatedAt.Add(time.Hour)
	storagePath := loaded.StoragePath
	call := model.ToolCall{
		ID: "full-call", Name: "bash", Arguments: map[string]any{"command": "printf full-tool"},
	}
	entries := []session.Entry{
		{
			ParentID:    mo.None[string](),
			ID:          "full-user-entry",
			CreatedAt:   createdAt.Add(time.Second),
			Information: mo.None[session.Information](),
			User: mo.Some(model.Message{Content: []model.InputContent{
				{
					Kind:      model.InputContentText,
					Text:      mo.Some("full user"),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				},
				{
					Kind:      model.InputContentImage,
					Text:      mo.None[string](),
					MediaType: mo.Some("image/png"),
					Data:      mo.Some([]byte{1, 2, 3, 4}),
				},
				{
					Kind:      model.InputContentText,
					Text:      mo.Some("after image"),
					MediaType: mo.None[string](),
					Data:      mo.None[[]byte](),
				},
			}}),
			Model:         mo.None[session.ModelResponse](),
			ToolResult:    mo.None[session.ToolResult](),
			Extension:     mo.None[session.ExtensionEnvelope](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			BranchSummary: mo.None[session.
				BranchSummaryEntry](),
		},
		{
			ParentID: mo.None[string](), ID: "full-model-entry", CreatedAt: createdAt.Add(2 * time.Second),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.Some(model.Response{
				Content: []model.Content{
					{
						Kind: model.ContentReasoning, Text: mo.Some("full reasoning"), Final: true,
						ProviderContext: mo.Some(model.ProviderContext{
							Source: model.ProviderContextSource{
								ProviderID: "openai-codex", API: "responses", Model: "selected-model",
								CompatibilityKey: mo.None[string](),
							},
							Payload: []byte(
								`{"id":"r-full","encrypted_content":"enc-full","summary":["full summary"]}`,
							),
						}),
						ToolCall: mo.None[model.ToolCall](),
					},
					{
						Kind: model.ContentRefusal, Text: mo.Some("full refusal"), Final: true,
						ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.None[model.ToolCall](),
					},
					{
						Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
						ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(call),
					},
				},
				Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
				Provider: mo.Some(model.ProviderID("openai-codex")), Model: mo.Some(model.ID("selected-model")),
				ResponseModel: mo.Some(model.ID("selected-model")), ResponseID: mo.Some("full-response"),
				Usage:       usage,
				Diagnostics: []model.Diagnostic{{Code: "full_notice", Message: "full diagnostic"}},
			}),
			ToolResult: mo.None[session.ToolResult](), Extension: mo.None[session.ExtensionEnvelope](),
			EstimatedCost: estimatedCost, BranchSummary: mo.None[session.
					BranchSummaryEntry](),
		},
		{
			ParentID:    mo.None[string](),
			ID:          "full-tool-entry",
			CreatedAt:   createdAt.Add(3 * time.Second),
			Information: mo.None[session.Information](),
			User:        mo.None[session.UserMessage](),
			Model:       mo.None[session.ModelResponse](),
			ToolResult: mo.Some(agent.ToolResult{
				CallID: call.ID, ToolName: call.Name, IsError: false,
				Contents: []tool.ResultContent{
					{
						Kind:  tool.ResultContentText,
						Text:  mo.Some("full tool output"),
						Image: mo.None[tool.ResultImage](),
					},
					{
						Kind:  tool.ResultContentImage,
						Text:  mo.None[string](),
						Image: mo.Some(tool.ResultImage{MediaType: "image/png", Data: []byte{9, 8, 7, 6}}),
					},
				},
			}),
			Extension:     mo.None[session.ExtensionEnvelope](),
			EstimatedCost: mo.None[session.EstimatedCost](),
			BranchSummary: mo.None[session.
				BranchSummaryEntry](),
		},
		{
			ParentID: mo.None[string](), ID: "full-extension-entry", CreatedAt: createdAt.Add(4 * time.Second),
			Information: mo.None[session.Information](), User: mo.None[session.UserMessage](),
			Model: mo.None[session.ModelResponse](), ToolResult: mo.None[session.ToolResult](),
			Extension: mo.Some(session.ExtensionEnvelope{
				ExtensionID: "example.extension", EntryType: "checkpoint",
				Data: []byte(`{"private":"full-extension"}`),
			}), EstimatedCost: mo.None[session.EstimatedCost](), BranchSummary: mo.None[session.
						BranchSummaryEntry](),
		},
	}
	for index := range entries {
		if index == 0 {
			entries[index].ParentID = loaded.Tree.ActiveLeafID()
		} else {
			entries[index].ParentID = mo.Some(entries[index-1].ID)
		}
		result, applyErr := repository.Apply(t.Context(), hostsessions.ApplyCommand{
			Header: loaded.Header, StoragePath: storagePath,
			Mutation: hostsessions.Mutation{
				Entry: mo.Some(entries[index]), Navigation: mo.None[hostsessions.NavigationMutation](),
				Label:              mo.None[hostsessions.LabelMutation](),
				SessionInformation: mo.None[hostsessions.SessionInformationMutation](),
			},
		})
		require.NoError(t, applyErr)
		storagePath = result.StoragePath
	}
}
