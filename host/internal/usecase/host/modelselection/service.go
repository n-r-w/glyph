package modelselection

import (
	"context"
	"errors"
	"sync"

	"github.com/n-r-w/glyph/host/internal/domain/model"
	hostprogrammatic "github.com/n-r-w/glyph/host/internal/usecase/host/programmatic"
	hostui "github.com/n-r-w/glyph/host/internal/usecase/host/ui"
)

// Service owns shared selection admission, handler composition, and commit publication order.
type Service struct {
	// catalog owns selection resolution, validation, and state.
	catalog Catalog
	// publisher publishes authoritative committed selection state.
	publisher Publisher
	// mutex protects reservation, handler registrations, and optional handler dependencies.
	mutex sync.Mutex
	// active reports whether one selection is reserved.
	active bool
	// handlers preserves accepted registration order.
	handlers []registeredHandler
	// runtime invokes process handlers when handler support is bound.
	runtime Runtime
	// contexts issues current session-bound invocation contexts.
	contexts ContextIssuer
	// issues publishes ordinary handler diagnostics.
	issues IssueDelivery
}

var (
	_ hostprogrammatic.ModelSelection = (*Service)(nil)
	_ hostui.ModelSelection           = (*Service)(nil)
)

// preparedSelection owns one admitted private selection operation.
type preparedSelection struct {
	// owner coordinates composition, commit, and release.
	owner *Service
	// kind identifies the matching handler group.
	kind HandlerKind
	// target is the complete immutable starting target.
	target model.Selection
	// releaseOnce prevents duplicate admission release.
	releaseOnce sync.Once
}

// selectionResult is the private operation result projected to each consumer boundary.
type selectionResult struct {
	// selection is authoritative when committed is true.
	selection model.Selection
	// committed reports whether catalog state changed or confirmed a no-op.
	committed bool
	// issues contains ordered nonfatal handler diagnostics.
	issues []Issue
	// deliveryErr contains a post-commit client publication failure.
	deliveryErr error
	// err contains a pre-commit terminal failure.
	err error
}

// New creates one shared model-selection owner.
func New(catalog Catalog, publisher Publisher) *Service {
	return &Service{
		catalog: catalog, publisher: publisher, mutex: sync.Mutex{}, active: false,
		handlers: nil, runtime: nil, contexts: nil, issues: nil,
	}
}

// BindHandlers connects runtime invocation and issue delivery before registrations are committed.
func (s *Service) BindHandlers(runtime Runtime, contexts ContextIssuer, issues IssueDelivery) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.runtime = runtime
	s.contexts = contexts
	s.issues = issues
}

// prepareModel reserves selection and resolves the requested starting model.
func (s *Service) prepareModel(provider model.ProviderID, modelID model.ID) (*preparedSelection, error) {
	return s.prepare(HandlerKindModel, func() (model.Selection, error) {
		return s.catalog.ResolveModel(provider, modelID)
	})
}

// prepareReasoning reserves selection and resolves the requested starting reasoning choice.
func (s *Service) prepareReasoning(choice model.ReasoningChoice) (*preparedSelection, error) {
	return s.prepare(HandlerKindReasoning, func() (model.Selection, error) {
		return s.catalog.ResolveReasoning(choice)
	})
}

// prepare reserves the shared writer and resolves a non-mutating starting target.
func (s *Service) prepare(
	kind HandlerKind,
	resolve func() (model.Selection, error),
) (*preparedSelection, error) {
	s.mutex.Lock()
	if s.active {
		s.mutex.Unlock()
		return nil, &SelectionError{Code: ErrorCodeBusy, cause: errors.New("another model selection is active")}
	}
	s.active = true
	s.mutex.Unlock()

	target, resolveErr := resolve()
	if resolveErr != nil {
		s.release()
		return nil, resolveErr
	}
	return &preparedSelection{owner: s, kind: kind, target: target, releaseOnce: sync.Once{}}, nil
}

// run executes composition, final validation, atomic commit, and publication in order.
func (s *Service) run(ctx context.Context, kind HandlerKind, target model.Selection) selectionResult {
	final, issues, compositionErr := s.compose(ctx, kind, target)
	if compositionErr != nil {
		return selectionResult{
			selection: model.Selection{}, committed: false, issues: issues, deliveryErr: nil, err: compositionErr,
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return selectionResult{
			selection: model.Selection{}, committed: false, issues: issues, deliveryErr: nil, err: ctxErr,
		}
	}
	validationErr := s.catalog.ValidateSelection(ctx, final)
	if validationErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			validationErr = errors.Join(ctxErr, validationErr)
		} else {
			validationErr = classifyFinalValidation(validationErr)
		}
		return selectionResult{
			selection: model.Selection{}, committed: false, issues: issues, deliveryErr: nil, err: validationErr,
		}
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return selectionResult{
			selection: model.Selection{}, committed: false, issues: issues, deliveryErr: nil, err: ctxErr,
		}
	}
	preceding, committed, commitErr := s.catalog.CommitSelection(ctx, final)
	if commitErr != nil {
		return selectionResult{
			selection: model.Selection{}, committed: false, issues: issues, deliveryErr: nil, err: commitErr,
		}
	}
	result := selectionResult{
		selection: committed, committed: true, issues: issues, deliveryErr: nil, err: nil,
	}
	if preceding == committed {
		return result
	}
	wait, publicationErr := s.publisher.PublishSelection(committed)
	if publicationErr != nil {
		result.deliveryErr = publicationErr
		return result
	}
	result.deliveryErr = wait(ctx)
	return result
}

// Run executes one admitted private selection operation.
func (p *preparedSelection) Run(ctx context.Context) selectionResult {
	return p.owner.run(ctx, p.kind, p.target)
}

// Release frees shared selection admission exactly once.
func (p *preparedSelection) Release() {
	p.releaseOnce.Do(p.owner.release)
}

// release frees the shared reservation.
func (s *Service) release() {
	s.mutex.Lock()
	s.active = false
	s.mutex.Unlock()
}
