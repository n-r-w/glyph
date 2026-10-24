//go:build integration

package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/proto"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

const (
	// externalSelectionFailureEnvironment controls the real selection handler failure.
	externalSelectionFailureEnvironment = "GLYPH_EXTERNAL_SELECTION_FAILURE"
	// publicSelectionHandlerFailureText identifies the complete handler cause.
	publicSelectionHandlerFailureText = "public selection handler full unique failure text"
	// publicSelectionLaterHandlerSignal marks an incorrectly continued chain.
	publicSelectionLaterHandlerSignal = "selection-later-handler"
)

// TestPublicSelectionHandlerFailureStopsOperation verifies both public selection chains stop before commit.
func TestPublicSelectionHandlerFailureStopsOperation(t *testing.T) {
	for _, kind := range []string{"model", "reasoning"} {
		for _, mode := range []string{"error", "invalid", "unavailable", "independent-cancel"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				// Arrange two real composing handlers and a failing first participant.
				t.Setenv(externalSelectionCompositionEnvironment, "1")
				t.Setenv(externalSelectionFailureEnvironment, mode)
				signals := t.TempDir()
				t.Setenv(externalSignalsEnvironment, signals)
				paths := testPaths(t, selectionCompositionSettings())
				writeProgrammaticCredentials(t, paths)
				fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
				defer fixture.closeOwner(t)
				request := selectModelRequest("handler-failure", "openai-codex", "gpt-test")
				if kind == "reasoning" {
					request = selectReasoningRequest(
						"handler-failure",
						programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF,
					)
				}

				before := readPublicSelection(t, fixture, "before-failure")

				// Act through Programmatic Control and the real extension handler.
				failure, progress := requestProgrammaticFailure(t, fixture, request)

				// Assert the complete failure stops the later handler and produces no retry.
				require.NotEmpty(t, failure.Code)
				if mode == "error" {
					require.Contains(t, failure.Message, publicSelectionHandlerFailureText)
				}
				require.Zero(t, progress)
				_, err := os.Stat(filepath.Join(signals, publicSelectionLaterHandlerSignal))
				require.ErrorIs(t, err, os.ErrNotExist)
				after := readPublicSelection(t, fixture, "after-failure")
				require.True(t, proto.Equal(before, after))
			})
		}
	}
}

// TestPublicSelectionObserverFailureStopsOperation verifies a committed selection fails without later observation.
func TestPublicSelectionObserverFailureStopsOperation(t *testing.T) {
	for _, scenario := range []struct {
		kind string
		mode string
	}{
		{kind: "model", mode: "error"},
		{kind: "reasoning", mode: "error"},
		{kind: "model", mode: "invalid"},
		{kind: "reasoning", mode: "invalid"},
		{kind: "model", mode: "unavailable"},
		{kind: "reasoning", mode: "unavailable"},
		{kind: "model", mode: "independent-cancel"},
		{kind: "reasoning", mode: "independent-cancel"},
	} {
		t.Run(scenario.kind+"/"+scenario.mode, func(t *testing.T) {
			kind := scenario.kind
			// Arrange a real extension whose first reasoning observer fails.
			t.Setenv(externalSelectionObserversEnvironment, "1")
			t.Setenv(externalSelectionObserverErrorEnvironment, scenario.mode)
			settings := selectionCompositionSettings()
			if kind == "model" {
				t.Setenv(externalSelectionCompositionEnvironment, "1")
				settings = selectionFailureSettings()
			}
			signals := t.TempDir()
			t.Setenv(externalSignalsEnvironment, signals)
			paths := testPaths(t, settings)
			writeProgrammaticCredentials(t, paths)
			fixture := startProgrammaticFixtureWithExtension(t, paths, buildPublicExtensionFixture(t))
			defer fixture.closeOwner(t)
			request := selectReasoningRequest("observer-failure", programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH)
			if kind == "model" {
				request = selectModelRequest("observer-failure", "openai-codex", "gpt-test")
			}

			// Act through the public selection entry point and real observer process.
			require.NoError(t, fixture.stream.Send(request))
			var terminal *programmaticv1.HostEvent
			for terminal == nil {
				response, err := fixture.stream.Recv()
				require.NoError(t, err)
				if response.GetOperationId() != request.GetOperationId() {
					continue
				}
				event := response.GetEvent()
				if event.HasFailed() || event.HasCompleted() || event.HasCanceled() {
					terminal = event
				}
			}

			// Assert failure retains the committed selection and stops both later observation groups.
			require.NotNil(t, terminal.GetFailed())
			failed := terminal.GetFailed()
			require.Equal(t, "EXTENSION_FAILED", failed.GetFailure().GetCode())
			require.NotEmpty(t, failed.GetFailure().GetMessage())
			if scenario.mode == "error" {
				require.Contains(t, failed.GetFailure().GetMessage(), "public reasoning observer failed")
			}
			selection := failed.GetResult().GetModelSelection().GetSelection()
			require.NotNil(t, selection)
			require.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH, selection.GetReasoningChoice())
			require.Equal(t, "gpt-test", selection.GetModelId())
			require.True(t, proto.Equal(selection, readPublicSelection(t, fixture, "committed-state")))
			if scenario.mode == "error" || scenario.mode == "independent-cancel" {
				choice := programmaticv1.ReasoningChoice_REASONING_CHOICE_HIGH
				if kind == "model" {
					choice = programmaticv1.ReasoningChoice_REASONING_CHOICE_OFF
				}
				unchanged := completeProgrammaticRequest(
					t,
					fixture,
					selectReasoningRequest("unchanged-selection", choice),
				)
				require.True(t, proto.Equal(selection, unchanged.GetModelSelection().GetSelection()))
			} else {
				// Assert registrations from the now-unavailable runtime are excluded from a new observer chain.
				completed := completeProgrammaticRequest(t, fixture,
					selectReasoningRequest("unavailable-excluded", programmaticv1.ReasoningChoice_REASONING_CHOICE_LOW))
				require.Equal(t, programmaticv1.ReasoningChoice_REASONING_CHOICE_LOW,
					completed.GetModelSelection().GetSelection().GetReasoningChoice())
			}
			for _, signal := range []string{laterReasoningSelectionObservedSignal, "model-selection-observed"} {
				_, err := os.Stat(filepath.Join(signals, signal))
				require.ErrorIs(t, err, os.ErrNotExist)
			}
		})
	}
}
