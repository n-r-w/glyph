//go:build integration

package app

import (
	"encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// consumedSelectionKindEnvironment selects the real SDK nested selection caller.
	consumedSelectionKindEnvironment = "GLYPH_CONSUMED_SELECTION_KIND"
	// consumedSelectionReportName identifies the SDK caller's retained outcome.
	consumedSelectionReportName = "consumed-selection.json"
)

// TestPublicConsumedSelectionFailureStopsParent verifies Host retains a failure consumed by the real SDK caller.
func TestPublicConsumedSelectionFailureStopsParent(t *testing.T) {
	for _, kind := range []string{"model", "reasoning"} {
		for _, mode := range []string{"error", "invalid", "independent-cancel", "observer", "observer-independent-cancel"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				// Arrange a real ordinary tool that consumes its nested selection outcome.
				t.Setenv(consumedSelectionKindEnvironment, kind)
				if strings.HasPrefix(mode, "observer") {
					t.Setenv(externalSelectionObserversEnvironment, "1")
					observerMode := strings.TrimPrefix(mode, "observer-")
					if observerMode == "observer" {
						observerMode = "error"
					}
					t.Setenv(externalSelectionObserverErrorEnvironment, observerMode)
					if kind == "model" {
						t.Setenv(externalSelectionCompositionEnvironment, "1")
					}
				} else {
					t.Setenv(externalSelectionCompositionEnvironment, "1")
					t.Setenv(externalSelectionFailureEnvironment, mode)
				}
				signals := t.TempDir()
				t.Setenv(externalSignalsEnvironment, signals)
				settings := selectionCompositionSettings()
				if kind == "model" {
					settings = selectionFailureSettings()
				}
				paths := testPaths(t, settings)
				writeProgrammaticCredentials(t, paths)
				transport, bodies := extensionSelectionProviderTransport(t)
				previous := http.DefaultTransport
				http.DefaultTransport = transport
				t.Cleanup(func() { http.DefaultTransport = previous })
				fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
				defer fixture.closeOwner(t)

				// Act through a public run while the tool catches the nested selection failure and returns success.
				failure, retries := requestProgrammaticFailure(
					t,
					fixture,
					userRequest("consumed-selection", "select model"),
				)

				// Assert the actual SDK result survives with failure and Host stops the parent before another model call.
				require.Equal(t, "EXTENSION_FAILED", failure.Code)
				require.Zero(t, retries)
				require.Len(t, bodies.snapshot(), 1)
				encoded, err := os.ReadFile(filepath.Join(signals, consumedSelectionReportName))
				require.NoError(t, err)
				var report struct {
					Failure   string `json:"failure"`
					Code      string `json:"code"`
					Reasoning string `json:"reasoning"`
					Later     string `json:"later"`
				}
				require.NoError(t, json.Unmarshal(encoded, &report))
				require.Equal(t, "EXTENSION_FAILED", report.Code)
				require.NotEmpty(t, report.Failure)
				cause := publicSelectionHandlerFailureText
				if mode == "observer" {
					cause = "public reasoning observer failed"
				} else if mode == "invalid" {
					cause = "invalid action"
				} else if strings.Contains(mode, "independent-cancel") {
					cause = "context canceled"
				}
				require.Contains(t, failure.Message, cause)
				require.Contains(t, report.Failure, cause)
				require.Contains(t, report.Later, "parent invocation")
				if strings.HasPrefix(mode, "observer") {
					require.Equal(t, "high", report.Reasoning)
				} else {
					require.Empty(t, report.Reasoning)
				}
				// Assert an independent selection still invokes the same available extension.
				selectionRequest := selectModelRequest("independent-selection-operation", "openai-codex", "gpt-test")
				if strings.HasPrefix(mode, "observer") {
					if kind == "model" {
						selectionRequest = selectReasoningRequest(
							"independent-selection-operation",
							programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
						)
					}
					completeProgrammaticRequest(t, fixture, selectionRequest)
				} else {
					independentFailure, _ := requestProgrammaticFailure(t, fixture, selectionRequest)
					require.Equal(t, "EXTENSION_FAILED", independentFailure.Code)
				}
				// Assert the same extension connection remains usable by an independent successful run.
				completeProgrammaticRequest(t, fixture, userRequest("independent-selection", "continue independently"))
				require.Len(t, bodies.snapshot(), 2)
			})
		}
	}
}
