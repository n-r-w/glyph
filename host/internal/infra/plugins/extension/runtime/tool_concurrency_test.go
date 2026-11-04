//go:build !integration

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestToolConcurrencyDeclaration verifies explicit and omitted concurrency reach runtime declarations.
func TestToolConcurrencyDeclaration(t *testing.T) {
	t.Parallel()
	for _, concurrent := range []*bool{nil, new(false), new(true)} {
		// Arrange a descriptor with an optional concurrency declaration.
		descriptor := extensionpb.ToolDescriptor_builder{
			Name: new("read"), Description: new("Read."), InputSchemaJson: []byte(`{"type":"object"}`),
			ConstrainedSampling: nil, ReplacesTool: nil, Concurrent: concurrent,
		}.Build()
		// Act at the runtime decode boundary.
		declaration, present := mapToolDescriptor(descriptor).Get()
		// Assert omission remains serial and explicit concurrency survives.
		require.True(t, present)
		assert.Equal(t, concurrent != nil && *concurrent, declaration.Concurrent)
	}
}
