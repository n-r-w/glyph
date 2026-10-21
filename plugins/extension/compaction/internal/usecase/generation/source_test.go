//go:build !integration

package generation

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestSourceMetadataStaysControlOnly keeps original-record correspondence out of generation and token estimates.
func TestSourceMetadataStaysControlOnly(t *testing.T) {
	t.Parallel()
	// Arrange identical prepared conversation content with different source correspondence.
	request := newGenerationRequest()
	original, err := serializeSource(request)
	require.NoError(t, err)
	tokens := compaction.EstimateInput(request.Prefix, "", "")
	request.Prefix[0].ID = "different-original-record"
	request.Prefix[0].SourcePosition = 99
	request.Prefix[0].SourceKind = compaction.SourceExtensionMessage
	request.Prefix[0].Tokens = 999
	// Act through source serialization and the existing content estimator.
	changed, err := serializeSource(request)
	estimated := compaction.EstimateInput(request.Prefix, "", "")
	// Assert only prepared content contributes to the model source and its estimated weight.
	require.NoError(t, err)
	require.Equal(t, original, changed)
	require.Equal(t, tokens, estimated)
}
