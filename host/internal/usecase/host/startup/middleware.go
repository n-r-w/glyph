package startup

import "errors"

// BindMiddleware connects the ordinary middleware owner before loading extensions.
func (s *Service) BindMiddleware(owner MiddlewareRegistrar) error {
	if owner == nil {
		return errors.New("middleware registrar is required")
	}
	if s.middleware != nil {
		return errors.New("middleware registrar is already bound")
	}
	s.middleware = owner
	return nil
}

// isMiddleware reports whether a kind belongs to the ordinary model-facing middleware owner.
func (kind RawHandlerKind) isMiddleware() bool {
	return kind >= RawHandlerKindProviderHeaders && kind <= RawHandlerKindFinalizedMessage
}

// validateMiddlewareHandlers validates the middleware partition without publishing partial registration.
func (s *Service) validateMiddlewareHandlers(registration PendingRegistration) ([]AcceptedHandler, error) {
	partition := partitionHandlers(registration, RawHandlerKind.isMiddleware)
	if len(partition.Handlers) == 0 {
		return nil, nil
	}
	if s.middleware == nil {
		return nil, errors.New("middleware registrar is not bound")
	}
	return s.middleware.ValidateMiddlewareHandlers(partition)
}
