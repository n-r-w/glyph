//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

// uIManualCancellationStopsObserverModelSuite isolates UI cancellation of observer-owned model work.
type uIManualCancellationStopsObserverModelSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestUIManualCancellationStopsObserverModel checks UI owner cancellation joins observer model work and retains the
// durable receipt.
func TestUIManualCancellationStopsObserverModel(t *testing.T) {
	// Arrange observer-owned model work for the UI caller.
	t.Setenv(manualFixtureGlyphReferenceObserverModel, manualFixtureEnabled)
	// Act through the UI cancellation scenario.
	// Assert joined observer work and the durable receipt in TestScenario.
	suite.Run(t, &uIManualCancellationStopsObserverModelSuite{Suite: suite.Suite{}})
}

// extensionManualCancellationStopsObserverModelSuite isolates Extension cancellation of observer-owned model work.
type extensionManualCancellationStopsObserverModelSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestExtensionManualCancellationStopsObserverModel checks Extension owner cancellation joins observer model work
// and retains the durable receipt.
func TestExtensionManualCancellationStopsObserverModel(t *testing.T) {
	// Arrange observer-owned model work for the Extension caller.
	t.Setenv(manualFixtureGlyphReferenceObserverModel, manualFixtureEnabled)
	// Act through the Extension cancellation scenario.
	// Assert joined observer work and the durable receipt in TestScenario.
	suite.Run(t, &extensionManualCancellationStopsObserverModelSuite{Suite: suite.Suite{}})
}

// programmaticManualCancellationStopsObserverModelSuite isolates Programmatic cancellation
// of observer-owned model work.
type programmaticManualCancellationStopsObserverModelSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCancellationStopsObserverModel checks Programmatic owner cancellation joins observer model
// work and retains the durable receipt.
func TestProgrammaticManualCancellationStopsObserverModel(t *testing.T) {
	// Arrange observer-owned model work for the Programmatic caller.
	t.Setenv(manualFixtureGlyphReferenceObserverModel, manualFixtureEnabled)
	// Act through the Programmatic cancellation scenario.
	// Assert joined observer work and the durable receipt in TestScenario.
	suite.Run(t, &programmaticManualCancellationStopsObserverModelSuite{Suite: suite.Suite{}})
}

// programmaticManualCompactionUsesRegisteredPolicySuite isolates Programmatic replacement
// and subsequent provider input.
type programmaticManualCompactionUsesRegisteredPolicySuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCompactionUsesRegisteredPolicy checks registered-policy replacement and subsequent
// Programmatic provider input.
func TestProgrammaticManualCompactionUsesRegisteredPolicy(t *testing.T) {
	const manualFixturePolicyInputJson = "policy-input.json"

	// Arrange capture of the public policy invocation input.
	inputPath := filepath.Join(t.TempDir(), manualFixturePolicyInputJson)
	t.Setenv(manualFixtureGlyphReferencePolicyInput, inputPath)
	// Act through Programmatic replacement and the next conversation request.
	// Assert captured policy input and replacement projection in TestScenario.
	suite.Run(t, &programmaticManualCompactionUsesRegisteredPolicySuite{Suite: suite.Suite{}})
}

// programmaticManualCompactionRejectsAbsentPolicySuite isolates Programmatic admission with no registered policy.
type programmaticManualCompactionRejectsAbsentPolicySuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCompactionRejectsAbsentPolicy checks the public Programmatic failure when the policy owner
// is absent.
func TestProgrammaticManualCompactionRejectsAbsentPolicy(t *testing.T) {
	// Arrange parallel execution of the owner-absence suite.
	t.Parallel()
	// Act through Programmatic admission with no registered policy.
	// Assert the distinct owner-absence failure in TestScenario.
	suite.Run(t, &programmaticManualCompactionRejectsAbsentPolicySuite{Suite: suite.Suite{}})
}

