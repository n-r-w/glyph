//go:build integration

package app

import (
	"context"
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	contextcompaction "github.com/n-r-w/glyph/host/internal/usecase/host/contextcompaction"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensionmodels"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	extensionsdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// TestCompactionUsesRealGRPCCustomResult verifies Host orchestration through the public extension fixture.
func TestCompactionUsesRealGRPCCustomResult(t *testing.T) {
	t.Parallel()
	// Arrange one real extension process that supplies a ready compaction result.
	extensionDirectory := t.TempDir()
	writeHandlerFixtureScript(t, extensionDirectory, "01-compact", handlerFixtureCompactionSupplyMode, "")
	factory := extensionruntime.NewFactory()
	managerController := gomock.NewController(t)
	reporter := extensionmanager.NewMockFailureReporter(managerController)
	reporter.EXPECT().ReportRuntimeFailure(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	extensions := extensionmanager.New(catalog.New(), factory, reporter)
	t.Cleanup(func() { require.NoError(t, extensions.Close()) })

	controller := gomock.NewController(t)
	treeOwner := sessiontree.New(
		sessiontree.NewMockActiveSession(controller), sessiontree.NewMockModelSelection(controller),
		sessiontree.NewMockModelRequester(controller), extensions,
	)
	tools := toolservice.New(extensions)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	contextSession := extensioncontext.NewMockSessionState(controller)
	contextSession.EXPECT().ContextSession().Return(identity).AnyTimes()
	contexts := extensioncontext.New(extensions, contextSession)
	extensionModels := extensionmodels.New(
		extensionmodels.NewMockCatalog(controller), extensionmodels.NewMockModelRequester(controller), contexts,
	)
	factory.BindHostServiceFactory(func(extensionID, runtimeID string) extensionsdk.HostService {
		return extensioncontroller.New(extensionModels, contexts, extensions, extensionID, runtimeID)
	})

	sessionState := contextcompaction.NewMockSessionState(controller)
	compactor := contextcompaction.New(sessionState)
	require.NoError(t, compactor.BindOrchestration(extensions, contexts, 20_000))
	startupService := startup.New(extensions, tools, treeOwner, lifecycle.New(extensions, nil), nil)
	require.NoError(t, startupService.BindCompaction(compactor))
	report, err := startupService.Load(
		t.Context(), startup.Request{DataDirectory: "", ExtensionDirectory: extensionDirectory},
	)
	require.NoError(t, err)
	require.Empty(t, report.Issues)
	extensions.Activate(t.Context())

	descriptor := publicCompactionDescriptor()
	providerRequest := modelexecution.ProviderRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: model.ReasoningChoiceOff,
		History: []agent.HistoryEntry{{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("history")),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		}},
		Tools: nil,
	}
	createdAt := time.Unix(1, 0)
	entries := []session.Entry{
		grpcUserEntry("old", mo.None[string](), "old", createdAt),
		grpcUserEntry("kept", mo.Some("old"), "kept", createdAt),
	}
	sessionState.EXPECT().CompactionSnapshot().Return(contextcompaction.Snapshot{
		Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
		Context: providerRequest.History, Previous: mo.None[session.CompactionEntry](),
	})
	sessionState.EXPECT().ProjectSuffix(gomock.Any(), gomock.Any()).Return(providerRequest.History, nil).AnyTimes()
	sessionState.EXPECT().ProjectCompaction(gomock.Any(), gomock.Any()).Return(providerRequest.History).AnyTimes()
	committed := grpcUserEntry("compaction", mo.Some("kept"), "", createdAt)
	committed.User = mo.None[session.UserMessage]()
	committed.Compaction = mo.Some(session.CompactionEntry{
		Summary: "public custom summary", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("01-compact"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
	})
	sessionState.EXPECT().CommitCompaction(gomock.Any(), identity, mo.Some("kept"), gomock.Any()).DoAndReturn(
		func(
			_ context.Context,
			_ session.Identity,
			_ mo.Option[string],
			marker session.CompactionEntry,
		) (session.Entry, error) {
			require.Equal(t, "public custom summary", marker.Summary)
			require.Equal(t, mo.Some("01-compact"), marker.Source.ExtensionID)
			return committed, nil
		},
	)

	// Act through real gRPC request handling and Host compaction orchestration.
	result, err := compactor.Compact(
		t.Context(), contextcompaction.TriggerManual, mo.Some("preserve decisions"), false, providerRequest,
	)

	// Assert the custom public result bypasses generation and commits through the Host owner.
	require.NoError(t, err)
	require.Equal(t, committed, result.Committed.MustGet())
}

// TestCompactionRejectsRealGRPCMalformedReplacement verifies public state validation stops composition.
func TestCompactionRejectsRealGRPCMalformedReplacement(t *testing.T) {
	t.Parallel()
	// Arrange one real extension process that corrupts Previous before a later public handler.
	extensionDirectory := t.TempDir()
	writeHandlerFixtureScript(t, extensionDirectory, "01-compact", handlerFixtureCompactionInvalidMode, "")
	factory := extensionruntime.NewFactory()
	managerController := gomock.NewController(t)
	reporter := extensionmanager.NewMockFailureReporter(managerController)
	reporter.EXPECT().ReportRuntimeFailure(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	extensions := extensionmanager.New(catalog.New(), factory, reporter)
	t.Cleanup(func() { require.NoError(t, extensions.Close()) })
	controller := gomock.NewController(t)
	treeOwner := sessiontree.New(
		sessiontree.NewMockActiveSession(controller), sessiontree.NewMockModelSelection(controller),
		sessiontree.NewMockModelRequester(controller), extensions,
	)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	contextSession := extensioncontext.NewMockSessionState(controller)
	contextSession.EXPECT().ContextSession().Return(identity).AnyTimes()
	contexts := extensioncontext.New(extensions, contextSession)
	extensionModels := extensionmodels.New(
		extensionmodels.NewMockCatalog(controller), extensionmodels.NewMockModelRequester(controller), contexts,
	)
	factory.BindHostServiceFactory(func(extensionID, runtimeID string) extensionsdk.HostService {
		return extensioncontroller.New(extensionModels, contexts, extensions, extensionID, runtimeID)
	})
	sessionState := contextcompaction.NewMockSessionState(controller)
	compactor := contextcompaction.New(sessionState)
	require.NoError(t, compactor.BindOrchestration(extensions, contexts, 20_000))
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
	providerRequest := modelexecution.ProviderRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: model.ReasoningChoiceOff,
		History: []agent.HistoryEntry{{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("history")),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		}},
		Tools: nil,
	}
	previous := session.CompactionEntry{
		Summary: "valid previous summary", FirstKeptEntryID: "kept",
		Source: session.CompactionSource{
			ExtensionID: mo.Some("previous-extension"), Model: mo.None[session.BranchSummaryModelSource](),
		},
		EstimatedCost: mo.None[session.EstimatedCost](), Details: mo.None[[]byte](),
	}
	marker := grpcUserEntry("marker", mo.Some("kept"), "unused", time.Unix(2, 0))
	marker.User = mo.None[session.UserMessage]()
	marker.Compaction = mo.Some(previous)
	entries := []session.Entry{
		grpcUserEntry("kept", mo.None[string](), "kept", time.Unix(1, 0)),
		marker,
		grpcUserEntry("tail", mo.Some("marker"), "tail", time.Unix(3, 0)),
	}
	sessionState.EXPECT().CompactionSnapshot().Return(contextcompaction.Snapshot{
		Identity: identity, ActiveLeafID: mo.Some("tail"), Entries: entries,
		Context: providerRequest.History, Previous: mo.Some(previous),
	})
	sessionState.EXPECT().ProjectSuffix(gomock.Any(), gomock.Any()).Return(providerRequest.History, nil).AnyTimes()

	// Act through real gRPC request replacement and Host validation.
	result, err := compactor.Compact(
		t.Context(), contextcompaction.TriggerManual, mo.None[string](), false, providerRequest,
	)

	// Assert no later capability or commit runs and the complete public validation cause is retained.
	require.True(t, result.Committed.IsNone())
	require.ErrorContains(t, err, "compaction input summary is empty")
	require.NotContains(t, err.Error(), "later compaction handler observed malformed nested marker")
	var failure *contextcompaction.FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, contextcompaction.FailureExtensionFailed, failure.Category)
}

