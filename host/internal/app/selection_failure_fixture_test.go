//go:build integration

package app

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	programmaticv1 "github.com/n-r-w/glyph/pkg/programmatic/v1"
)

// readPublicSelection obtains Host-confirmed active state through Programmatic Control.
func readPublicSelection(t *testing.T, fixture *programmaticFixture, id string) *programmaticv1.ModelSelection {
	t.Helper()
	request := new(programmaticv1.OpenRequest)
	request.SetOperationId(id)
	payload := new(programmaticv1.ControllerRequest)
	payload.SetGetModels(new(programmaticv1.GetModels))
	request.SetRequest(payload)
	require.NoError(t, fixture.stream.Send(request))
	for {
		response, err := fixture.stream.Recv()
		require.NoError(t, err)
		event := response.GetEvent()
		if response.GetOperationId() != id {
			require.False(t, event.HasCompleted() || event.HasFailed() || event.HasCanceled(),
				"a settled selection emitted another terminal before the state-query barrier")
			continue
		}
		require.False(t, event.HasFailed() || event.HasCanceled() || event.HasRejected())
		if event.HasCompleted() {
			return event.GetCompleted().GetModels().GetActiveSelection()
		}
	}
}

// selectionFailureSettings makes a model selection change both the model and reasoning choice.
func selectionFailureSettings() string {
	settings := selectionCompositionSettings()
	models := strings.SplitN(settings, "    models:\n", 2)
	return strings.Replace(settings, "defaultModel: gpt-test", "defaultModel: gpt-start", 1) +
		strings.Replace(models[1], "id: gpt-test", "id: gpt-start", 1)
}
