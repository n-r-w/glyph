package policy

import (
	"context"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// observationKey is the identity of a stored usage observation.
type observationKey struct {
	// binding identifies issued session access.
	binding compaction.Binding
	// provider identifies the configured provider.
	provider string
	// model identifies the configured model.
	model string
}

// Observe records completed conversation input for continuation estimates.
func (s *Service) Observe(ctx context.Context, observation compaction.Observation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	key := observationKey{
		binding:  observation.Input.Binding,
		provider: observation.Input.Model.Provider,
		model:    observation.Input.Model.ID,
	}
	anchor := compaction.NewAnchor(observation)
	s.observationsMu.Lock()
	defer s.observationsMu.Unlock()
	if value, present := anchor.Get(); present {
		s.observations[key] = value
	} else {
		delete(s.observations, key)
	}
	return nil
}

// estimateInput selects the conversation input estimate.
func (s *Service) estimateInput(input compaction.Input) int64 {
	key := observationKey{binding: input.Binding, provider: input.Model.Provider, model: input.Model.ID}
	s.observationsMu.RLock()
	anchor, present := s.observations[key]
	s.observationsMu.RUnlock()
	if present {
		if tokens, continuation := anchor.EstimateContinuation(input).Get(); continuation {
			return tokens
		}
	}
	return compaction.EstimateInput(input.History, input.Instructions, input.ToolText)
}

// forgetObservations clears stored usage observations.
func (s *Service) forgetObservations(binding compaction.Binding) {
	s.observationsMu.Lock()
	defer s.observationsMu.Unlock()
	for key := range s.observations {
		if key.binding == binding {
			delete(s.observations, key)
		}
	}
}