// uIManualCompactionUsesRegisteredPolicySuite isolates UI replacement and subsequent provider input.
type uIManualCompactionUsesRegisteredPolicySuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestUIManualCompactionUsesRegisteredPolicy checks registered-policy replacement and subsequent UI provider input.
func TestUIManualCompactionUsesRegisteredPolicy(t *testing.T) {
	// Arrange the public UI compaction fixture.
	t.Setenv(appUIBehaviorEnvironment, manualUIBehavior)
	// Act through UI replacement and the next conversation request.
	// Assert the durable receipt and replacement projection in TestScenario.
	suite.Run(t, &uIManualCompactionUsesRegisteredPolicySuite{Suite: suite.Suite{}})
}

// programmaticManualCompactionTerminalStatesSuite isolates Programmatic cancellation and failure outcomes.
type programmaticManualCompactionTerminalStatesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCompactionTerminalStates checks Programmatic cancellation, stale capture, and durable
// failure receipts.
func TestProgrammaticManualCompactionTerminalStates(t *testing.T) {
	// Arrange policy-input capture for Programmatic terminal scenarios.
	t.Setenv(manualFixtureGlyphReferencePolicyInput, filepath.Join(t.TempDir(), manualFixtureInputJson))
	// Act through Programmatic cancellation and failure scenarios.
	// Assert preserved originals and receipt-dependent outcomes in TestScenario.
	suite.Run(t, &programmaticManualCompactionTerminalStatesSuite{Suite: suite.Suite{}})
}

// uIManualCompactionTerminalStatesSuite isolates UI cancellation and failure outcomes.
type uIManualCompactionTerminalStatesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestUIManualCompactionTerminalStates checks public UI failure and cancellation results.
func TestUIManualCompactionTerminalStates(t *testing.T) {
	// Arrange the public UI fixture for terminal outcomes.
	t.Setenv(appUIBehaviorEnvironment, manualUIBehavior)
	// Act through UI cancellation and failure scenarios.
	// Assert preserved originals and receipt-dependent outcomes in TestScenario.
	suite.Run(t, &uIManualCompactionTerminalStatesSuite{Suite: suite.Suite{}})
}

// programmaticManualCompactionRejectsAmbiguousOwnersSuite isolates registration with competing policy owners.
type programmaticManualCompactionRejectsAmbiguousOwnersSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCompactionRejectsAmbiguousOwners checks registration rejects multiple policy owners.
func TestProgrammaticManualCompactionRejectsAmbiguousOwners(t *testing.T) {
	// Arrange parallel execution of the owner-ambiguity suite.
	t.Parallel()
	// Act through registration with competing policy owners.
	// Assert rejection of owner ambiguity in TestScenario.
	suite.Run(t, &programmaticManualCompactionRejectsAmbiguousOwnersSuite{Suite: suite.Suite{}})
}

// extensionManualCompactionTerminalStatesSuite isolates Extension cancellation and failure outcomes.
type extensionManualCompactionTerminalStatesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestExtensionManualCompactionTerminalStates checks session-bound Extension failure, cancellation, and durable
// receipts.
func TestExtensionManualCompactionTerminalStates(t *testing.T) {
	const manualFixtureDisabled = "0"

	// Arrange policy registration for the Extension terminal scenarios.
	t.Setenv(manualFixtureGlyphReferencePolicyDisabled, manualFixtureDisabled)
	// Act through session-bound Extension cancellation and failure scenarios.
	// Assert preserved originals and receipt-dependent outcomes in TestScenario.
	suite.Run(t, &extensionManualCompactionTerminalStatesSuite{Suite: suite.Suite{}})
}

// programmaticManualCancellationStopsNestedModelSuite isolates cancellation of policy-owned model work.
type programmaticManualCancellationStopsNestedModelSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCancellationStopsNestedModel checks owning cancellation joins nested policy model work.
func TestProgrammaticManualCancellationStopsNestedModel(t *testing.T) {
	// Arrange policy-input capture for nested cancellation scenarios.
	t.Setenv(manualFixtureGlyphReferencePolicyInput, filepath.Join(t.TempDir(), manualFixtureInputJson))
	// Act through Programmatic cancellation during nested model execution.
	// Assert joined policy and participant model work in TestScenario.
	suite.Run(t, &programmaticManualCancellationStopsNestedModelSuite{Suite: suite.Suite{}})
}
