package modelexecution

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/mo"

	"github.com/n-r-w/glyph/host/internal/domain/agent"
	"github.com/n-r-w/glyph/host/internal/domain/model"
	agentrun "github.com/n-r-w/glyph/host/internal/usecase/agent/run"
	"github.com/n-r-w/glyph/host/internal/usecase/host/extensioncontext"
	"github.com/n-r-w/glyph/host/internal/usecase/host/sessiontree"
)

// Service owns one-attempt logical model execution for Host consumers.
type Service struct {
	// catalog resolves logical model selections to raw provider bindings.
	catalog CatalogResolver
}

var (
	_ agentrun.ModelRuntime           = (*Service)(nil)
	_ agentrun.ModelProvider          = (*Service)(nil)
	_ extensioncontext.ModelRequester = (*Service)(nil)
	_ sessiontree.ModelRequester      = (*Service)(nil)
)

// New creates the logical model-execution owner.
func New(catalog CatalogResolver) *Service {
	return &Service{catalog: catalog}
}

// Snapshot returns the active logical model snapshot with this service as its provider.
func (s *Service) Snapshot() agentrun.RequestSnapshot {
	// binding keeps the model and provider selection atomic.
	binding := s.catalog.ActiveBinding()
	return agentrun.RequestSnapshot{
		Model:           binding.Model,
		ReasoningChoice: binding.ReasoningChoice,
		Provider:        s,
	}
}

// Stream executes one logical Agent Core request as exactly one raw provider attempt.
func (s *Service) Stream(
	ctx context.Context,
	request agentrun.ModelRequest,
	handle agentrun.StreamHandler,
) error {
	// selection reconstructs the exact catalog key captured by Agent Core.
	selection := model.Selection{
		Provider:        request.Model.Provider,
		Model:           request.Model.Model,
		ReasoningChoice: request.ReasoningChoice,
	}
	// binding selects one raw provider without configured-request credential policy.
	binding, err := s.catalog.ResolveBinding(selection)
	if err != nil {
		return err
	}
	// ownedHistory prevents the raw provider from mutating Agent Core history.
	ownedHistory, err := cloneHistory(request.History)
	if err != nil {
		return err
	}
	return binding.Provider.Stream(ctx, ProviderRequest{
		Instructions:    request.Instructions,
		Model:           binding.Model,
		ReasoningChoice: binding.ReasoningChoice,
		History:         ownedHistory,
		Tools:           request.Tools,
	}, func(event StreamEvent) error {
		return handle(logicalStreamEvent(event))
	})
}

// Request executes one configured request and returns its detached terminal response.
func (s *Service) Request(
	ctx context.Context,
	selection model.Selection,
	instructions string,
	history []agent.HistoryEntry,
) (model.Response, error) {
	// binding preserves exact configured selection validation and credential preflight.
	binding, err := s.catalog.ResolveConfiguredBinding(ctx, selection)
	if err != nil {
		return model.Response{}, err
	}
	// ownedHistory prevents the raw provider from mutating caller history.
	ownedHistory, err := cloneHistory(history)
	if err != nil {
		return model.Response{}, err
	}
	// terminal retains a detached response while incremental events are reduced.
	terminal := mo.None[model.Response]()
	// streamErr contains the one raw provider-attempt result.
	streamErr := binding.Provider.Stream(ctx, ProviderRequest{
		Instructions:    instructions,
		Model:           binding.Model,
		ReasoningChoice: binding.ReasoningChoice,
		History:         ownedHistory,
		Tools:           nil,
	}, func(event StreamEvent) error {
		if event.Kind == StreamEventDone || event.Kind == StreamEventError {
			response, present := event.Response.Get()
			if !present {
				return errors.New("model request terminal event has no response")
			}
			terminal = mo.Some(response.Clone())
		}
		return nil
	})
	if streamErr != nil {
		return model.Response{}, fmt.Errorf("execute model request: %w", streamErr)
	}
	// response is present only after one valid terminal event.
	response, present := terminal.Get()
	if !present {
		return model.Response{}, errors.New("model request ended without a terminal response")
	}
	return response, nil
}

// cloneHistory validates and transfers immutable history ownership to one raw attempt.
func cloneHistory(history []agent.HistoryEntry) ([]agent.HistoryEntry, error) {
	// ownedHistory receives validated deep clones in source order.
	ownedHistory := make([]agent.HistoryEntry, len(history))
	for index := range history {
		clone, err := history[index].ValidatedClone()
		if err != nil {
			return nil, fmt.Errorf("validate model request history: %w", err)
		}
		ownedHistory[index] = clone
	}
	return ownedHistory, nil
}

// logicalStreamEvent converts one raw event into the independent Agent Core consumer contract.
func logicalStreamEvent(event StreamEvent) agentrun.StreamEvent {
	// logicalKind remains the Agent Core invalid zero value for unsupported raw kinds.
	logicalKind := agentrun.StreamEventKind(0)
	switch event.Kind {
	case StreamEventContentStart:
		logicalKind = agentrun.StreamEventContentStart
	case StreamEventTextDelta:
		logicalKind = agentrun.StreamEventTextDelta
	case StreamEventContentEnd:
		logicalKind = agentrun.StreamEventContentEnd
	case StreamEventToolCallStart:
		logicalKind = agentrun.StreamEventToolCallStart
	case StreamEventToolCallDelta:
		logicalKind = agentrun.StreamEventToolCallDelta
	case StreamEventToolCallEnd:
		logicalKind = agentrun.StreamEventToolCallEnd
	case StreamEventDone:
		logicalKind = agentrun.StreamEventDone
	case StreamEventError:
		logicalKind = agentrun.StreamEventError
	}
	return agentrun.StreamEvent{
		Kind:     logicalKind,
		Position: event.Position,
		Content:  event.Content,
		Delta:    event.Delta,
		Preview:  event.Preview,
		ToolCall: event.ToolCall,
		Response: event.Response,
	}
}
