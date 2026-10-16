package policy

import (
	"context"
	"fmt"
	"sync"

	"github.com/samber/mo"

	controller "github.com/n-r-w/glyph/plugins/extension/compaction/internal/controller/extension"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// Service coordinates conversation compaction.
type Service struct {
	// host supplies operation-scoped system services.
	host Host
	// retained is the recent-history target.
	retained int64
	// observations contains usage anchors by binding and model.
	observations map[observationKey]compaction.Anchor
	// observationsMu guards callback access to observations.
	observationsMu sync.RWMutex
}

var _ controller.Policy = (*Service)(nil)

// New assembles the conversation policy.
func New(host Host, retained int64) *Service {
	return &Service{
		host:           host,
		retained:       retained,
		observations:   make(map[observationKey]compaction.Anchor),
		observationsMu: sync.RWMutex{},
	}
}

// Compact coordinates compaction.
func (s *Service) Compact(ctx context.Context, input compaction.Input) (controller.Outcome, error) {
	outcome := controller.Outcome{State: mo.None[compaction.State](), Preserve: false, Canceled: false}
	tokens := s.estimateInput(input)
	if !input.NeedsCompaction(tokens) {
		outcome.Preserve = true
		return outcome, nil
	}
	request, err := compaction.SelectRequest(input, s.retained, tokens)
	if err != nil {
		return outcome, err
	}
	s.forgetObservations(input.Binding)
	state := compaction.State{Original: request, Current: request, Result: mo.None[compaction.Result]()}
	for _, participant := range input.Requests {
		if err = ctx.Err(); err != nil {
			break
		}
		var action compaction.RequestAction
		action, err = s.host.Request(ctx, participant, state)
		if err != nil {
			break
		}
		if err = action.Validate(); err != nil {
			break
		}
		if action.Cancel {
			outcome.Canceled = true
			break
		}
		next := state
		if replacement, present := action.Request.Get(); present {
			next.Current = replacement
		}
		if replacement, present := action.Result.Get(); present {
			if err = s.validateCandidate(ctx, input, replacement); err != nil {
				break
			}
			next.Result = mo.Some(replacement)
		} else if action.ResultDisposition == compaction.ResultClear {
			next.Result = mo.None[compaction.Result]()
		}
		state = next
	}
	if err == nil && !outcome.Canceled {
		err = s.complete(ctx, input, &state, &outcome)
	}
	outcome.State = mo.Some(state)
	return outcome, err
}

// complete finishes replacement composition.
func (s *Service) complete(
	ctx context.Context,
	input compaction.Input,
	state *compaction.State,
	outcome *controller.Outcome,
) error {
	if state.Result.IsNone() {
		if len(input.Generators) != 1 {
			return fmt.Errorf("compaction requires exactly one generator, found %d", len(input.Generators))
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		generated, err := s.host.Generate(ctx, input.Generators[0], *state)
		if err != nil {
			return err
		}
		if projectionErr := s.validateCandidate(ctx, input, generated); projectionErr != nil {
			return projectionErr
		}
		state.Result = mo.Some(generated)
	}
	original := state.Result.OrEmpty()
	for _, participant := range input.Results {
		if err := ctx.Err(); err != nil {
			return err
		}
		action, err := s.host.InvokeResultHandler(ctx, participant, *state, original)
		if err != nil {
			return err
		}
		if actionErr := action.Validate(); actionErr != nil {
			return actionErr
		}
		if action.Cancel {
			outcome.Canceled = true
			return nil
		}
		if replacement, present := action.Result.Get(); present {
			if projectionErr := s.validateCandidate(ctx, input, replacement); projectionErr != nil {
				return projectionErr
			}
			state.Result = mo.Some(replacement)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.host.Commit(ctx, state.Result.OrEmpty())
}

// validateCandidate checks whether a compaction candidate fits the conversation input budget.
func (s *Service) validateCandidate(ctx context.Context, input compaction.Input, candidate compaction.Result) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	history, err := s.host.Project(ctx, candidate)
	if err != nil {
		return fmt.Errorf("project compaction candidate: %w", err)
	}
	input.History = history
	return s.ValidateOutgoing(ctx, input)
}

// ValidateOutgoing checks the captured conversation budget.
func (s *Service) ValidateOutgoing(ctx context.Context, input compaction.Input) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tokens := compaction.EstimateInput(input.History, input.Instructions, input.ToolText)
	budget := input.Model.Window - input.Model.Reserve
	if tokens > budget {
		return fmt.Errorf(
			"conversation input budget exceeded: estimated %d tokens, budget %d (context %d, response reserve %d)",
			tokens,
			budget,
			input.Model.Window,
			input.Model.Reserve,
		)
	}
	return nil
}
