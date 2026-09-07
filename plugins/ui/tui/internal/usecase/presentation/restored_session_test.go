//go:build !integration

package presentation

import (
	"testing"
	"time"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"
)

// TestStateReplacesRestoredTranscriptOnlyAfterConfirmedSessionChange verifies replacement waits for session identity.
func TestStateReplacesRestoredTranscriptOnlyAfterConfirmedSessionChange(t *testing.T) {
	t.Parallel()

	// Arrange an existing transcript, restored entries, and pending and confirmed session events.
	oldLine := NewTextLine(LineUser, mo.Some("old"))
	restored := []Line{
		NewTextLine(LineUser, mo.Some("prior-user")),
		NewTextLine(LineModel, mo.Some("prior-model")),
	}
	state := projection{
		Startup: nil, Transcript: []Line{oldLine}, Models: nil,
		ActiveModel: nil, ActiveToolCalls: nil, ActiveTools: nil,
		Availability: mo.None[Availability](), Settled: mo.None[bool](),
		AuthorizationURL: mo.None[string](), ModelSelection: mo.None[ModelSelection](),
		SessionInfo: mo.None[SessionInfo](), Sessions: nil,
	}
	// Act by applying a pending replacement before session identity is confirmed.
	pending := testSessionEvent(
		eventSessionChanged, mo.None[SessionInfo](), restored,
	)
	state = state.Apply(pending)

	// Assert the pending event retains the existing transcript.

	require.Equal(t, []Line{oldLine}, state.Transcript)

	timestamp := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	info := SessionInfo{
		ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: timestamp, UpdatedAt: timestamp,
	}
	// Act by applying the confirmed replacement twice.
	confirmed := testSessionEvent(eventSessionChanged, mo.Some(info), restored)
	state = state.Apply(confirmed)

	// Assert confirmation replaces the transcript and repeated delivery is idempotent.
	require.Equal(t, restored, state.Transcript)
	state = state.Apply(confirmed)
	require.Equal(t, restored, state.Transcript)

	information := testSessionEvent(
		eventSessionInformation, mo.Some(info), []Line{oldLine},
	)
	state = state.Apply(information)
	require.Equal(t, restored, state.Transcript)
}

// TestStateOwnsRestoredUserImageBytes verifies restored user images transfer ownership to presentation state.
func TestStateOwnsRestoredUserImageBytes(t *testing.T) {
	t.Parallel()

	// Arrange a restored user line backed by caller-owned image bytes.
	imageBytes := []byte{1, 2, 3}
	restored := []Line{{
		Kind: LineUser, ToolName: mo.None[string](), Status: mo.None[string](),
		Text: mo.Some("[image image/png, 3 bytes]"),
		Contents: mo.Some([]Content{{
			Text: mo.None[string](), MediaType: mo.Some("image/png"), Data: mo.Some(imageBytes),
		}}),
	}}
	state := projection{
		Startup: nil, Transcript: nil, Models: nil, ActiveModel: nil, ActiveToolCalls: nil, ActiveTools: nil,
		Availability: mo.None[Availability](), AuthorizationURL: mo.None[string](),
		Settled: mo.None[bool](), ModelSelection: mo.None[ModelSelection](),
		SessionInfo: mo.None[SessionInfo](), Sessions: nil,
	}
	timestamp := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	info := SessionInfo{
		ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: timestamp, UpdatedAt: timestamp,
	}

	// Act by applying SessionChanged and mutating every caller-owned byte reference.
	state = state.Apply(testSessionEvent(
		eventSessionChanged, mo.Some(info), restored,
	))
	imageBytes[0] = 9
	restored[0].Contents.MustGet()[0].Data.MustGet()[1] = 9

	// Assert presentation state retains an independent copy of the original image.
	require.Equal(t, []byte{1, 2, 3}, state.Transcript[0].Contents.MustGet()[0].Data.MustGet())
}
