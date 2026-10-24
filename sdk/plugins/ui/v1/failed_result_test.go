//go:build !integration

package uiv1

import (
	"testing"

	"github.com/stretchr/testify/require"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
)

// TestFailedHostPayloadValidation verifies optional retained data is typed and failure fields remain mandatory.
func TestFailedHostPayloadValidation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"absent-result", "selection", "missing-failure", "missing-code", "missing-text", "empty-result", "wrong-kind", "invalid-selection"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			// Arrange one public failed selection envelope with a scenario-specific malformed field.
			failure := operationpb.Failed_builder{
				Code:    new("EXTENSION_FAILED"),
				Message: new("complete observer cause"),
			}.Build()
			var result *uiv1.HostCompleted
			if scenario == "selection" || scenario == "invalid-selection" {
				result = new(uiv1.HostCompleted)
				selection := uiv1.ModelSelection_builder{
					ProviderId: new("provider"), ModelId: new("model"),
					ReasoningChoice: new(uiv1.ReasoningChoice_REASONING_CHOICE_HIGH),
				}.Build()
				if scenario == "invalid-selection" {
					selection.ClearModelId()
				}
				result.SetModelSelection(uiv1.ModelSelectionChanged_builder{Selection: selection, Issues: nil}.Build())
			}
			switch scenario {
			case "missing-failure":
				failure = nil
			case "missing-code":
				failure.ClearCode()
			case "missing-text":
				failure.ClearMessage()
			case "empty-result":
				result = new(uiv1.HostCompleted)
			case "wrong-kind":
				result = new(uiv1.HostCompleted)
				result.SetSubmit(new(uiv1.SubmitCompleted))
			}
			event := new(uiv1.HostEvent)
			event.SetFailed(uiv1.HostFailed_builder{Failure: failure, Result: result}.Build())

			// Act at the public transport's request-kind and payload validation boundary.
			err := validateHostPayload(requestSelectModel, event)

			// Assert absent result is valid, while missing failure data and invalid retained data are diagnosed.
			if scenario == "absent-result" || scenario == "selection" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				if scenario != "missing-failure" && scenario != "missing-text" {
					require.Contains(t, err.Error(), "complete observer cause")
				}
			}
		})
	}
}
