//go:build !integration

package extensionruntime

import (
	"encoding/json/v2"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/domain/tool"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
)

// TestRegistrationBindsDiscoveredIdentity preserves raw declarations while discovery alone supplies trusted identity.
func TestRegistrationBindsDiscoveredIdentity(t *testing.T) {
	t.Parallel()
	// Arrange declarations whose tool and handler names differ from the discovered identity.
	raw := Registration{Tools: []mo.Option[ToolDeclaration]{mo.None[ToolDeclaration](), mo.Some(ToolDeclaration{
		Name:            "declared-name",
		Description:     "description",
		InputSchemaJSON: []byte("schema"),
		Constraint: mo.Some(
			ConstraintDeclaration{
				Kind:       ConstraintJSONSchema,
				Present:    true,
				Strictness: 99,
				Lark:       mo.None[string](),
				Regex:      mo.None[string](),
			},
		),
	})}, Handlers: []mo.Option[HandlerDeclaration]{
		mo.None[HandlerDeclaration](),
		mo.Some(HandlerDeclaration{ID: "declared-handler", Kind: 99}),
	}}
	// Act at the runtime binding boundary before startup capability acceptance.
	pending := (&Service{}).bindRegistration(
		Candidate{ID: "discovered", Path: "/trusted/executable", InstanceID: "instance"},
		raw,
	)
	// Assert trusted identity and unvalidated declaration presence reach startup separately.
	require.Equal(t, "discovered", pending.ID)
	require.Equal(t, "/trusted/executable", pending.Path)
	require.False(t, pending.Tools[0].Present)
	require.Equal(t, "declared-name", pending.Tools[1].Name)
	require.Equal(
		t,
		startup.RawJSONSchemaStrictness(99),
		pending.Tools[1].ConstrainedSampling.OrEmpty().JSONSchemaStrictness,
	)
	require.False(t, pending.Handlers[0].Present)
	require.Equal(t, startup.RawHandlerKindUnspecified, pending.Handlers[1].Kind)
}

// TestHandlerProjectionPreservesOriginalCurrentAndHidesPrivateData separates capability state from process payloads.
func TestHandlerProjectionPreservesOriginalCurrentAndHidesPrivateData(t *testing.T) {
	t.Parallel()
	// Arrange distinct original and current requests with private abandoned entries.
	entries := privateEntries()
	original := sessiontree.HandlerNavigationState{
		SessionID:             "session",
		PrecedingActiveLeafID: mo.Some("leaf"),
		Request: sessiontree.HandlerNavigationRequest{
			Navigation: sessiontree.NavigationRequest{
				TargetEntryID: "original",
				SummaryMode:   sessiontree.SummaryModeSummarize,
				CustomFocus:   mo.None[string](),
			},
			SummaryModel: model.Selection{Provider: "provider", Model: "model", ReasoningChoice: "medium"},
		},
		Preparation: session.NavigationPreparation{
			DestinationID:    mo.Some("destination"),
			NextInput:        mo.Some("private prepared input"),
			CommonAncestorID: mo.Some("common"),
			AbandonedPath:    entries,
		},
	}
	current := original
	current.Request.Navigation.TargetEntryID = "current"
	current.Request.Navigation.CustomFocus = mo.Some("")
	summary := sessiontree.HandlerBranchSummaryResult{
		Summary: "current summary",
		Source: session.BranchSummarySource{
			ExtensionID: mo.Some("producer"),
			Model:       mo.None[session.BranchSummaryModelSource](),
		},
	}
	request := sessiontree.HandlerRequest{
		Context: extension.Context{
			ID:                "binding",
			ExtensionID:       "owner",
			RuntimeInstanceID: "instance",
			SessionID:         "session",
			WorkingDirectory:  "/project",
		},
		Request: mo.Some(
			sessiontree.RequestHandlerInvocation{Original: original, Current: current, CurrentResult: mo.Some(summary)},
		),
		Result:   mo.None[sessiontree.ResultHandlerInvocation](),
		Observer: mo.None[sessiontree.TreeObserverInvocation](),
	}
	// Act before any process transport is invoked.
	payload, err := (&Service{}).projectHandler(request)
	require.NoError(t, err)
	encoded, err := json.Marshal(payload)
	// Assert distinct snapshots, exact presence, public content and removal of both private data sources.
	require.NoError(t, err)
	require.Equal(t, "original", payload.Original.Request.TargetEntryID)
	require.Equal(t, "current", payload.Current.Request.TargetEntryID)
	require.True(t, payload.Original.Request.CustomFocus.IsNone())
	require.True(t, payload.Current.Request.CustomFocus.IsSome())
	require.Equal(t, "current summary", payload.CurrentResult.OrEmpty().Summary)
	require.Contains(t, string(encoded), "public reasoning")
	require.Contains(t, string(encoded), "hidden-kind")
	require.NotContains(t, string(encoded), "private provider bytes")
	require.NotContains(t, string(encoded), "private extension bytes")
	require.NotContains(t, string(encoded), "private prepared input")
}

