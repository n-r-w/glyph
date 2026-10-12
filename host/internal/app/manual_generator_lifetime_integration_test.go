//go:build integration

package app

// TestGeneratorCallerCancellation checks cancellation and joining of a generator's nested provider work.
func (s *manualRequestHandlersSuite) TestGeneratorCallerCancellation() {
	// Arrange blocked configured-model work owned by one captured generator.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorBlockedModelMode)
	// Act through public cancellation in the nested-work helper.
	// Assert provider completion before release and unchanged original session history.
	runProgrammaticManualNestedCancellation(s.T(), manualRequestClearGuidance, "", "")
}

// TestGeneratorParticipantTimeout checks configured expiry while the caller remains active.
func (s *manualRequestHandlersSuite) TestGeneratorParticipantTimeout() {
	// Arrange blocked configured-model work under the YAML participant duration.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorBlockedModelMode)
	// Act through public compaction with a two-second participant duration.
	// Assert deadline failure, joined provider cleanup and every handled child cause.
	runProgrammaticManualNestedCancellation(s.T(), manualRequestClearGuidance, "2s", "")
}

// TestGeneratorProviderFailure checks the configured model's complete classified failure causes.
func (s *manualRequestHandlersSuite) TestGeneratorProviderFailure() {
	// Arrange a nested model that reports overflow together with provider cleanup failure.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorBlockedModelMode)
	// Act through public compaction with independently classified nested provider failure.
	// Assert the nested category, provider body, cleanup and handled selection causes.
	runProgrammaticManualNestedCancellation(s.T(), manualRequestClearGuidance, "2s", manualProviderCodedRejection)
}
