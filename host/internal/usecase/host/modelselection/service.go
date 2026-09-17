package modelselection

import (
	"context"
	"errors"
	"sync"

	"github.com/samber/mo"

	extensioncontroller "github.com/n-r-w/glyph/host/internal/controller/extension"
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
	// protection validates and protects bound extension selection commits.
	protection BindingProtection
	// observer delivers post-commit selection lifecycle events.
	observer Observer
}

var (
	_ extensioncontroller.ModelSelection = (*Service)(nil)
	_ hostprogrammatic.ModelSelection    = (*Service)(nil)
	_ hostui.ModelSelection              = (*Service)(nil)
)

// preparedSelection owns one admitted private selection operation.
type preparedSelection struct {
	// owner coordinates composition, commit, and release.
	owner *Service
	// kind identifies the matching handler group.
	kind HandlerKind
	// target is the complete immutable starting target.
	target model.Selection
	// binding contains protection identity only for extension-initiated selection.
	binding mo.Option[Binding]
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
		handlers: nil, runtime: nil, contexts: nil, issues: nil, protection: nil, observer: nil,
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

// BindProtection binds extension context protection before extension selection becomes available.
func (s *Service) BindProtection(protection BindingProtection) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.protection = protection
}

// BindObserver installs selection lifecycle observation before selection operations become available.
func (s *Service) BindObserver(observer Observer) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.observer = observer
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
	return &preparedSelection{
		owner: s, kind: kind, target: target, binding: mo.None[Binding](), releaseOnce: sync.Once{},
	}, nil
}

// run executes composition, final validation, atomic commit, and publication in order.
func (s *Service) run(
	ctx context.Context,
	kind HandlerKind,
	target model.Selection,
	binding mo.Option[Binding],
) selectionResult {
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
	return s.commitSelection(ctx, final, binding, issues)
}

// commitSelection commits and enqueues under optional binding protection, then waits after guard release.
func (s *Service) commitSelection(
	ctx context.Context,
	final model.Selection,
	binding mo.Option[Binding],
	issues []Issue,
) selectionResult {
	result := selectionResult{
		selection: model.Selection{}, committed: false, issues: issues, deliveryErr: nil, err: nil,
	}
	var wait func(context.Context) error
	var change mo.Option[SelectionChange]
	commit := func() error {
		preceding, committed, commitErr := s.catalog.CommitSelection(ctx, final)
		if commitErr != nil {
			return commitErr
		}
		result.selection = committed
		result.committed = true
		if preceding == committed {
			return nil
		}
		change = mo.Some(SelectionChange{Preceding: preceding, Committed: committed})
		var publicationErr error
		wait, publicationErr = s.publisher.PublishSelection(committed)
		result.deliveryErr = publicationErr
		return nil
	}
	protected, commitErr := s.commitWithBindingProtection(ctx, binding, commit)
	if commitErr != nil {
		switch {
		case result.committed:
			result.deliveryErr = errors.Join(result.deliveryErr, commitErr)
		case protected:
			result.err = classifyBindingProtection(commitErr)
		default:
			result.err = commitErr
		}
	}
	if !result.committed {
		return result
	}
	if result.deliveryErr == nil && wait != nil {
		result.deliveryErr = wait(ctx)
	}
	if committedChange, changed := change.Get(); changed {
		result.issues = s.observeSelection(context.WithoutCancel(ctx), committedChange, result.issues)
	}
	return result
}

// commitWithBindingProtection runs commit directly for clients or under extension binding protection.
func (s *Service) commitWithBindingProtection(
	ctx context.Context,
	binding mo.Option[Binding],
	commit func() error,
) (bool, error) {
	bound, hasBinding := binding.Get()
	if !hasBinding {
		return false, commit()
	}
	if s.protection == nil {
		return true, &SelectionError{
			Code: ErrorCodeInternal, cause: errors.New("extension selection binding protection is not bound"),
		}
	}
	return true, s.protection.ProtectSelectionCommit(ctx, bound, commit)
}

// Run executes one admitted private selection operation.
func (p *preparedSelection) Run(ctx context.Context) selectionResult {
	return p.owner.run(ctx, p.kind, p.target, p.binding)
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
