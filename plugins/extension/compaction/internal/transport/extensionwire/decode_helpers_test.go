//go:build !integration

package extensionwire

import (
	"testing"

	"github.com/stretchr/testify/require"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// decodeHistoryOrFail decodes history that must hold only values of the public contract.
func decodeHistoryOrFail(t *testing.T, values []*pb.CompactionHistoryRecord) []compaction.Entry {
	t.Helper()
	history, err := DecodeHistory(values)
	require.NoError(t, err)
	return history
}

// decodeInputOrFail decodes policy input that must hold only values of the public contract.
func decodeInputOrFail(t *testing.T, value *pb.CompactionPolicyInvocation) compaction.Input {
	t.Helper()
	input, err := DecodeInput(value)
	require.NoError(t, err)
	return input
}

// decodeRequestOrFail decodes a request that must hold only values of the public contract.
func decodeRequestOrFail(t *testing.T, value *pb.CompactionRequest) compaction.Request {
	t.Helper()
	request, err := DecodeRequest(value)
	require.NoError(t, err)
	return request
}

// decodeRequestActionOrFail decodes a request action that must hold only values of the public contract.
func decodeRequestActionOrFail(t *testing.T, value *pb.CompactionRequestAction) compaction.RequestAction {
	t.Helper()
	action, err := DecodeRequestAction(value)
	require.NoError(t, err)
	return action
}

// decodeObservationOrFail decodes an observation that must hold only values of the public contract.
func decodeObservationOrFail(
	t *testing.T,
	value *pb.CompletedConversationObservation,
	binding compaction.Binding,
) compaction.Observation {
	t.Helper()
	observation, err := DecodeObservation(value, binding)
	require.NoError(t, err)
	return observation
}

// encodeHistoryOrFail encodes history that must hold only tool error categories of the closed set.
func encodeHistoryOrFail(t *testing.T, entries []compaction.Entry) []*pb.CompactionHistoryRecord {
	t.Helper()
	history, err := encodeHistory(entries)
	require.NoError(t, err)
	return history
}

// encodeRequestOrFail encodes a request that must hold only tool error categories of the closed set.
func encodeRequestOrFail(t *testing.T, value compaction.Request) *pb.CompactionRequest {
	t.Helper()
	request, err := EncodeRequest(value)
	require.NoError(t, err)
	return request
}

// encodeInvocationOrFail encodes a state that must hold only tool error categories of the closed set.
func encodeInvocationOrFail(t *testing.T, value compaction.State) *pb.CompactionRequestInvocation {
	t.Helper()
	invocation, err := EncodeInvocation(value)
	require.NoError(t, err)
	return invocation
}