// TestCompactionClassifiesRealGRPCHandlerCancellation verifies active-owner transport cancellation is typed.
func TestCompactionClassifiesRealGRPCHandlerCancellation(t *testing.T) {
	t.Parallel()
	// Arrange one real extension process whose request handler returns context cancellation.
	extensionDirectory := t.TempDir()
	writeHandlerFixtureScript(t, extensionDirectory, "01-compact", handlerFixtureCompactionCancelMode, "")
	factory := extensionruntime.NewFactory()
	managerController := gomock.NewController(t)
	reporter := extensionmanager.NewMockFailureReporter(managerController)
	reporter.EXPECT().ReportRuntimeFailure(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
	extensions := extensionmanager.New(catalog.New(), factory, reporter)
	t.Cleanup(func() { require.NoError(t, extensions.Close()) })
	controller := gomock.NewController(t)
	treeOwner := sessiontree.New(
		sessiontree.NewMockActiveSession(controller), sessiontree.NewMockModelSelection(controller),
		sessiontree.NewMockModelRequester(controller), extensions,
	)
	identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 1}
	contextSession := extensioncontext.NewMockSessionState(controller)
	contextSession.EXPECT().ContextSession().Return(identity).AnyTimes()
	contexts := extensioncontext.New(extensions, contextSession)
	extensionModels := extensionmodels.New(
		extensionmodels.NewMockCatalog(controller), extensionmodels.NewMockModelRequester(controller), contexts,
	)
	factory.BindHostServiceFactory(func(extensionID, runtimeID string) extensionsdk.HostService {
		return extensioncontroller.New(extensionModels, contexts, extensions, extensionID, runtimeID)
	})
	sessionState := contextcompaction.NewMockSessionState(controller)
	compactor := contextcompaction.New(sessionState)
	require.NoError(t, compactor.BindOrchestration(extensions, contexts, 20_000))
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
	providerRequest := modelexecution.ProviderRequest{
		Instructions: "instructions", Model: descriptor, ReasoningChoice: model.ReasoningChoiceOff,
		History: []agent.HistoryEntry{{
			Kind: agent.HistoryEntryUser, User: mo.Some(model.TextMessage("history")),
			Model: mo.None[model.Response](), ToolResult: mo.None[agent.ToolResult](),
		}},
		Tools: nil,
	}
	entries := []session.Entry{grpcUserEntry("kept", mo.None[string](), "kept", time.Unix(1, 0))}
	sessionState.EXPECT().CompactionSnapshot().Return(contextcompaction.Snapshot{
		Identity: identity, ActiveLeafID: mo.Some("kept"), Entries: entries,
		Context: providerRequest.History, Previous: mo.None[session.CompactionEntry](),
	})
	sessionState.EXPECT().ProjectSuffix(gomock.Any(), gomock.Any()).Return(providerRequest.History, nil).AnyTimes()

	// Act through the real SDK terminal and production Host runtime adapter.
	result, err := compactor.Compact(
		t.Context(), contextcompaction.TriggerManual, mo.None[string](), false, providerRequest,
	)

	// Assert an active owner treats handler transport cancellation as an extension failure.
	require.True(t, result.Committed.IsNone())
	require.False(t, result.Canceled)
	require.ErrorIs(t, err, context.Canceled)
	var failure *contextcompaction.FailureError
	require.ErrorAs(t, err, &failure)
	require.Equal(t, contextcompaction.FailureExtensionFailed, failure.Category)
}

// publicCompactionDescriptor creates the valid captured agent model exposed to the fixture.
func publicCompactionDescriptor() model.Descriptor {
	return model.Descriptor{
		Provider: "provider", Model: "model", Input: []model.InputModality{model.InputModalityText},
		ContextWindow: 100_000, MaxTokens: 10_000,
		ReasoningCapabilities: model.ReasoningCapabilities{
			Supported: false, Choices: []model.ReasoningChoice{model.ReasoningChoiceOff},
			Default: model.ReasoningChoiceOff,
		},
		ToolCapabilities: model.ToolCapabilities{
			StrictJSONSchema: false,
			Grammar:          model.GrammarCapabilities{Lark: false, Regex: false},
		},
		Pricing: mo.None[model.Pricing](),
	}
}
