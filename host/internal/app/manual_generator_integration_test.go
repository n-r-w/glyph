//go:build integration

package app

// manualGeneratorsEnvironment selects generator registrations in the reference extension.
const manualGeneratorsEnvironment = "GLYPH_REFERENCE_GENERATORS"

// manualGeneratorModeEnvironment selects the reference generator outcome.
const manualGeneratorModeEnvironment = "GLYPH_REFERENCE_GENERATOR_MODE"

// manualSeparateGeneratorEnvironment selects a generator in a distinct runtime.
const manualSeparateGeneratorEnvironment = "GLYPH_REFERENCE_SEPARATE_GENERATOR"

const (
	// manualGeneratorImagesMode selects ordered configured-model image input.
	manualGeneratorImagesMode = "images"
	// manualGeneratorBlockedModelMode selects nested provider lifetime checks.
	manualGeneratorBlockedModelMode = "blocked-model"
	// manualGeneratorCancelMode selects an explicit canceled outcome.
	manualGeneratorCancelMode = "cancel"
	// manualGeneratorMixedCancelMode selects a generator outcome with cancellation and an independent failure.
	manualGeneratorMixedCancelMode = "mixed-cancel"
	// manualGeneratorClassifiedCancelMode selects a classified cancellation failure.
	manualGeneratorClassifiedCancelMode = "classified-cancel"
	// manualGeneratorForgedProducerMode selects a generator result with a forged producer identity.
	manualGeneratorForgedProducerMode = "forged-producer"
	// manualGeneratorMultipleRegistrations selects two fixture generators.
	manualGeneratorMultipleRegistrations = "2"
	// manualGeneratorInternalCategory identifies classified extension failures.
	manualGeneratorInternalCategory = "INTERNAL"
)

// manualGeneratorSummary identifies the reference generator's replacement.
const manualGeneratorSummary = "Reference generator replacement summary."

// TestSeparateGenerator checks policy-selected dispatch to a distinct captured runtime.
func (s *manualRequestHandlersSuite) TestSeparateGenerator() {
	// Arrange one generator outside the policy runtime.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualSeparateGeneratorEnvironment, manualFixtureEnabled)
	// Act through public manual compaction in runScenario.
	// Assert distinct captured runtime identity, replacement and preserved history.
	s.runScenario(manualRequestClearGuidance, manualGeneratorSummary, "", false)
}

// TestGeneratorCancellation checks explicit generator cancellation with an active caller.
func (s *manualRequestHandlersSuite) TestGeneratorCancellation() {
	// Arrange one canceling generator in the policy runtime.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorCancelMode)
	// Act through public manual compaction in runScenario.
	// Assert explicit cancellation and no persisted replacement in runScenario.
	s.runScenario(manualRequestClearGuidance, "", "", true)
}

// TestGeneratorMixedCancellation checks preservation of an independent generator failure.
func (s *manualRequestHandlersSuite) TestGeneratorMixedCancellation() {
	// Arrange cancellation joined with an independent failure.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorMixedCancelMode)
	// Act through public manual compaction in runScenario.
	// Assert the independent cause and no replacement in runScenario.
	s.runScenario(manualRequestClearGuidance, "", "reference generator independent failure", false)
}

// TestGeneratorClassifiedCancellation checks failure precedence over a cancellation sentinel.
func (s *manualRequestHandlersSuite) TestGeneratorClassifiedCancellation() {
	// Arrange a generator that classifies cancellation as failure.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorClassifiedCancelMode)
	// Act through public manual compaction in runScenario.
	// Assert the classified failure and no replacement in runScenario.
	s.runScenario(manualRequestClearGuidance, "", manualGeneratorInternalCategory, false)
}

// TestGeneratorProducerRejection checks the selected generator's producer identity.
func (s *manualRequestHandlersSuite) TestGeneratorProducerRejection() {
	// Arrange a generator that claims another extension as producer.
	s.T().Setenv(manualGeneratorsEnvironment, manualFixtureEnabled)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorForgedProducerMode)
	// Act through public manual compaction in runScenario.
	// Assert rejection before replacement in runScenario.
	s.runScenario(manualRequestClearGuidance, "", "generator producer", false)
}

// TestMultipleGenerators checks the policy's explicit cardinality failure.
func (s *manualRequestHandlersSuite) TestMultipleGenerators() {
	// Arrange two captured generators with no current result.
	s.T().Setenv(manualGeneratorsEnvironment, manualGeneratorMultipleRegistrations)
	// Act through public manual compaction in runScenario.
	// Assert the policy's explicit failure and no replacement in runScenario.
	s.runScenario(
		manualRequestClearGuidance,
		"",
		"reference policy requires exactly one captured generator; received 2",
		false,
	)
}

// TestCompleteResultBypassesMultipleGenerators characterizes complete-result bypass.
func (s *manualRequestHandlersSuite) TestCompleteResultBypassesMultipleGenerators() {
	// Arrange multiple generator registrations and a complete request-handler result.
	s.T().Setenv(manualGeneratorsEnvironment, manualGeneratorMultipleRegistrations)
	s.T().Setenv(manualGeneratorModeEnvironment, manualGeneratorMixedCancelMode)
	// Act through request composition in runScenario.
	// Assert the supplied result succeeds without invoking the failing generator.
	s.runScenario(manualRequestCompositionGuidance, manualRequestFinalSummary, "", false)
}
