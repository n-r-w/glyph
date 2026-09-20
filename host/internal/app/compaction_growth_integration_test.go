//go:build integration

package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	contextcompaction "github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionmodels"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessions"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestPreparationRetainsCommitAndRejectsRealObserverContextGrowth verifies the assembled threshold path.
func TestPreparationRetainsCommitAndRejectsRealObserverContextGrowth(t *testing.T) {
	t.Parallel()
	runRealObserverContextGrowth(t, false)
}

// TestRecoveryRetainsCommitAndRejectsRealObserverContextGrowth verifies the assembled overflow path.
func TestRecoveryRetainsCommitAndRejectsRealObserverContextGrowth(t *testing.T) {
	t.Parallel()
	runRealObserverContextGrowth(t, true)
}

// runRealObserverContextGrowth exercises real commit and observer append through one automatic entry point.
func runRealObserverContextGrowth(t *testing.T, recoverOverflow bool) {
	t.Helper()
	// Arrange a real session owner and public extension that appends visible context from its success observer.
	controller := gomock.NewController(t)
	repository := sessions.NewMockRepository(controller)
	ids := sessions.NewMockIDGenerator(controller)
	clock := sessions.NewMockClock(controller)
	pricing := sessions.NewMockPricingCatalog(controller)
	publisher := sessions.NewMockEntryPublisher(controller)
	identifiers := []string{"session", "old", "kept", "compaction", "growth"}
	identifierIndex := 0
	ids.EXPECT().NewID().DoAndReturn(func() (string, error) {
		identifier := identifiers[identifierIndex]
		identifierIndex++
		return identifier, nil
	}).Times(len(identifiers))
	clock.EXPECT().Now().Return(time.Unix(1, 0)).AnyTimes()
	repository.EXPECT().Apply(gomock.Any(), gomock.Any()).Return(
		sessions.ApplyResult{StoragePath: "/sessions/session.jsonl"}, nil,
	).Times(4)
	publisher.EXPECT().PublishSessionEntry(gomock.Any()).Return(
		func(context.Context) error { return nil }, nil,
	).Times(2)
	sessionOwner := sessions.New(repository, ids, clock, pricing, "/project")
	sessionOwner.BindEntryPublisher(publisher)
	_, _, err := sessionOwner.CreateActive()
	require.NoError(t, err)
	require.NoError(t, sessionOwner.Append(t.Context(), growthUserHistory(strings.Repeat("oversized ", 5_000))))
	require.NoError(t, sessionOwner.Append(t.Context(), growthUserHistory("kept")))

	extensionDirectory := t.TempDir()
	writeHandlerFixtureScript(t, extensionDirectory, "01-compact", handlerFixtureCompactionSupplyMode, "grow")
	factory := extensionruntime.NewFactory()
	reporter := extensionmanager.NewMockFailureReporter(controller)
	reporter.EXPECT().ReportRuntimeFailure(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	extensions := extensionmanager.New(catalog.New(), factory, reporter)
	t.Cleanup(func() { require.NoError(t, extensions.Close()) })
	contexts := extensioncontext.New(extensions, sessionOwner)
	extensionModels := extensionmodels.New(
		extensionmodels.NewMockCatalog(controller), extensionmodels.NewMockModelRequester(controller), contexts,
	)
	factory.BindHostServiceFactory(func(extensionID, runtimeID string) extensionsdk.HostService {
		return extensioncontroller.New(extensionModels, contexts, extensions, extensionID, runtimeID)
	})
	compactor := contextcompaction.New(sessionOwner)
	require.NoError(t, compactor.BindOrchestration(extensions, contexts, 20_000))
	treeOwner := sessiontree.New(
		sessionOwner, sessiontree.NewMockModelSelection(controller),
		sessiontree.NewMockModelRequester(controller), extensions,
	)
	startupService := startup.New(
		extensions, toolservice.New(extensions), treeOwner, lifecycle.New(extensions, nil), nil,
	)
	require.NoError(t, startupService.BindCompaction(compactor))
	report, err := startupService.Load(
		t.Context(), startup.Request{DataDirectory: "", ExtensionDirectory: extensionDirectory},
	)
	require.NoError(t, err)
	require.Empty(t, report.Issues)
	extensions.Activate(t.Context())

	descriptor := publicCompactionDescriptor()
	descriptor.ContextWindow = 10_000
	descriptor.MaxTokens = 1_000
	request := modelexecution.ProviderRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: model.ReasoningChoiceOff,
		History: sessionOwner.Snapshot(), Tools: nil,
	}

	// Act through production compaction, real commit, public observer append, and final sizing.
	if recoverOverflow {
		_, err = compactor.RecoverOverflow(t.Context(), request)
	} else {
		_, err = compactor.PrepareContext(t.Context(), request)
	}

	// Assert the durable marker and observer append remain while the oversized outbound request is rejected.
	require.ErrorContains(t, err, "after compaction")
	require.ErrorContains(t, err, "exceeds input budget")
	entries := sessionOwner.ActiveEntries()
	require.Len(t, entries, 4)
	require.True(t, entries[2].Compaction.IsSome())
	require.Equal(t, "public custom summary", entries[2].Compaction.MustGet().Summary)
	require.True(t, entries[3].ExtensionMessage.IsSome())
	require.Contains(t, entries[3].ExtensionMessage.MustGet().Text, "observer growth")
}

// growthUserHistory creates one complete user entry for the real session owner.
func growthUserHistory(text string) agent.HistoryEntry {
	return agent.HistoryEntry{
		Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage(text)),
		Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
	}
}
