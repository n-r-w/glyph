package contextcompaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
)

// RecoverOverflow compacts one provider-rejected request before a changed replacement attempt.
func (s *Service) RecoverOverflow(
	ctx context.Context,
	request modelexecution.ProviderRequest,
) (modelexecution.ProviderRequest, error) {
	return s.compactAndRebuild(ctx, TriggerOverflow, mo.None[string](), true, cloneProviderRequest(request))
}

// compactAndRebuild coordinates compaction and reads the exact committed model-context projection.
func (s *Service) compactAndRebuild(
	ctx context.Context,
	trigger Trigger,
	instructions mo.Option[string],
	retryIntent bool,
	request modelexecution.ProviderRequest,
) (modelexecution.ProviderRequest, error) {
	result, err := s.Compact(ctx, trigger, instructions, retryIntent, request)
	if err != nil {
		return modelexecution.ProviderRequest{}, err
	}
	if result.Canceled {
		return modelexecution.ProviderRequest{}, errors.New("compaction was canceled by an extension handler")
	}
	if result.Committed.IsNone() {
		return modelexecution.ProviderRequest{}, errors.New("compaction completed without a committed entry")
	}
	snapshot := s.sessions.CompactionSnapshot()
	if snapshot.Identity != s.sessions.ContextSession() {
		return modelexecution.ProviderRequest{}, errors.New("active session changed after compaction")
	}
	request.History = snapshot.Context
	rebuilt := cloneProviderRequest(request)
	estimate, err := fallbackEstimate(rebuilt)
	if err != nil {
		return modelexecution.ProviderRequest{}, fmt.Errorf("estimate outbound context after compaction: %w", err)
	}
	inputBudget := request.Model.ContextWindow - request.Model.MaxTokens
	if estimate > inputBudget {
		return modelexecution.ProviderRequest{}, fmt.Errorf(
			"outbound context estimate %d after compaction exceeds input budget %d", estimate, inputBudget,
		)
	}
	return rebuilt, nil
}
