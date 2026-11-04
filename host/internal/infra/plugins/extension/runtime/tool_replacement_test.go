//go:build !integration

package runtime

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestToolDescriptorReplacementDeclarationCrossesTheWire verifies that the replacement declaration of a protobuf
// tool descriptor reaches the runtime declaration, and that an omitted declaration is not a replacement.
func TestToolDescriptorReplacementDeclarationCrossesTheWire(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		// replaces is the wire value; nil omits the field.
		replaces *bool
		// want is the expected declaration.
		want bool
	}{
		"declared": {replaces: new(true), want: true},
		"false":    {replaces: new(false), want: false},
		"omitted":  {replaces: nil, want: false},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			// Arrange a wire descriptor with the replacement value.
			descriptor := extensionpb.ToolDescriptor_builder{
				Name: new("read"), Description: new("Read."), InputSchemaJson: []byte(`{"type":"object"}`),
				ConstrainedSampling: nil, ReplacesTool: test.replaces, Concurrent: nil,
			}.Build()

			// Act by decoding the descriptor.
			declaration, present := mapToolDescriptor(descriptor).Get()

			// Assert the declaration carries the replacement value.
			require.True(t, present)
			assert.Equal(t, test.want, declaration.ReplacesTool)
		})
	}
}
