//go:build integration

package app

import (
	"context"
	"encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/config/codingagent"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	settingstore "github.com/n-r-w/glyph/host/internal/infra/persistence/settings"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	runtimeadapter "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionmodels"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/middleware"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/runcontrol"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
)

const (
	// consumedChildFixtureEnvironment supplies the public fixture's outcome artifact.
	consumedChildFixtureEnvironment = "GLYPH_REFERENCE_CONSUMED_CHILD"
	// consumedChildFixtureTool identifies the live ordinary compaction caller.
	consumedChildFixtureTool = "reference_consumed_child"
	// consumedChildCompleteCause is the existing handler's complete source diagnostic.
	consumedChildCompleteCause = "reference request handler failure: complete source cause"
)

// TestConsumedNestedCompactionFailureStopsCore exercises the public SDK, actual handler and Host ownership.
func TestConsumedNestedCompactionFailureStopsCore(t *testing.T) {
	// Arrange the real request-handler failure independently of invalid-output modes.
	t.Setenv(consumedInvalidOutputEnvironment, "")
	t.Setenv(consumedChildGuidanceEnvironment, "")
	// Act through Core using the shared real-process scenario.
	// Assert retained failure, history, and independent work in the scenario.
	assertConsumedCompactionStopsCore(t, consumedChildCompleteCause)
}

