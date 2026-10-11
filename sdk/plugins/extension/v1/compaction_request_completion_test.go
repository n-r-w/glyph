//go:build !integration

package extensionv1

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// requestCompletionSuite checks missing request-handler actions at the Host completion boundary.
type requestCompletionSuite struct {
	// Suite separates absent-action rejection from present-action delivery.
	suite.Suite
}

// TestCompactionRequestCompletion checks action presence before typed delivery.
func TestCompactionRequestCompletion(t *testing.T) {
	t.Parallel()
	// Arrange independent completion payloads.
	// Act and Assert through the boundary suite.
	suite.Run(t, &requestCompletionSuite{Suite: suite.Suite{}})
}

// TestMissingAction checks rejection of absent and wrong-kind completions before successful Wait.
func (s *requestCompletionSuite) TestMissingAction() {
	t := s.T()
	// Arrange missing action and a completion for another request kind.
	missing := new(extensionpb.HostCompleted)
	wrong := new(extensionpb.HostCompleted)
	wrong.SetGetModels(new(extensionpb.GetModelsResult))
	for _, result := range []*extensionpb.HostCompleted{nil, missing, wrong} {
		// Act through completion validation.
		err := hostRequestCompactionRequest.validateHostCompleted(result)
		// Assert that typed delivery cannot succeed with a nil action.
		require.EqualError(t, err, "received Host completion does not match request kind")
	}
}

// TestPresentAction checks that composition validation remains separate from payload presence.
func (s *requestCompletionSuite) TestPresentAction() {
	t := s.T()
	// Arrange a present action whose dispositions are unspecified.
	result := new(extensionpb.HostCompleted)
	result.SetInvokeCompactionRequest(new(extensionpb.CompactionRequestAction))
	// Act through completion validation.
	err := hostRequestCompactionRequest.validateHostCompleted(result)
	// Assert successful typed delivery for later composition-action validation.
	require.NoError(t, err)
}
