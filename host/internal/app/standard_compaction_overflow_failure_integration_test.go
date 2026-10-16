//go:build integration

package app

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestStandardOverflowFailure preserves complete causes and durable outcomes when admitted recovery cannot continue.
func TestStandardOverflowFailure(t *testing.T) {
	const standardGenerationFailureBody = `{"error":{"message":"standard summary generation source",` +
		`"type":"invalid_request_error"}}`

	for _, scenario := range []struct {
		name  string
		cause string
	}{
		{name: "immediate-repeat", cause: "no older reducible portion"},
		{name: "allowance", cause: "provider context limit source"},
		{name: "no-reducible", cause: "no older reducible portion"},
		{name: "generation-failure", cause: "standard summary generation source"},
		{name: "observer-failure", cause: "reference compaction observer failure: original observer cause"},
		{name: "final-rejection", cause: "conversation input budget exceeded"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			// Arrange a fitting conversation and the selected external failure in the standard recovery path.
			t.Setenv(standardRetainedEnvironment, "1")
			directory, outcomePath := buildStandardOverflowFixture(t)
			if scenario.name == "observer-failure" {
				t.Setenv("GLYPH_REFERENCE_OBSERVER_FAILURE", "1")
			}
			if scenario.name == "final-rejection" {
				t.Setenv(automaticFixtureAppendEnvironment, manualFixtureEnabled)
				t.Setenv(automaticFixtureModeEnvironment, automaticFixtureRejectFinalMode)
			}
			retries := "3\n  delays: [1ms, 1ms, 1ms]"
			if scenario.name == "allowance" {
				retries = "1\n  delays: [1ms]"
			}
			overflowAt := 2
			if scenario.name == "no-reducible" {
				overflowAt = 1
			}
			bodies := installStandardOverflowTransport(t, func(position int) (*http.Response, error) {
				if position == overflowAt {
					return newStandardOverflowResponse("provider context limit source"), nil
				}
				if (scenario.name == "immediate-repeat" || scenario.name == "allowance") && position >= overflowAt+2 {
					return newStandardOverflowResponse("provider repeated overflow source"), nil
				}
				response := runtimeFailureHTTPResponse(finalResponseSSE)
				if scenario.name == "generation-failure" && position == overflowAt+1 {
					response = runtimeFailureHTTPResponse(standardGenerationFailureBody)
					response.StatusCode = http.StatusBadRequest
				}
				return response, nil
			})
			fixture := startStandardOverflowFixture(t, directory, retries)
			defer fixture.closeOwner(t)
			if scenario.name != "no-reducible" {
				completeProgrammaticRequest(t, fixture, userRequest("overflow-original", manualOriginalText))
			}

			// Act through ordinary submission and the provider's context-overflow classification.
			require.NoError(t, fixture.stream.Send(userRequest("overflow-recent", manualRetainedText)))
			terminal := receiveAutomaticTerminal(t, fixture, "overflow-recent").GetEvent()

			// Assert full terminal causes and no generation, retry, or marker beyond the admitted path.
			require.NotNil(t, terminal.GetFailed())
			require.Contains(t, terminal.GetFailed().GetMessage(), "provider context limit source")
			require.Contains(t, terminal.GetFailed().GetMessage(), scenario.cause)
			repeated := scenario.name == "immediate-repeat" || scenario.name == "allowance"
			if repeated {
				require.Contains(t, terminal.GetFailed().GetMessage(), "provider repeated overflow source")
			}
			if scenario.name == "final-rejection" {
				require.Contains(
					t,
					terminal.GetFailed().GetMessage(),
					"budget 7000 (context 8000, response reserve 1000)",
				)
			}
			committed := scenario.name != "no-reducible" && scenario.name != "generation-failure"
			marker := assertStandardOverflowHistory(t, fixture, scenario.name != "no-reducible", committed, 0)
			actual := bodies.snapshot()
			expectedCalls := overflowAt + 1
			if repeated {
				expectedCalls++
			}
			if scenario.name == "no-reducible" {
				expectedCalls = 1
			}
			require.Len(t, actual, expectedCalls)
			if committed {
				require.Contains(t, string(actual[overflowAt]), manualOriginalText)
				if repeated {
					assertStandardOverflowProjection(t, actual[len(actual)-1], marker)
				}
			}
			outcome := readStandardOverflowOutcome(t, outcomePath)
			if scenario.name == "immediate-repeat" || scenario.name == "no-reducible" {
				require.Nil(t, outcome.GetCommitted())
				require.Contains(t, outcome.GetError(), "no older reducible portion")
			} else {
				assertStandardOverflowIntent(t, outcome.GetState().GetOriginalRequest())
				if committed {
					require.Equal(t, marker.GetId(), outcome.GetCommitted().GetId())
				}
			}
		})
	}
}