// assertConsumedCompactionStopsCore checks the ordinary tool boundary with real nested participants.
func assertConsumedCompactionStopsCore(t *testing.T, completeCause string) {
	t.Helper()
	// Arrange a real extension process and an idle Host gate for direct Core execution.
	artifact := filepath.Join(t.TempDir(), "consumed.json")
	t.Setenv(consumedChildFixtureEnvironment, artifact)
	paths := testPaths(t, configuredRequestSettings)
	writeProgrammaticCredentials(t, paths)
	configured, err := settingstore.New(paths.SettingsFile).Load()
	require.NoError(t, err)
	mocks := gomock.NewController(t)
	reporter := extensionmanager.NewMockFailureReporter(mocks)
	factory := runtimeadapter.NewFactory()
	runtimes := extensionmanager.New(catalog.New(), factory, reporter)
	t.Cleanup(func() { require.NoError(t, runtimes.Close()) })
	tools := toolservice.New(runtimes)
	sessionServices, err := newSessionComposition(t.Context(), paths, runtimes)
	require.NoError(t, err)
	contexts := bindExtensionContexts(runtimes, tools, sessionServices, configured.Compaction.ParticipantTimeout)
	providerCatalog, err := newProviderCatalog(configured, paths, nil)
	require.NoError(t, err)
	modelExecution := modelexecution.New(providerCatalog, retryPolicy(configured), runtimes, nil, contexts)
	modelExecution.BindManualCompaction(contexts, tools, codingagent.Instructions())
	extensionModels := extensionmodels.New(providerCatalog, modelExecution, contexts)
	require.NoError(t, sessionServices.active.BindPricingCatalog(providerCatalog))
	require.NoError(t, sessionServices.tree.BindModels(providerCatalog, modelExecution))
	observers := lifecycle.New(runtimes, contexts)
	bindExtensionHostFactory(factory, runtimes, extensionModels, contexts, nil, modelExecution, sessionServices.gate)
	startupService := startup.New(runtimes, tools, sessionServices.tree, observers, nil)
	require.NoError(t, startupService.BindCompaction(contexts))
	report, err := startupService.Load(t.Context(), startup.Request{
		DataDirectory: paths.Directory, ExtensionDirectory: buildManualPolicyFixture(t),
	})
	require.NoError(t, err)
	require.Empty(t, report.Issues)
	runtimes.Activate(t.Context())
	for _, text := range []string{manualOriginalText, manualRetainedText} {
		require.NoError(t, sessionServices.active.Append(t.Context(), agent.HistoryEntry{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(text)),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		}))
	}
	arguments, err := model.NewToolCallArguments([]byte(`{"fail":true}`))
	require.NoError(t, err)
	first := model.ToolCall{ID: "consumed", Name: consumedChildFixtureTool, Arguments: arguments}
	second := model.ToolCall{ID: "must-not-run", Name: consumedChildFixtureTool, Arguments: arguments}
	response := model.Response{
		Content: []model.Content{
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(first),
			},
			{
				Kind: model.ContentToolCall, Text: mo.None[string](), Final: true,
				ProviderContext: mo.None[model.ProviderContext](), ToolCall: mo.Some(second),
			},
		},
		Outcome: mo.Some(model.OutcomeToolUse), ErrorMessage: mo.None[string](),
		Provider: mo.None[model.ProviderID](), Model: mo.None[model.ID](), ResponseModel: mo.None[model.ID](),
		ResponseID: mo.None[string](), Usage: mo.None[model.Usage](), Diagnostics: nil,
	}
	provider := agentrun.NewMockModelProvider(mocks)
	provider.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ agentrun.ModelRequest, update agentrun.StreamHandler) error {
			return update(agentrun.StreamEvent{
				Kind: agentrun.StreamEventDone, Position: mo.None[int](), Content: mo.None[model.Content](),
				Delta: mo.None[string](), Preview: mo.None[model.ToolCallPreview](),
				ToolCall: mo.None[model.ToolCall](), Response: mo.Some(response),
			})
		},
	)
	modelRuntime := agentrun.NewMockEffectiveContextPort(mocks)
	snapshot := providerCatalog.ActiveBinding()
	modelRuntime.EXPECT().PrepareContext(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, request agentrun.ContextRequest) (agentrun.EffectiveContext, error) {
			return agentrun.EffectiveContext{Request: agentrun.ModelRequest{
				Instructions: request.Instructions, Model: snapshot.Model, ReasoningChoice: snapshot.ReasoningChoice,
				History: request.History, Tools: request.Tools,
			}, Provider: provider}, nil
		})
	events := agentrun.NewMockEventSink(mocks)
	events.EXPECT().Deliver(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	middlewareOwner := middleware.New(runtimes, contexts, providerCatalog)
	require.NoError(t, modelExecution.BindResponseFinalizer(middlewareOwner))
	core := agentrun.New(
		codingagent.Instructions(), modelRuntime, tools, events, sessionServices.active, middlewareOwner,
	)

	// Act through Core while the ordinary tool consumes a nested compaction failure.
	result, runErr := core.Run(
		t.Context(),
		runcontrol.Request{RunID: "consumed-child", Message: model.TextMessage("invoke tool")},
	)

	// Assert the parent and run fail with the original cause and retain required tool-result history.
	require.ErrorContains(t, runErr, completeCause)
	require.Equal(t, agent.RunOutcomeFailed, result.Outcome)
	encoded, err := os.ReadFile(artifact)
	require.NoError(t, err)
	var observed struct {
		Failure string `json:"failure"`
		Later   string `json:"later"`
	}
	require.NoError(t, json.Unmarshal(encoded, &observed))
	require.Contains(t, observed.Failure, completeCause)
	require.Contains(t, observed.Later, "parent invocation")
	history := core.History()
	require.Equal(t, "consumed", history[len(history)-1].ToolResult.OrEmpty().CallID)
	require.NoError(t, core.Settle("consumed-child"))
	release, acquired := sessionServices.gate.TryAcquire()
	require.True(t, acquired)
	release()
	// Assert an independent invocation still uses the same healthy extension and connection.
	independentArguments, err := model.NewToolCallArguments([]byte(`{"fail":false}`))
	require.NoError(t, err)
	independent, err := tools.Execute(t.Context(), model.ToolCall{
		ID: "independent", Name: consumedChildFixtureTool, Arguments: independentArguments,
	}, nil)
	require.NoError(t, err)
	require.False(t, independent.IsError)
}
