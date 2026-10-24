//go:build !integration

package extensionv1

import (
	"testing"

	"github.com/stretchr/testify/require"

	operationpb "github.com/n-r-w/glyph/pkg/operation/v1"
	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestFailedHostPayloadValidation verifies optional typed results included with Failed and complete failure data at the Extension boundary.
func TestFailedHostPayloadValidation(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{
		"absent-result", "selection", "missing-failure", "missing-code", "missing-text", "empty-result",
		"wrong-kind", "invalid-selection", "wrong-code",
	} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			// Arrange one failed selection envelope and its optional typed result.
			failure := operationpb.Failed_builder{
				Code:    new("EXTENSION_FAILED"),
				Message: new("complete observer cause"),
			}.Build()
			var result *extensionpb.HostCompleted
			if scenario == "selection" || scenario == "invalid-selection" {
				result = new(extensionpb.HostCompleted)
				selection := extensionpb.ModelSelection_builder{
					ProviderId: new("provider"), ModelId: new("model"), ReasoningChoice: new("high"),
				}.Build()
				if scenario == "invalid-selection" {
					selection.ClearModelId()
				}
				result.SetSelection(extensionpb.SelectionResult_builder{Selection: selection, Issues: nil}.Build())
			}
			switch scenario {
			case "missing-failure":
				failure = nil
			case "missing-code":
				failure.ClearCode()
			case "missing-text":
				failure.ClearMessage()
			case "wrong-code":
				failure.SetCode("MODEL_FAILED")
			case "empty-result":
				result = new(extensionpb.HostCompleted)
			case "wrong-kind":
				result = new(extensionpb.HostCompleted)
				result.SetGetModels(new(extensionpb.GetModelsResult))
			}
			event := new(extensionpb.HostEvent)
			event.SetFailed(extensionpb.HostFailed_builder{Failure: failure, Result: result}.Build())

			// Act at request-specific transport validation and event mapping.
			mapped, terminal, err := mapHostEvent("selection", hostRequestModelSelection, event)

			// Assert a failure can retain a matching result, and malformed data preserves the peer text in its diagnosis.
			if scenario == "absent-result" || scenario == "selection" {
				require.NoError(t, err)
				require.True(t, terminal)
				require.Same(t, result, mapped.Result)
				require.Equal(t, "EXTENSION_FAILED", mapped.Code)
				require.Equal(t, "complete observer cause", mapped.Message)
			} else {
				require.Error(t, err)
				if scenario != "missing-failure" && scenario != "missing-text" {
					require.Contains(t, err.Error(), "complete observer cause")
				}
			}
		})
	}
}
