// Package host adapts public Extension SDK operations for compaction use cases.
package host

import (
	"context"
	"errors"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	controller "github.com/n-r-w/glyph/plugins/extension/compaction/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/usecase/policy"
	sdk "github.com/n-r-w/glyph/sdk/plugins/extension/v1"
)

// Service supplies invocation-scoped Host operations through the SDK.
type Service struct{}

var _ policy.Host = (*Service)(nil)

// Request invokes a request handler.
func (s *Service) Request(
	ctx context.Context,
	participant compaction.Participant,
	state compaction.State,
) (compaction.RequestAction, error) {
	binding, present := sdk.FindCompactionPolicy(ctx)
	if !present {
		return compaction.RequestAction{}, errors.New("compaction policy authority is required")
	}
	operation, err := binding.StartRequestHandler(
		ctx,
		controller.EncodeParticipant(participant),
		controller.EncodeInvocation(state),
	)
	if err != nil {
		return compaction.RequestAction{}, err
	}
	action, err := operation.Wait(ctx)
	if err != nil {
		return compaction.RequestAction{}, err
	}
	return controller.DecodeRequestAction(action), nil
}

// Generate invokes the generator.
func (s *Service) Generate(
	ctx context.Context,
	participant compaction.Participant,
	state compaction.State,
) (compaction.Result, error) {
	binding, present := sdk.FindCompactionPolicy(ctx)
	if !present {
		return compaction.Result{}, errors.New("compaction policy authority is required")
	}
	operation, err := binding.StartGenerator(
		ctx,
		controller.EncodeParticipant(participant),
		controller.EncodeInvocation(state),
	)
	if err != nil {
		return compaction.Result{}, err
	}
	result, err := operation.Wait(ctx)
	if err != nil {
		return compaction.Result{}, err
	}
	if result == nil {
		return compaction.Result{}, errors.New("compaction generator result is required")
	}
	return controller.DecodeResult(result), nil
}

// InvokeResultHandler invokes a result handler.
func (s *Service) InvokeResultHandler(
	ctx context.Context,
	participant compaction.Participant,
	state compaction.State,
	original compaction.Result,
) (compaction.ResultAction, error) {
	binding, present := sdk.FindCompactionPolicy(ctx)
	if !present {
		return compaction.ResultAction{}, errors.New("compaction policy authority is required")
	}
	invocation := pb.CompactionResultInvocation_builder{
		OriginalRequest: controller.EncodeRequest(state.Original),
		CurrentRequest:  controller.EncodeRequest(state.Current),
		OriginalResult:  controller.EncodeResult(original),
		CurrentResult:   controller.EncodeResult(state.Result.OrEmpty()),
	}.Build()
	operation, err := binding.StartResultHandler(ctx, controller.EncodeParticipant(participant), invocation)
	if err != nil {
		return compaction.ResultAction{}, err
	}
	action, err := operation.Wait(ctx)
	if err != nil {
		return compaction.ResultAction{}, err
	}
	return controller.DecodeResultAction(action), nil
}

// Project returns a preview of the replacement history.
func (s *Service) Project(ctx context.Context, candidate compaction.Result) ([]compaction.Entry, error) {
	binding, present := sdk.FindCompactionPolicy(ctx)
	if !present {
		return nil, errors.New("compaction policy authority is required")
	}
	operation, err := binding.StartProjectReplacement(ctx, controller.EncodeResult(candidate))
	if err != nil {
		return nil, err
	}
	projection, err := operation.Wait(ctx)
	if err != nil {
		return nil, err
	}
	return controller.DecodeHistory(projection.GetHistory()), nil
}

// Commit persists a compaction replacement.
func (s *Service) Commit(ctx context.Context, candidate compaction.Result) error {
	binding, present := sdk.FindCompactionPolicy(ctx)
	if !present {
		return errors.New("compaction policy authority is required")
	}
	operation, err := binding.StartReplaceHistory(ctx, controller.EncodeResult(candidate))
	if err != nil {
		return err
	}
	result, err := operation.Wait(ctx)
	if err != nil {
		return err
	}
	if result.GetError() != "" {
		return errors.New(result.GetError())
	}
	if result.GetCommitted().GetEntryId() == "" {
		return errors.New("compaction replacement returned no durable receipt")
	}
	return nil
}
