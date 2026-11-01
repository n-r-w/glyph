//go:build !integration

package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
)

// newInvalidCategoryHistory creates history with an error tool result whose category is outside the closed set.
func newInvalidCategoryHistory() []*pb.CompactionHistoryRecord {
	record := new(pb.CompactionHistoryRecord)
	record.SetSourceEntryId("bad-record")
	record.SetToolResult(pb.SessionTreeToolResult_builder{
		CallId: new("call"), ToolName: new("read"), Contents: nil, IsError: new(true),
		ErrorCategory: new(pb.ToolErrorCategory(99)),
	}.Build())
	return []*pb.CompactionHistoryRecord{record}
}

// TestHandlerFailsOnUnrecognizedErrorCategory verifies that a callback whose history holds a tool error category
// outside the closed set fails with the decoding cause and reaches neither the policy nor the generator.
func TestHandlerFailsOnUnrecognizedErrorCategory(t *testing.T) {
	t.Parallel()
	history := newInvalidCategoryHistory()
	tests := map[string]func(*pb.HandleRequest){
		"completed conversation": func(request *pb.HandleRequest) {
			request.SetCompletedConversation(pb.CompletedConversationObservation_builder{History: history}.Build())
		},
		"outgoing input": func(request *pb.HandleRequest) {
			request.SetCompactionOutgoingInput(pb.CompactionOutgoingInput_builder{History: history}.Build())
		},
		"generate": func(request *pb.HandleRequest) {
			request.SetCompactionGenerate(pb.CompactionRequestInvocation_builder{
				Original: nil, CurrentResult: nil,
				Current: pb.CompactionRequest_builder{Prefix: history}.Build(),
			}.Build())
		},
		"policy": func(request *pb.HandleRequest) {
			request.SetCompactionPolicy(pb.CompactionPolicyInvocation_builder{History: history}.Build())
		},
	}
	for name, setCallback := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a callback with the invalid category and a policy that accepts no call.
			request := new(pb.HandleRequest)
			setCallback(request)
			operation := &handler{service: New(NewMockPolicy(gomock.NewController(t)), nil, false), request: request}

			// Act through the production callback dispatcher.
			response, err := operation.Run(t.Context())

			// Assert the failure names the category and the record.
			require.ErrorContains(t, err, "99")
			require.ErrorContains(t, err, `"bad-record"`)
			assert.Nil(t, response)
		})
	}
}
