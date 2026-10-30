//go:build !integration

package extensionv1

import (
	"testing"

	"github.com/stretchr/testify/require"

	extensionpb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// TestWireHandlerAdmission accepts only the payload declared by each public wire handler.
func TestWireHandlerAdmission(t *testing.T) {
	t.Parallel()
	// Arrange the three ordinary public wire payloads.
	//nolint:exhaustruct_v5 // Each protobuf oneof builder sets only its active wire payload.
	requests := []*extensionpb.HandleRequest{
		extensionpb.HandleRequest_builder{
			ProviderHeaders: extensionpb.ProviderHeadersInvocation_builder{}.Build(),
		}.Build(),
		extensionpb.HandleRequest_builder{ProviderBody: extensionpb.ProviderBodyInvocation_builder{}.Build()}.Build(),
		extensionpb.HandleRequest_builder{
			ProviderResponse: extensionpb.ProviderResponseInvocation_builder{}.Build(),
		}.Build(),
	}
	kinds := []extensionpb.HandlerKind{
		extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_HEADERS,
		extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_BODY,
		extensionpb.HandlerKind_HANDLER_KIND_PROVIDER_RESPONSE,
	}
	// Act and assert matching payload admission and mismatched payload rejection.
	for i, kind := range kinds {
		for j, request := range requests {
			require.Equal(t, i == j, handlerKindMatches(kind, request), "kind %v payload %d", kind, j)
		}
	}
}
