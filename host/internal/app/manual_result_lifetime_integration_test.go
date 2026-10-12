//go:build integration

package app

// TestCallerCancellation checks cancellation and joining of result-participant model work.
func (s *manualResultHandlersSuite) TestCallerCancellation() {
	// Arrange blocked ordinary model access in a result participant.
	s.T().Setenv("GLYPH_REFERENCE_RESULT_MODE", manualGeneratorBlockedModelMode)
	// Act through public caller cancellation.
	// Assert joined provider work and preserved durable entries in the nested-work helper.
	runProgrammaticManualNestedCancellation(s.T(), "", "", "")
}

// TestParticipantTimeout checks configured expiry after a preceding accepted result.
func (s *manualResultHandlersSuite) TestParticipantTimeout() {
	// Arrange blocked model work under the configured participant lifetime.
	s.T().Setenv("GLYPH_REFERENCE_RESULT_MODE", manualGeneratorBlockedModelMode)
	// Act through public compaction with a bounded result participant.
	// Assert deadline precedence and complete acquired cleanup causes in the nested-work helper.
	runProgrammaticManualNestedCancellation(s.T(), "", "2s", "")
}

// TestProviderFailure checks independent classified failure after result-participant model access.
func (s *manualResultHandlersSuite) TestProviderFailure() {
	// Arrange a provider failure acquired inside a result participant.
	s.T().Setenv("GLYPH_REFERENCE_RESULT_MODE", manualGeneratorBlockedModelMode)
	// Act through public compaction under a bounded participant lifetime.
	// Assert provider, selection and cleanup causes in the nested-work helper.
	runProgrammaticManualNestedCancellation(s.T(), "", "2s", manualProviderCodedRejection)
}
