package presentation

import (
	"testing"
	"time"

	"github.com/samber/mo"

	"github.com/stretchr/testify/require"

	presentationdomain "github.com/n-r-w/glyph/plugins/ui/tui/internal/domain/presentation"
)

// TestServiceReplacesRestoredTranscriptOnlyAfterConfirmedSessionChange verifies replacement waits for session identity.
func TestServiceReplacesRestoredTranscriptOnlyAfterConfirmedSessionChange(t *testing.T) {
	t.Parallel()

	// Arrange an existing transcript, restored entries, and pending and confirmed session events.
	oldLine := presentationdomain.NewTextLine(presentationdomain.LineUser, mo.Some("old"))
	restored := []presentationdomain.Line{
		presentationdomain.NewTextLine(presentationdomain.LineUser, mo.Some("prior-user")),
		presentationdomain.NewTextLine(presentationdomain.LineModel, mo.Some("prior-model")),
	}
	service := New()
	state := presentationdomain.State{
		Startup: nil, Transcript: []presentationdomain.Line{oldLine}, Models: nil,
		ActiveModel: nil, ActiveToolCalls: nil, ActiveTools: nil,
		Availability: mo.None[presentationdomain.Availability](), Settled: mo.None[bool](),
		AuthorizationURL: mo.None[string](), ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
	}
	// Act by applying a pending replacement before session identity is confirmed.
	pending := testSessionEvent(presentationdomain.EventSessionChanged, mo.None[presentationdomain.SessionInfo](), restored)
	state = service.Apply(state, pending)

	// Assert the pending event retains the existing transcript.

	require.Equal(t, []presentationdomain.Line{oldLine}, state.Transcript)

	timestamp := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	info := presentationdomain.SessionInfo{
		ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: timestamp, UpdatedAt: timestamp,
	}
	// Act by applying the confirmed replacement twice.
	confirmed := testSessionEvent(presentationdomain.EventSessionChanged, mo.Some(info), restored)
	state = service.Apply(state, confirmed)

	// Assert confirmation replaces the transcript and repeated delivery is idempotent.
	require.Equal(t, restored, state.Transcript)
	state = service.Apply(state, confirmed)
	require.Equal(t, restored, state.Transcript)

	information := testSessionEvent(presentationdomain.EventSessionInformation, mo.Some(info), []presentationdomain.Line{oldLine})
	state = service.Apply(state, information)
	require.Equal(t, restored, state.Transcript)
}

// TestServiceOwnsRestoredUserImageBytes verifies restored user images transfer ownership to presentation state.
func TestServiceOwnsRestoredUserImageBytes(t *testing.T) {
	t.Parallel()

	// Arrange a restored user line backed by caller-owned image bytes.
	imageBytes := []byte{1, 2, 3}
	restored := []presentationdomain.Line{{
		Kind: presentationdomain.LineUser, ToolName: mo.None[string](), Status: mo.None[string](),
		Text: mo.Some("[image image/png, 3 bytes]"),
		Contents: mo.Some([]presentationdomain.Content{{
			Text: mo.None[string](), MediaType: mo.Some("image/png"), Data: mo.Some(imageBytes),
		}}),
	}}
	service := New()
	state := presentationdomain.State{
		Startup: nil, Transcript: nil, Models: nil, ActiveModel: nil, ActiveToolCalls: nil, ActiveTools: nil,
		Availability: mo.None[presentationdomain.Availability](), AuthorizationURL: mo.None[string](),
		Settled: mo.None[bool](), ModelSelection: mo.None[presentationdomain.ModelSelection](),
		SessionInfo: mo.None[presentationdomain.SessionInfo](), Sessions: nil,
	}
	timestamp := time.Date(2026, 8, 27, 1, 0, 0, 0, time.UTC)
	info := presentationdomain.SessionInfo{
		ID: "stored", Name: "", NamePresent: false, WorkingDirectory: "/project",
		StoragePath: "", StoragePresent: false, CreatedAt: timestamp, UpdatedAt: timestamp,
	}

	// Act by applying SessionChanged and mutating every caller-owned byte reference.
	state = service.Apply(state, testSessionEvent(
		presentationdomain.EventSessionChanged, mo.Some(info), restored,
	))
	imageBytes[0] = 9
	restored[0].Contents.MustGet()[0].Data.MustGet()[1] = 9

	// Assert presentation state retains an independent copy of the original image.
	require.Equal(t, []byte{1, 2, 3}, state.Transcript[0].Contents.MustGet()[0].Data.MustGet())
}
