package runtime

import (
	"testing"
	"time"

	"github.com/samber/mo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/n-r-w/glyph/host/internal/domain/session"
	domainui "github.com/n-r-w/glyph/host/internal/domain/ui"
)

// TestMapFramePreservesSessionInformationAndStatistics verifies Host UI protobuf mapping keeps both payloads.
func TestMapFramePreservesSessionInformationAndStatistics(t *testing.T) {
	t.Parallel()

	// Arrange lifecycle fields and present-zero token usage.
	info := session.Info{
		ID: "active", Name: mo.Some("name"), WorkingDirectory: "/project", StoragePath: mo.Some("/session.jsonl"),
		CreatedAt: time.Unix(1, 0), UpdatedAt: time.Unix(2, 0),
	}
	statistics := session.Statistics{
		UserMessages: 1, ModelResponses: 2, ToolCalls: 3, ToolResults: 4, TotalMessages: 7,
		TokenUsage: mo.Some(session.TokenUsage{}),
	}
	frame := domainui.Frame{
		Kind: domainui.FrameSessionInformation, Initialization: mo.None[domainui.Initialization](),
		Lifecycle: mo.None[domainui.Lifecycle](), AuthorizationURL: mo.None[string](), Text: mo.None[string](),
		RetryAuthentication: mo.None[bool](), ModelSelection: mo.None[domainui.ModelSelection](),
		SessionInfo: mo.Some(info), Sessions: nil, SessionEntries: nil,
		SessionStatistics: mo.Some(statistics),
	}

	// Act by mapping the Host frame to the UI protobuf request.
	request, err := mapFrame(frame)

	// Assert all lifecycle fields and present-zero token availability survive.
	require.NoError(t, err)
	mapped := request.GetSessionInformation()
	assert.Equal(t, "active", mapped.GetInfo().GetId())
	assert.Equal(t, "name", mapped.GetInfo().GetName())
	assert.Equal(t, "/project", mapped.GetInfo().GetWorkingDirectory())
	assert.Equal(t, "/session.jsonl", mapped.GetInfo().GetStoragePath())
	assert.Equal(t, int64(7), mapped.GetStatistics().GetTotalMessages())
	assert.True(t, mapped.GetStatistics().HasTokens())
	assert.Zero(t, mapped.GetStatistics().GetTokens().GetTotalTokens())
}
