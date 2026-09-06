//go:build integration

package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	controllerui "github.com/n-r-w/glyph/host/internal/controller/ui"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/lifecycle"
	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// TestRealExtensionChecksCredentialsOnlyAfterClearing verifies real catalog dispatch and credential-check counts.
func TestRealExtensionChecksCredentialsOnlyAfterClearing(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{summaryControlExtensionMode, summaryControlMissingMode, summaryControlClearMode} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()

			// Arrange a real extension and catalog whose configured summary credentials always fail.
			directory := t.TempDir()
			writeHandlerFixtureScript(t, directory, "control", mode, "")
			reporter := extensionmanager.NewMockFailureReporter(gomock.NewController(t))
			reporter.EXPECT().ReportRuntimeFailure(gomock.Any(), gomock.Any()).AnyTimes().Return(nil)
			extensions := extensionmanager.New(
				catalog.New(),
				extensionruntime.NewFactory(),
				reporter,
			)
			t.Cleanup(extensions.Close)
			controller := gomock.NewController(t)
			active := sessiontree.NewMockActiveSession(controller)
			provider := agentrun.NewMockModelProvider(controller)
			credentials := providers.NewMockCredentialChecker(controller)
			var checks atomic.Int64
			credentials.EXPECT().CheckCredentials(gomock.Any()).DoAndReturn(func(context.Context) error {
				checks.Add(1)
				return errors.New("resolve credentials: missing API key")
			}).AnyTimes()
			selection := model.Selection{
				Provider:        "openai-codex",
				Model:           "unused",
				ReasoningChoice: model.ReasoningChoiceOff,
			}
			models, err := providers.New([]providers.Entry{{
				Descriptor: model.Descriptor{
					Provider:      selection.Provider,
					Model:         selection.Model,
					Input:         []model.InputModality{model.InputModalityText},
					ContextWindow: 131072,
					MaxTokens:     16384,
					ReasoningCapabilities: model.ReasoningCapabilities{
						Supported: false,
						Choices:   []model.ReasoningChoice{model.ReasoningChoiceOff},
						Default:   model.ReasoningChoiceOff,
					},
					ToolCapabilities: model.ToolCapabilities{},
					Pricing:          mo.None[model.Pricing](),
				}, Provider: provider, CredentialChecker: credentials, Authentication: nil,
			}}, selection)
			require.NoError(t, err)
			service := sessiontree.New(active, models, extensions)
			contexts := sessiontree.NewMockContextIssuer(controller)
			contexts.EXPECT().IssueContext("control").DoAndReturn(func(extensionID string) (extension.Context, error) {
				instance, _ := extensions.ContextRuntime(extensionID)
				return extension.Context{
					ID:                "binding",
					ExtensionID:       extensionID,
					RuntimeInstanceID: instance,
					SessionID:         "session",
					WorkingDirectory:  "/project",
				}, nil
			}).AnyTimes()
			service.BindContextIssuer(contexts)
			startupService := startup.New(
				extensions,
				toolservice.New(extensions),
				service,
				lifecycle.New(extensions, nil),
			)
			report, err := startupService.Load(
				t.Context(),
				startup.Request{DataDirectory: "", ExtensionDirectory: directory},
			)
			require.NoError(t, err)
			require.Empty(t, report.Issues)
			extensions.Activate(t.Context())
			tree := grpcNavigationTree(t)
			active.EXPECT().Tree().Return(tree)
			active.EXPECT().SessionID().Return("session")
			publisher := func(session.Tree) error { return nil }
			if mode != summaryControlClearMode {
				active.EXPECT().
					CommitNavigation(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(
						_ context.Context,
						command sessiontree.CommitCommand,
						_ func(session.Tree) error,
					) (sessiontree.NavigationCommit, error) {
						assert.Equal(
							t,
							mo.Some(summaryControlProducer),
							command.BranchSummary.OrEmpty().Source.ExtensionID,
						)
						committed := tree.Clone()
						require.NoError(t, committed.SetActiveLeaf(mo.Some("root")))
						require.NoError(t, committed.Add(grpcSummaryEntry()))
						return sessiontree.NavigationCommit{
							Committed: true, Tree: committed, CreatedSummary: mo.Some(grpcSummaryEntry()),
						}, nil
					})
			}

			// Act through the real Extension Contract; the model provider has no stream expectation.
			result, err := service.NavigateUI(t.Context(), hostui.NavigationIntent{
				TargetEntryID: "user",
				SummaryMode:   controllerui.SummaryModeSummarize,
				CustomFocus:   mo.None[string](),
			}, publisher)

			// Assert replacement avoids credential checks, while clearing performs one check before any commit.
			if mode == summaryControlClearMode {
				require.ErrorIs(t, err, sessiontree.ErrCredentialUnavailable)
				assert.Contains(t, err.Error(), "resolve credentials: missing API key")
				assert.Equal(t, int64(1), checks.Load())
			} else {
				require.NoError(t, err)
				assert.True(t, result.Committed.IsSome())
				assert.Equal(t, int64(0), checks.Load())
				require.Len(t, result.Issues, 3)
			}
		})
	}
}
