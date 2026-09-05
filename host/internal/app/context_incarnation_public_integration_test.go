//go:build integration

package app

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	programmaticpb "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// TestPublicRetainedContextNeverReactivates verifies one external process rejects its saved A context after every
// replacement.
//
//nolint:paralleltest // This test replaces process-global provider HTTP transport.
func TestPublicRetainedContextNeverReactivates(t *testing.T) {
	// Arrange: keep the same public-only extension process alive across session replacement commands.
	paths := testPaths(t, codexSettings(""))
	writeProgrammaticCredentials(t, paths)
	directory := buildPublicCatalogueExtension(t)
	var count atomic.Int32
	var body atomic.Value
	var mode atomic.Value
	mode.Store("catalogs")
	previous := http.DefaultTransport
	http.DefaultTransport = catalogueProviderTransport(t, &count, &body, func() string { return mode.Load().(string) })
	t.Cleanup(func() { http.DefaultTransport = previous })
	fixture := startProgrammaticFixtureWithExtension(t, paths, directory)
	defer fixture.closeOwner(t)
	sequence := 0
	read := func(requested string) string {
		sequence++
		mode.Store(requested)
		completeProgrammaticRequest(t, fixture, userRequest(fmt.Sprintf("catalog-%d", sequence), "inspect catalog"))
		return catalogueToolOutput(t, body.Load().([]byte))
	}
	initial := catalogueReportIdentity(t, read("catalogs"))

	// Act: switch A to B, resume A, and resume the same durable A again.
	for index := range 3 {
		sendProgrammaticOperation(
			t,
			fixture,
			fmt.Sprintf("replace-%d", index),
			func(request *programmaticpb.OpenRequest) {
				if index == 0 {
					programmaticRequest(request).SetCreateSession(new(programmaticpb.CreateSession))
					return
				}
				programmaticRequest(
					request,
				).SetResumeSession(programmaticpb.ResumeSession_builder{SessionId: new(initial.GetSessionId())}.Build())
			},
		)
		var stale struct {
			// PreviousContextID identifies the retained first A binding.
			PreviousContextID string `json:"previous_context_id"`
			// CurrentContextID identifies this invocation's fresh binding.
			CurrentContextID string `json:"current_context_id"`
			// ErrorCode records the public SDK rejection or failure category.
			ErrorCode string `json:"error_code"`
			// ErrorText preserves the complete public SDK error text.
			ErrorText string `json:"error_text"`
		}
		require.NoError(t, json.Unmarshal([]byte(read("stale-catalogs")), &stale))

		// Assert: the old binding remains stale while the newly issued context can read both catalogs.
		assert.Equal(t, initial.GetContextId(), stale.PreviousContextID)
		assert.NotEqual(t, initial.GetContextId(), stale.CurrentContextID)
		assert.Equal(t, "STALE_CONTEXT", stale.ErrorCode)
		assert.Contains(t, stale.ErrorText, `extension "external"`)
		assert.Contains(t, stale.ErrorText, "reference does not match")
		fresh := catalogueReportIdentity(t, read("catalogs"))
		assert.Equal(t, stale.CurrentContextID, fresh.GetContextId())
		if index > 0 {
			assert.Equal(t, initial.GetSessionId(), fresh.GetSessionId())
		}
	}
	assert.Equal(t, int32(14), count.Load())
}

// catalogueReportIdentity decodes the binding returned by the public-only extension.
func catalogueReportIdentity(t *testing.T, encoded string) *extensionpb.ExtensionContext {
	t.Helper()
	var report struct {
		// Identity contains the exact SDK identity in protobuf JSON form.
		Identity jsontext.Value `json:"identity"`
	}
	require.NoError(t, json.Unmarshal([]byte(encoded), &report))
	identity := new(extensionpb.ExtensionContext)
	require.NoError(t, protojson.Unmarshal(report.Identity, identity))
	return identity
}
