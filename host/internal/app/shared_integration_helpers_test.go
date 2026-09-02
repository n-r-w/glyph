//go:build integration

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeUIExecutable creates one executable wrapper around the current test binary.
func writeUIExecutable(t *testing.T, directory, name string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=serve exec %q -test.run=^TestUIPluginHelperProcess$\n",
		appUIHelperEnvironment,
		os.Args[0],
	)
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(script), 0o755))
}

// writeConfiguredUIExecutable creates an executable UI candidate with isolated fixture settings.
func writeConfiguredUIExecutable(t *testing.T, directory, name, tracePath, mode string) {
	t.Helper()
	script := fmt.Sprintf(
		"#!/bin/sh\n%s=serve %s=%q %s=1 %s=%q exec %q -test.run=^TestUIPluginHelperProcess$\n",
		appUIHelperEnvironment, appUITraceEnvironment, tracePath, appUITerminalEnvironment,
		appUIBehaviorEnvironment, mode, os.Args[0],
	)
	require.NoError(t, os.WriteFile(filepath.Join(directory, name), []byte(script), 0o755))
}

// assertStatisticsObservation compares every count, token, cost, and grouping field.
func assertStatisticsObservation(t *testing.T, expected, actual statisticsObservation) {
	t.Helper()
	assert.Equal(t, expected.UserMessages, actual.UserMessages)
	assert.Equal(t, expected.ModelResponses, actual.ModelResponses)
	assert.Equal(t, expected.ToolCalls, actual.ToolCalls)
	assert.Equal(t, expected.ToolResults, actual.ToolResults)
	assert.Equal(t, expected.TotalMessages, actual.TotalMessages)
	assert.Equal(t, expected.Tokens, actual.Tokens)
	assertCostObservation(t, expected.EstimatedCost, actual.EstimatedCost)
	assert.Equal(t, expected.CostGroupCount, actual.CostGroupCount)
	assert.Equal(t, expected.GroupProvider, actual.GroupProvider)
	assert.Equal(t, expected.GroupModel, actual.GroupModel)
	assertCostObservation(t, expected.GroupCost, actual.GroupCost)
}

// assertCostObservation compares one optional cost with floating tolerance.
func assertCostObservation(t *testing.T, expected, actual costObservation) {
	t.Helper()
	assert.Equal(t, expected.Present, actual.Present)
	assert.InDelta(t, expected.Input, actual.Input, 1e-12)
	assert.InDelta(t, expected.Output, actual.Output, 1e-12)
	assert.InDelta(t, expected.CacheRead, actual.CacheRead, 1e-12)
	assert.InDelta(t, expected.CacheWrite, actual.CacheWrite, 1e-12)
	assert.InDelta(t, expected.Total, actual.Total, 1e-12)
}