// TestLifecycleProjectionPreservesTransitionAndFiltersPrivateContext moves neutral filtering before transport.
func TestLifecycleProjectionPreservesTransitionAndFiltersPrivateContext(t *testing.T) {
	t.Parallel()
	// Arrange a context-only reasoning transition followed by a finalized response with public text.
	response := model.Response{
		Content:       []model.Content{privateContent(mo.None[string]()), privateContent(mo.Some("public reasoning"))},
		Outcome:       mo.Some(model.OutcomeStop),
		ErrorMessage:  mo.None[string](),
		Provider:      mo.None[model.ProviderID](),
		Model:         mo.None[model.ID](),
		ResponseModel: mo.None[model.ID](),
		ResponseID:    mo.None[string](),
		Usage:         mo.None[model.Usage](),
		Diagnostics:   nil,
	}
	event := agent.Event{
		Type:       agent.EventContentStart,
		RunID:      "run",
		Position:   mo.Some(0),
		Content:    mo.Some(privateContent(mo.None[string]())),
		Message:    mo.None[model.Response](),
		Preview:    mo.None[model.ToolCallPreview](),
		ToolCall:   mo.None[model.ToolCall](),
		Progress:   mo.None[tool.Progress](),
		ToolResult: mo.None[agent.ToolResult](),
		Turn:       mo.None[agent.TurnSummary](),
		Agent:      mo.None[agent.RunSummary](),
	}
	// Act at runtime management rather than the process encoder.
	payload := (&Service{}).projectLifecycle(extension.Context{}, lifecycle.Event{Agent: event, Settled: false})
	event.Type = agent.EventMessageEnd
	event.Content = mo.None[model.Content]()
	event.Message = mo.Some(response)
	terminal := (&Service{}).projectLifecycle(extension.Context{}, lifecycle.Event{Agent: event, Settled: false})
	encoded, err := json.Marshal([]LifecycleInvocation{payload, terminal})
	// Assert transition identity remains while only public response content reaches the process payload.
	require.NoError(t, err)
	require.Equal(t, agent.EventContentStart, payload.Type)
	require.Equal(t, 0, payload.Position.OrEmpty())
	require.True(t, payload.Content.IsNone())
	require.Len(t, terminal.Response.OrEmpty().Content, 1)
	require.Contains(t, string(encoded), "public reasoning")
	require.NotContains(t, string(encoded), "private provider bytes")
}

// TestHandlerActionPreservesRawActionSemantics leaves preserve, replace, clear and invalid intent to the capability.
func TestHandlerActionPreservesRawActionSemantics(t *testing.T) {
	t.Parallel()
	// Arrange each raw result action with an explicitly present empty replacement.
	for _, raw := range []int32{0, 1, 2, 3, 99} {
		action := HandlerAction{
			Kind:          InvocationRequest,
			Cancel:        true,
			RequestAction: 2,
			Request: mo.Some(
				Navigation{
					TargetEntryID: "target",
					SummaryMode:   2,
					CustomFocus:   mo.Some(""),
					SummaryModel:  model.Selection{},
				},
			),
			ResultAction: raw,
			Result:       mo.Some(Summary{}),
		}
		// Act without running navigation policy or applying the requested mutation.
		result := (&Service{}).capabilityAction(action).Request.OrEmpty()
		// Assert exact action distinctions and payload presence survive the boundary.
		expected := sessiontree.ResultAction(raw)
		if raw == 99 {
			expected = 0
		}
		require.Equal(t, expected, result.ResultAction)
		require.Equal(t, sessiontree.RequestActionReplace, result.RequestAction)
		require.True(t, result.Cancel)
		require.True(t, result.Request.IsSome())
		require.True(t, result.Result.IsSome())
	}
}

// privateContent supplies a public-text alternative with opaque provider reconstruction bytes.
func privateContent(text mo.Option[string]) model.Content {
	return model.Content{
		Kind:  model.ContentReasoning,
		Text:  text,
		Final: true,
		ProviderContext: mo.Some(
			model.ProviderContext{
				Source: model.ProviderContextSource{
					ProviderID:       "provider",
					API:              "api",
					Model:            "model",
					CompatibilityKey: mo.None[string](),
				},
				Payload: []byte("private provider bytes"),
			},
		),
		ToolCall: mo.None[model.ToolCall](),
	}
}

// privateEntries supplies independent private model and extension entries for projection tests.
func privateEntries() []session.Entry {
	modelEntry := session.Entry{
		ID:          "model",
		ParentID:    mo.None[string](),
		CreatedAt:   time.Unix(1, 0),
		Information: mo.None[session.Information](),
		User:        mo.None[session.UserMessage](),
		Model: mo.Some(
			session.ModelResponse{
				Content:       []model.Content{privateContent(mo.Some("public reasoning"))},
				Outcome:       mo.None[model.Outcome](),
				ErrorMessage:  mo.None[string](),
				Provider:      mo.None[model.ProviderID](),
				Model:         mo.None[model.ID](),
				ResponseModel: mo.None[model.ID](),
				ResponseID:    mo.None[string](),
				Usage:         mo.None[model.Usage](),
				Diagnostics:   nil,
			},
		),
		EstimatedCost:    mo.None[session.EstimatedCost](),
		ToolResult:       mo.None[session.ToolResult](),
		Extension:        mo.None[session.ExtensionEnvelope](),
		BranchSummary:    mo.None[session.BranchSummaryEntry](),
		ExtensionMessage: mo.None[session.ExtensionMessage](),
	}
	hidden := modelEntry
	hidden.ID = "hidden"
	hidden.Model = mo.None[session.ModelResponse]()
	hidden.Extension = mo.Some(
		session.ExtensionEnvelope{
			ExtensionID: "owner",
			EntryType:   "hidden-kind",
			Data:        []byte("private extension bytes"),
		},
	)
	return []session.Entry{modelEntry, hidden}
}
