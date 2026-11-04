//go:build !integration

package extensioncontext

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/domain/session"
)

// TestQueuedMessageBinding checks capture, delayed guarded persistence, and stale-generation discard.
func TestQueuedMessageBinding(t *testing.T) {
	t.Parallel()
	for _, change := range []string{
		"current", "runtime", "incarnation", "commit-runtime", "commit-session", "persistence", "committed-delivery",
	} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			// Arrange a captured binding independent of invocation accounting.
			mocks := gomock.NewController(t)
			runtime := NewMockRuntimeState(mocks)
			sessions := NewMockSessionState(mocks)
			identity := session.Identity{ID: "session", WorkingDirectory: "/project", Incarnation: 3}
			instance := "runtime"
			runtime.EXPECT().
				ContextRuntime("extension").
				DoAndReturn(func(string) (string, bool) { return instance, true }).
				AnyTimes()
			sessions.EXPECT().ContextSession().DoAndReturn(func() session.Identity { return identity }).AnyTimes()
			service := New(runtime, sessions)
			issued, err := service.IssueContext("extension")
			require.NoError(t, err)
			message, err := service.CaptureExtensionMessage(t.Context(), "extension", instance,
				extension.ContextRef{ID: issued.ID, RuntimeInstanceID: instance, SessionID: identity.ID},
				"kind", "exact\ntext", session.ClientVisibilityHidden)
			require.NoError(t, err)
			expected := identity
			cause := errors.New("disk append failed: complete original cause")
			switch change {
			case "runtime":
				instance = "replacement"
			case "incarnation":
				identity.Incarnation++
			default:
				runtime.EXPECT().
					BeginContextCommit("extension", "runtime").
					DoAndReturn(func(string, string) (func(), error) {
						if change == "commit-runtime" {
							return nil, staleBinding("extension", "runtime replaced during commit")
						}
						return func() {}, nil
					}).
					MaxTimes(1)
				sessions.EXPECT().AppendExtensionMessage(gomock.Any(), expected,
					session.ExtensionMessage{
						ExtensionID: "extension",
						EntryType:   "kind",
						Text:        "exact\ntext",
						Visibility:  session.ClientVisibilityHidden,
					}, gomock.Any()).
					DoAndReturn(
						func(
							_ context.Context, _ session.Identity, _ session.ExtensionMessage, guard ContextCommitGuard,
						) (session.Entry, error) {
							if change == "commit-session" {
								return session.Entry{}, session.ErrUnavailable
							}
							release, guardErr := guard()
							if guardErr != nil {
								return session.Entry{}, guardErr
							}
							defer release()
							if change == "persistence" {
								return session.Entry{}, cause
							}
							if change == "committed-delivery" {
								return session.Entry{
									ID:               "committed",
									ParentID:         mo.None[string](),
									CreatedAt:        time.Time{},
									Information:      mo.None[session.Information](),
									User:             mo.None[session.UserMessage](),
									Model:            mo.None[session.ModelResponse](),
									EstimatedCost:    mo.None[session.EstimatedCost](),
									ToolResult:       mo.None[session.ToolResult](),
									Extension:        mo.None[session.ExtensionEnvelope](),
									ExtensionMessage: mo.None[session.ExtensionMessage](),
									BranchSummary:    mo.None[session.BranchSummaryEntry](),
									Compaction:       mo.None[session.CompactionEntry](),
								}, cause
							}
							return session.Entry{}, nil
						})
			}

			if change == "runtime" || change == "incarnation" {
				_, captureErr := service.CaptureExtensionMessage(t.Context(), "extension", "runtime",
					extension.ContextRef{ID: issued.ID, RuntimeInstanceID: "runtime", SessionID: "session"},
					"kind", "stale", session.ClientVisibilityHidden)
				require.Error(t, captureErr)
			}

			// Act after the originating invocation could have completed.
			err = service.DeliverExtensionMessage(t.Context(), message)

			// Assert stale work is discarded and persistence failures retain their cause.
			if change == "persistence" || change == "committed-delivery" {
				require.ErrorIs(t, err, cause)
				require.ErrorContains(t, err, cause.Error())
			} else {
				require.NoError(t, err)
			}
		})
	}
}
