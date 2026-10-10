//go:build integration

// Package app composes and runs the Host application.
package app

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
)

// uIManualCancellationStopsObserverModelSuite owns an independent scenario context.
type uIManualCancellationStopsObserverModelSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestUIManualCancellationStopsObserverModel checks UI owner cancellation joins observer model work and retains the
// durable receipt.
func TestUIManualCancellationStopsObserverModel(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Setenv(manualFixtureGlyphReferenceObserverModel, manualFixtureEnabled)
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &uIManualCancellationStopsObserverModelSuite{Suite: suite.Suite{}})
}

// extensionManualCancellationStopsObserverModelSuite owns an independent scenario context.
type extensionManualCancellationStopsObserverModelSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestExtensionManualCancellationStopsObserverModel checks Extension owner cancellation joins observer model work
// and retains the durable receipt.
func TestExtensionManualCancellationStopsObserverModel(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Setenv(manualFixtureGlyphReferenceObserverModel, manualFixtureEnabled)
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &extensionManualCancellationStopsObserverModelSuite{Suite: suite.Suite{}})
}

// programmaticManualCancellationStopsObserverModelSuite owns an independent scenario context.
type programmaticManualCancellationStopsObserverModelSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCancellationStopsObserverModel checks Programmatic owner cancellation joins observer model
// work and retains the durable receipt.
func TestProgrammaticManualCancellationStopsObserverModel(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Setenv(manualFixtureGlyphReferenceObserverModel, manualFixtureEnabled)
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &programmaticManualCancellationStopsObserverModelSuite{Suite: suite.Suite{}})
}

// programmaticManualCompactionUsesRegisteredPolicySuite owns an independent scenario context.
type programmaticManualCompactionUsesRegisteredPolicySuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCompactionUsesRegisteredPolicy checks registered-policy replacement and subsequent
// Programmatic provider input.
func TestProgrammaticManualCompactionUsesRegisteredPolicy(t *testing.T) {
	// manualFixturePolicyInputJson names the policy-input evidence file.
	const manualFixturePolicyInputJson = "policy-input.json"

	// Arrange independent scenario configuration.
	inputPath := filepath.Join(t.TempDir(), manualFixturePolicyInputJson)
	t.Setenv(manualFixtureGlyphReferencePolicyInput, inputPath)
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &programmaticManualCompactionUsesRegisteredPolicySuite{Suite: suite.Suite{}})
}

// programmaticManualCompactionRejectsAbsentPolicySuite owns an independent scenario context.
type programmaticManualCompactionRejectsAbsentPolicySuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCompactionRejectsAbsentPolicy checks the public Programmatic failure when the policy owner
// is absent.
func TestProgrammaticManualCompactionRejectsAbsentPolicy(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &programmaticManualCompactionRejectsAbsentPolicySuite{Suite: suite.Suite{}})
}

// uIManualCompactionUsesRegisteredPolicySuite owns an independent scenario context.
type uIManualCompactionUsesRegisteredPolicySuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestUIManualCompactionUsesRegisteredPolicy checks registered-policy replacement and subsequent UI provider input.
func TestUIManualCompactionUsesRegisteredPolicy(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Setenv(appUIBehaviorEnvironment, manualUIBehavior)
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &uIManualCompactionUsesRegisteredPolicySuite{Suite: suite.Suite{}})
}

// programmaticManualCompactionTerminalStatesSuite owns an independent scenario context.
type programmaticManualCompactionTerminalStatesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCompactionTerminalStates checks Programmatic cancellation, stale capture, and durable
// failure receipts.
func TestProgrammaticManualCompactionTerminalStates(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Setenv(manualFixtureGlyphReferencePolicyInput, filepath.Join(t.TempDir(), manualFixtureInputJson))
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &programmaticManualCompactionTerminalStatesSuite{Suite: suite.Suite{}})
}

// uIManualCompactionTerminalStatesSuite owns an independent scenario context.
type uIManualCompactionTerminalStatesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestUIManualCompactionTerminalStates checks public UI failure and cancellation results.
func TestUIManualCompactionTerminalStates(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Setenv(appUIBehaviorEnvironment, manualUIBehavior)
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &uIManualCompactionTerminalStatesSuite{Suite: suite.Suite{}})
}

// programmaticManualCompactionRejectsAmbiguousOwnersSuite owns an independent scenario context.
type programmaticManualCompactionRejectsAmbiguousOwnersSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCompactionRejectsAmbiguousOwners checks registration rejects multiple policy owners.
func TestProgrammaticManualCompactionRejectsAmbiguousOwners(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Parallel()
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &programmaticManualCompactionRejectsAmbiguousOwnersSuite{Suite: suite.Suite{}})
}

// extensionManualCompactionTerminalStatesSuite owns an independent scenario context.
type extensionManualCompactionTerminalStatesSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestExtensionManualCompactionTerminalStates checks session-bound Extension failure, cancellation, and durable
// receipts.
func TestExtensionManualCompactionTerminalStates(t *testing.T) {
	// manualFixtureDisabled disables a reference fixture mode.
	const manualFixtureDisabled = "0"

	// Arrange independent scenario configuration.
	t.Setenv(manualFixtureGlyphReferencePolicyDisabled, manualFixtureDisabled)
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &extensionManualCompactionTerminalStatesSuite{Suite: suite.Suite{}})
}

// programmaticManualCancellationStopsNestedModelSuite owns an independent scenario context.
type programmaticManualCancellationStopsNestedModelSuite struct {
	// Suite owns scenario assertions and lifecycle.
	suite.Suite
}

// TestProgrammaticManualCancellationStopsNestedModel checks owning cancellation joins nested policy model work.
func TestProgrammaticManualCancellationStopsNestedModel(t *testing.T) {
	// Arrange independent scenario configuration.
	t.Setenv(manualFixtureGlyphReferencePolicyInput, filepath.Join(t.TempDir(), manualFixtureInputJson))
	// Act: run the suite scenario.
	// Assert: the scenario method checks the public result.
	suite.Run(t, &programmaticManualCancellationStopsNestedModelSuite{Suite: suite.Suite{}})
}
