package contextcompaction

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	controllerextension "github.com/n-r-w/glyph/host/internal/controller/extension"
	extensiondomain "github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/modelexecution"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// CompactManual builds the current agent request and runs the shared compaction chain.
func (s *Service) CompactManual(
	ctx context.Context,
	instructions mo.Option[string],
) (ui.ManualCompactionResult, error) {
	result, err := s.compactManual(ctx, instructions)
	return ui.ManualCompactionResult{Committed: result.Committed, Canceled: result.Canceled}, err
}

// CompactExtension validates the issued binding and maps the shared operation to the Extension controller contract.
func (s *Service) CompactExtension(
	ctx context.Context,
	extensionID string,
	runtimeID string,
	reference extensiondomain.ContextRef,
	instructions mo.Option[string],
) (controllerextension.CompactionResult, error) {
	if s.contexts == nil {
		return controllerextension.CompactionResult{}, errors.New("compaction context validation is not bound")
	}
	if err := s.contexts.ValidateContext(extensionID, runtimeID, reference); err != nil {
		return controllerextension.CompactionResult{}, err
	}
	result, err := s.compactManual(ctx, instructions)
	return controllerextension.CompactionResult{Committed: result.Committed, Canceled: result.Canceled}, err
}

// CompactProgrammatic maps the shared manual operation to the Programmatic consumer contract.
func (s *Service) CompactProgrammatic(
	ctx context.Context,
	instructions mo.Option[string],
) (programmatic.ManualCompactionResult, error) {
	result, err := s.compactManual(ctx, instructions)
	return programmatic.ManualCompactionResult{Committed: result.Committed, Canceled: result.Canceled}, err
}

// compactManual builds the current agent request and runs the shared compaction chain.
func (s *Service) compactManual(
	ctx context.Context,
	instructions mo.Option[string],
) (OperationResult, error) {
	if s.manualModel == nil || s.manualTools == nil {
		return emptyOperationResult(), errors.New("manual compaction request sources are not bound")
	}
	modelSnapshot := s.manualModel.ManualCompactionModel()
	snapshot := s.sessions.CompactionSnapshot()
	request := modelexecution.ProviderRequest{
		Instructions: s.manualInstructions, Model: modelSnapshot.Model,
		ReasoningChoice: modelSnapshot.ReasoningChoice, History: snapshot.Context, Tools: s.manualTools.Tools(),
	}
	return s.Compact(ctx, TriggerManual, instructions, false, request)
}

// PrepareContext checks the initial request against its captured input budget.
func (s *Service) PrepareContext(
	ctx context.Context,
	request modelexecution.ProviderRequest,
) (modelexecution.ProviderRequest, error) {
	owned := cloneProviderRequest(request)
	estimate, err := s.EstimateContext(owned)
	if err != nil {
		return modelexecution.ProviderRequest{}, fmt.Errorf("estimate conversation context: %w", err)
	}
	inputBudget := owned.Model.ContextWindow - owned.Model.MaxTokens
	if inputBudget < 0 {
		return modelexecution.ProviderRequest{}, errors.New("model response budget exceeds its context window")
	}
	if estimate <= inputBudget {
		return owned, nil
	}
	return s.compactAndRebuild(ctx, TriggerThreshold, mo.None[string](), false, owned)
}

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
