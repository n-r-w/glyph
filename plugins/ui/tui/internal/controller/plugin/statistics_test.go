package plugin

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	uiv1 "github.com/n-r-w/glyph/pkg/plugins/ui/v1"
	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestMapRequestReconstructsAvailableSessionStatistics verifies the helper-process boundary preserves token presence.
func TestMapRequestReconstructsAvailableSessionStatistics(t *testing.T) {
	t.Parallel()

	// Arrange a session-information protobuf with nondefault lifecycle values and available tokens.
	info := uiv1.SessionInfo_builder{
		Id: new("active"), Name: new("name"), WorkingDirectory: new("/project"), StoragePath: new("/session.jsonl"),
		CreatedTime: timestamppb.New(time.Unix(1, 0)), UpdateTime: timestamppb.New(time.Unix(2, 0)),
	}.Build()
	tokens := uiv1.TokenUsage_builder{
		InputTokens: new(int64(1)), OutputTokens: new(int64(2)), CacheReadTokens: new(int64(3)),
		CacheWriteTokens: new(int64(4)), ReasoningTokens: new(int64(2)), TotalTokens: new(int64(10)),
	}.Build()
	statistics := uiv1.SessionStatistics_builder{
		UserMessages: new(int64(1)), ModelResponses: new(int64(2)), ToolCalls: new(int64(3)),
		ToolResults: new(int64(4)), TotalMessages: new(int64(7)), Tokens: tokens,
	}.Build()
	//nolint:exhaustruct // The protobuf builder intentionally sets only the active oneof field.
	request := uiv1.OpenRequest_builder{
		SessionInformation: uiv1.SessionInformation_builder{Info: info, Statistics: statistics}.Build(),
	}.Build()

	// Act by reconstructing the TUI presentation event.
	event, err := mapRequest(request)

	// Assert lifecycle data, counts, and normalized token buckets match the wire values.
	require.NoError(t, err)
	assert.Equal(t, presentationdomain.EventSessionInformation, event.Kind)
	assert.Equal(t, "active", event.SessionInfo.OrEmpty().ID)
	mappedStatistics, present := event.SessionStatistics.Get()
	assert.True(t, present)
	assert.Equal(t, 7, mappedStatistics.TotalMessages)
	assert.Equal(t, int64(10), mappedStatistics.TokenUsage.OrEmpty().TotalTokens)
}
