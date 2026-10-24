//go:build integration

package app

import (
	"encoding/json/v2"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestPublicStaleInvocationCannotReuseLiveSession verifies session access does not authorize detached invocation work.
//
//nolint:paralleltest // This test replaces the process-wide HTTP transport for the assembled application.
func TestPublicStaleInvocationCannotReuseLiveSession(t *testing.T) {
	// Arrange one unchanged session and a real plugin that retains its first invocation context.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	var count atomic.Int32
	var body atomic.Value
	var mode atomic.Value
	mode.Store("catalogs")
	previous := http.DefaultTransport
	http.DefaultTransport = catalogueProviderTransport(t, &count, &body, func() string { return mode.Load().(string) })
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
	defer fixture.closeOwner(t)
	completeProgrammaticRequest(t, fixture, userRequest("initial-context", "inspect catalog"))
	initial := catalogueReportIdentity(t, externalToolOutput(t, body.Load().([]byte)))

	// Act through a second successful ordinary tool invocation using the preceding invocation's context.
	mode.Store("stale-catalogs")
	completeProgrammaticRequest(t, fixture, userRequest("stale-invocation", "inspect retained context"))
	var report struct {
		PreviousContextID string `json:"previous_context_id"`
		CurrentContextID  string `json:"current_context_id"`
		ErrorCode         string `json:"error_code"`
		ErrorText         string `json:"error_text"`
	}
	require.NoError(t, json.Unmarshal([]byte(externalToolOutput(t, body.Load().([]byte))), &report))
	// Assert the shared session binding remains unchanged but Host rejects the stale parent association.
	require.Equal(t, initial.GetContextId(), report.PreviousContextID)
	require.Equal(t, report.PreviousContextID, report.CurrentContextID)
	require.Equal(t, "STALE_CONTEXT", report.ErrorCode)
	require.Contains(t, report.ErrorText, "parent invocation is unavailable")
	require.Equal(t, int32(4), count.Load())
}
