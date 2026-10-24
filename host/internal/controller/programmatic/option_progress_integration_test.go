//go:build integration

package programmatic

import (
	"context"
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/internal/operation"
	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestProgressPresencePrecedence verifies mapping failures do not fall through and zero payloads retain precedence.
func TestProgressPresencePrecedence(t *testing.T) {
	t.Parallel()
	// Arrange a real ordered writer and a delivery failure collector.
	sent := make(chan *programmaticv1.OpenResponse, 3)
	writer, err := operation.NewWriter(
		func(response *programmaticv1.OpenResponse) error { sent <- response; return nil },
	)
	require.NoError(t, err)
	var failures []error
	delivery := streamDelivery{
		context:  t.Context(),
		writer:   writer,
		registry: nil,
		fail:     func(err error) { failures = append(failures, err) },
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- writer.Run(ctx) }()
	t.Cleanup(func() { cancel(); <-done })
	progress := OperationProgress{
		AgentEvent:          mo.None[AgentEvent](),
		TreeNavigation:      mo.None[TreeNavigationProgress](),
		TreeNavigationRetry: mo.None[RetryProgress](),
		CompactionStage:     mo.None[string](),
	}

	// Act and assert absence retains its failure path, followed by successful empty compaction progress.
	require.ErrorContains(t, delivery.Progress("missing", progress), "payload is required")
	progress.CompactionStage = mo.Some("")
	require.NoError(t, delivery.Progress("compaction", progress))
	require.Empty(t, (<-sent).GetEvent().GetProgress().GetCompaction().GetStage())

	// Act and assert a present zero retry takes precedence over compaction.
	progress.TreeNavigationRetry = mo.Some(RetryProgress{})
	require.NoError(t, delivery.Progress("retry", progress))
	retry := (<-sent).GetEvent().GetProgress()
	require.True(t, retry.HasSessionTreeRetry())
	require.Zero(t, retry.GetSessionTreeRetry().GetCompletedAttempts())

	// Act and assert present navigation takes precedence over retry.
	progress.TreeNavigation = mo.Some(
		TreeNavigationProgress{Tree: SessionTree{Entries: nil, ActiveLeafID: mo.None[string]()}, ActiveBranch: nil},
	)
	require.NoError(t, delivery.Progress("navigation", progress))
	require.True(t, (<-sent).GetEvent().GetProgress().HasSessionTreeNavigation())

	// Act and assert an invalid first-priority agent payload reports its mapping error rather than selecting navigation.
	progress.AgentEvent = mo.Some(AgentEvent{})
	require.ErrorContains(t, delivery.Progress("agent", progress), "map Programmatic progress")
	require.Len(t, failures, 2)
	require.Empty(t, sent)
}
