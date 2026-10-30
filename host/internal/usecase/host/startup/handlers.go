package startup

// validateRegistrationHandlers validates every capability before publishing any declaration.
func (s *Service) validateRegistrationHandlers(raw PendingRegistration) ([]AcceptedHandler, error) {
	if err := validateHandlerIdentities(raw.Handlers); err != nil {
		return nil, err
	}
	sessionHandlers, err := s.sessionTree.ValidateSessionTreeHandlers(partitionHandlers(raw, isSessionTreeKind))
	if err != nil {
		return nil, err
	}
	lifecycleHandlers, err := s.lifecycle.ValidateLifecycleHandlers(partitionHandlers(raw, isLifecycleKind))
	if err != nil {
		return nil, err
	}
	selectionHandlers, err := s.validateSelectionHandlers(raw)
	if err != nil {
		return nil, err
	}
	retryHandlers := partitionHandlers(raw, RawHandlerKind.isRetry).acceptHandlers()
	compactionHandlers, err := s.validateCompactionHandlers(raw)
	if err != nil {
		return nil, err
	}
	wireHandlers, err := s.validateMiddlewareHandlers(raw)
	if err != nil {
		return nil, err
	}
	return mergeHandlers(
		raw.Handlers,
		sessionHandlers,
		lifecycleHandlers,
		selectionHandlers,
		retryHandlers,
		compactionHandlers,
		wireHandlers,
	), nil
}
