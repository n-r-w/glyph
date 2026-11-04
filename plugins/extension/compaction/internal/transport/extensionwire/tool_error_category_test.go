//go:build !integration

package extensionwire

import (
	"testing"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/n-r-w/glyph/pkg/plugins/extension/v1"
	"github.com/n-r-w/glyph/plugins/extension/compaction/internal/domain/compaction"
)

// TestToolResultErrorCategoryRoundTrip verifies that the Host error category of a tool result passes through the
// policy representation and back to the participant request, and that a result without a category stays without one.
func TestToolResultErrorCategoryRoundTrip(t *testing.T) {
	t.Parallel()
	for name, category := range map[string]*pb.ToolErrorCategory{
		"none":              nil,
		"unavailable tool":  new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNAVAILABLE_TOOL),
		"invalid arguments": new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_INVALID_ARGUMENTS),
		"tool failure":      new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_TOOL_FAILURE),
		"policy rejection":  new(pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_POLICY_REJECTION),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a prepared error result with the scenario category.
			wire := new(pb.CompactionHistoryRecord)
			wire.SetSourceEntryId("source")
			wire.SetToolResult(pb.SessionTreeToolResult_builder{
				Terminate: nil,
				CallId:    new("call"), ToolName: new("read"), Contents: nil, IsError: new(true),
				ErrorCategory: category,
			}.Build())

			// Act through policy decoding and participant-request encoding.
			decoded := decodeHistoryOrFail(t, []*pb.CompactionHistoryRecord{wire})
			encoded := encodeHistoryOrFail(t, decoded)

			// Assert the category on both sides of the policy.
			require.Len(t, encoded, 1)
			result := encoded[0].GetToolResult()
			assert.Equal(t, category != nil, result.HasErrorCategory())
			if category != nil {
				assert.Equal(t, *category, result.GetErrorCategory())
			}
			assert.True(t, result.GetIsError())
		})
	}
}

// newInvalidCategoryRecord creates an error tool result of the named history record with a category outside the closed
// set.
func newInvalidCategoryRecord(id string, category pb.ToolErrorCategory) *pb.CompactionHistoryRecord {
	record := new(pb.CompactionHistoryRecord)
	record.SetSourceEntryId(id)
	record.SetToolResult(pb.SessionTreeToolResult_builder{
		Terminate: nil,
		CallId:    new("call"), ToolName: new("read"), Contents: nil, IsError: new(true),
		ErrorCategory: new(category),
	}.Build())
	return record
}

// TestToolResultUnrecognizedErrorCategory verifies that an unspecified or unknown category fails decoding with an
// error that names the category value and the record that holds it.
func TestToolResultUnrecognizedErrorCategory(t *testing.T) {
	t.Parallel()
	for name, category := range map[string]pb.ToolErrorCategory{
		"unspecified": pb.ToolErrorCategory_TOOL_ERROR_CATEGORY_UNSPECIFIED,
		"unknown":     pb.ToolErrorCategory(99),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			// Arrange a prepared error result with a category outside the closed set.
			wire := newInvalidCategoryRecord("source", category)

			// Act through policy decoding.
			decoded, err := DecodeHistory([]*pb.CompactionHistoryRecord{wire})

			// Assert the decoding failure names the category and the record.
			require.ErrorContains(t, err, category.String())
			require.ErrorContains(t, err, `"source"`)
			assert.Nil(t, decoded)
		})
	}
}

// TestRequestUnrecognizedErrorCategory verifies that a category outside the closed set in the prefix or the suffix of
// a request fails request decoding with an error that names the part, the category value, and the record.
func TestRequestUnrecognizedErrorCategory(t *testing.T) {
	t.Parallel()
	for part, setPart := range map[string]func(*pb.CompactionRequest, []*pb.CompactionHistoryRecord){
		"prefix": (*pb.CompactionRequest).SetPrefix,
		"suffix": (*pb.CompactionRequest).SetSuffix,
	} {
		t.Run(part, func(t *testing.T) {
			t.Parallel()

			// Arrange a request whose selected part holds a result with an unknown category.
			wire := newMappingRequest()
			setPart(
				wire,
				[]*pb.CompactionHistoryRecord{newInvalidCategoryRecord("bad-record", pb.ToolErrorCategory(99))},
			)

			// Act through request decoding.
			decoded, err := DecodeRequest(wire)

			// Assert the failure names the part, the category, and the record.
			require.ErrorContains(t, err, part)
			require.ErrorContains(t, err, "99")
			require.ErrorContains(t, err, `"bad-record"`)
			assert.Zero(t, decoded)
		})
	}
}

// TestEncodeUnrecognizedErrorCategory verifies that a category outside the closed set fails request encoding with an
// error that names the part, the category value, and the record.
func TestEncodeUnrecognizedErrorCategory(t *testing.T) {
	t.Parallel()
	// Arrange a request whose prefix holds a result with a category that the public contract does not define.
	request := decodeRequestOrFail(t, newMappingRequest())
	request.Prefix = []compaction.Entry{{
		ID: "bad-record", Kind: compaction.ToolResult, Blocks: nil, CallID: "call", Tool: "read", Failed: true,
		ErrorCategory:   mo.Some(compaction.ToolErrorCategory("not_a_category")),
		SourcePosition:  0,
		SourceKind:      compaction.SourceUser,
		PreviousSummary: false,
		PrivateBytes:    0,
		Tokens:          0,
	}}

	// Act through request encoding.
	encoded, err := EncodeRequest(request)

	// Assert the failure names the part, the category, and the record.
	require.ErrorContains(t, err, "prefix")
	require.ErrorContains(t, err, "not_a_category")
	require.ErrorContains(t, err, `"bad-record"`)
	assert.Nil(t, encoded)
}

// TestRequestActionUnrecognizedErrorCategory verifies that a category outside the closed set in the replacement
// request of an action fails action decoding with an error that names the category value and the record.
func TestRequestActionUnrecognizedErrorCategory(t *testing.T) {
	t.Parallel()
	// Arrange an action whose replacement request holds a result with an unknown category.
	request := newMappingRequest()
	request.SetPrefix([]*pb.CompactionHistoryRecord{newInvalidCategoryRecord("bad-record", pb.ToolErrorCategory(99))})
	wire := new(pb.CompactionRequestAction)
	wire.SetRequestAction(pb.CompactionRequestDisposition_COMPACTION_REQUEST_DISPOSITION_REPLACE)
	wire.SetRequest(request)

	// Act through action decoding.
	decoded, err := DecodeRequestAction(wire)

	// Assert the failure names the category and the record.
	require.ErrorContains(t, err, "99")
	require.ErrorContains(t, err, `"bad-record"`)
	assert.Zero(t, decoded)
}
