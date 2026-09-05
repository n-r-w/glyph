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

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	"github.com/n-r-w/glyph/host/internal/domain/session"
	"github.com/n-r-w/glyph/host/internal/infra/plugins/extension/catalog"
	extensionruntime "github.com/n-r-w/glyph/host/internal/infra/plugins/extension/runtime"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	extensionmanager "github.com/n-r-w/glyph/host/internal/usecase/host/extensionruntime"
	"github.com/n-r-w/glyph/host/internal/usecase/host/providers"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessionnavigation"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
	"github.com/n-r-w/glyph/host/internal/usecase/host/startup"
	toolservice "github.com/n-r-w/glyph/host/internal/usecase/host/tools"
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
			extensions := extensionmanager.New(
				catalog.New(),
				extensionruntime.NewFactory(),
				func(context.Context, extension.RuntimeFailure) error { return nil },
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
			startupService := startup.New(extensions, toolservice.New(extensions), service)
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
			if mode != summaryControlClearMode {
				active.EXPECT().
					CommitNavigation(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, command sessiontree.CommitCommand) (session.Tree, error) {
						assert.Equal(
							t,
							mo.Some(summaryControlProducer),
							command.BranchSummary.OrEmpty().Source.ExtensionID,
						)
						committed := tree.Clone()
						require.NoError(t, committed.SetActiveLeaf(mo.Some("root")))
						require.NoError(t, committed.Add(grpcSummaryEntry()))
						return committed, nil
					})
			}

			// Act through the real Extension Contract; the model provider has no stream expectation.
			result, err := service.NavigateTree(t.Context(), sessionnavigation.Request{
				TargetEntryID: "user",
				SummaryMode:   sessionnavigation.SummaryModeSummarize,
				CustomFocus:   mo.None[string](),
			})

			// Assert replacement avoids credential checks, while clearing performs one check before any commit.
			if mode == summaryControlClearMode {
				require.ErrorIs(t, err, sessionnavigation.ErrCredentialUnavailable)
				assert.Contains(t, err.Error(), "resolve credentials: missing API key")
				assert.Equal(t, int64(1), checks.Load())
			} else {
				require.NoError(t, err)
				assert.False(t, result.Canceled)
				assert.Equal(t, int64(0), checks.Load())
				require.Len(t, result.Issues, 3)
			}
		})
	}
}
