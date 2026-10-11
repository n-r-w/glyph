// Package modelexecution owns Host logical model execution and raw provider attempts.
package modelexecution

import (
	"context"

	"github.com/samber/mo"

	controllerextension "github.com/n-r-w/glyph/host/internal/controller/extension"
	"github.com/n-r-w/glyph/host/internal/domain/extension"
	"github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	"github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// BindManualCompaction supplies the manual input sources and preparation dependency.
func (s *Service) BindManualCompaction(
	preparation ManualCompactionPreparation,
	tools ManualCompactionTools,
	instructions string,
) {
	s.manualPreparation = preparation
	s.manualTools = tools
	s.manualInstructions = instructions
}

// compactManualPolicy captures model input for the extension-owned manual operation.
func (s *Service) compactManualPolicy(
	ctx context.Context,
	instructions mo.Option[string],
) (ManualCompactionResult, error) {
	binding := s.catalog.ActiveBinding()
	input := ProviderRequest{
		Instructions: s.manualInstructions, Model: binding.Model,
		ReasoningChoice: binding.ReasoningChoice, History: nil, Tools: s.manualTools.Tools(),
	}
	return s.manualPreparation.CompactManualPolicy(ctx, input, instructions)
}

// CompactManual supplies the UI manual-compaction outcome.
func (s *Service) CompactManual(
	ctx context.Context,
	instructions mo.Option[string],
) (ui.ManualCompactionResult, error) {
	result, err := s.compactManualPolicy(ctx, instructions)
	return ui.ManualCompactionResult{Committed: result.Committed, Canceled: result.Canceled}, err
}

// CompactProgrammatic supplies the Programmatic manual-compaction outcome.
func (s *Service) CompactProgrammatic(
	ctx context.Context,
	instructions mo.Option[string],
) (programmatic.ManualCompactionResult, error) {
	result, err := s.compactManualPolicy(ctx, instructions)
	return programmatic.ManualCompactionResult{Committed: result.Committed, Canceled: result.Canceled}, err
}

// CompactExtension supplies the session-bound Extension manual-compaction outcome.
func (s *Service) CompactExtension(
	ctx context.Context,
	extensionID, runtimeID string,
	reference extension.ContextRef,
	instructions mo.Option[string],
) (controllerextension.CompactionResult, error) {
	if err := s.manualPreparation.ValidateContext(extensionID, runtimeID, reference); err != nil {
		return controllerextension.CompactionResult{}, err
	}
	result, err := s.compactManualPolicy(ctx, instructions)
	return controllerextension.CompactionResult{Committed: result.Committed, Canceled: result.Canceled}, err
}
