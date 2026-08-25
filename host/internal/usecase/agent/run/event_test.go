package run

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewEventHasNoPayloads(t *testing.T) {
	t.Parallel()

	event := newEvent(EventAgentStart, "run-1")

	assert.Equal(t, EventAgentStart, event.Type)
	assert.Equal(t, "run-1", event.RunID)
	assert.True(t, event.Position.IsNone())
	assert.True(t, event.Content.IsNone())
	assert.True(t, event.Message.IsNone())
	assert.True(t, event.Preview.IsNone())
	assert.True(t, event.ToolCall.IsNone())
	assert.True(t, event.Progress.IsNone())
	assert.True(t, event.ToolResult.IsNone())
	assert.True(t, event.Turn.IsNone())
	assert.True(t, event.Agent.IsNone())
}
